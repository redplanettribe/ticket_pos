package reporter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

const sentrySDK = "github.com/getsentry/sentry-go"

// TestOnlyThisPackageImportsTheSentrySDK keeps the vendor behind the one seam
// (ADR 0079): any other Go file in the backend importing sentry-go, or one of
// its packages, fails here.
func TestOnlyThisPackageImportsTheSentrySDK(t *testing.T) {
	backendRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seam, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != backendRoot && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || filepath.Dir(path) == seam {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, spec := range file.Imports {
			importPath, _ := strconv.Unquote(spec.Path.Value)
			if importPath == sentrySDK || strings.HasPrefix(importPath, sentrySDK+"/") {
				rel, _ := filepath.Rel(backendRoot, path)
				t.Errorf("%s imports %s; only internal/platform/reporter may", rel, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("checked only %d Go files under %s; is the walk rooted at the backend?", checked, backendRoot)
	}
}

func TestWithNoDSNTheReporterIsTheNoOp(t *testing.T) {
	got := New(platform.ReporterConfig{Environment: "development", Release: "abc"}, platform.NewSlogLogger(slog.New(slog.DiscardHandler)))
	if _, ok := got.(platform.NoopReporter); !ok {
		t.Fatalf("New with no DSN = %T, want platform.NoopReporter", got)
	}
}

func TestARefusedDSNLeavesTheNoOpAndSaysSo(t *testing.T) {
	var logBuf bytes.Buffer
	got := New(platform.ReporterConfig{DSN: "not a dsn"}, platform.NewSlogLogger(slog.New(slog.NewJSONHandler(&logBuf, nil))))
	if _, ok := got.(platform.NoopReporter); !ok {
		t.Fatalf("New with a refused DSN = %T, want platform.NoopReporter", got)
	}
	if !strings.Contains(logBuf.String(), `"level":"WARN"`) {
		t.Fatalf("a refused DSN was not logged at Warn: %s", logBuf.String())
	}
}

// ingest stands in for Sentry's ingest endpoint on localhost: a test never
// contacts Sentry. It keeps every event item it is sent.
type ingest struct {
	server *httptest.Server
	status int

	mu     sync.Mutex
	events []map[string]any
	posts  int
}

func newIngest(t *testing.T, status int) *ingest {
	t.Helper()
	in := &ingest{status: status}
	in.server = httptest.NewServer(http.HandlerFunc(in.receive))
	t.Cleanup(in.server.Close)
	return in
}

func (in *ingest) dsn() string {
	return strings.Replace(in.server.URL, "http://", "http://publickey@", 1) + "/42"
}

// receive reads an envelope: a header line, then item header and payload
// line pairs.
func (in *ingest) receive(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	lines := bufio.NewScanner(bytes.NewReader(body))
	lines.Buffer(make([]byte, 1<<20), 1<<24)
	lines.Scan() // envelope header
	var events []map[string]any
	for lines.Scan() {
		var item struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(lines.Bytes(), &item)
		if !lines.Scan() {
			break
		}
		if item.Type == "event" {
			var event map[string]any
			_ = json.Unmarshal(lines.Bytes(), &event)
			events = append(events, event)
		}
	}
	in.mu.Lock()
	in.events = append(in.events, events...)
	in.posts++
	in.mu.Unlock()
	w.WriteHeader(in.status)
}

func (in *ingest) received() ([]map[string]any, int) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]map[string]any(nil), in.events...), in.posts
}

func panicking(http.ResponseWriter, *http.Request) {
	panic("handler exploded")
}

func TestARecoveredPanicLeavesTheProcessAsOneEventWithItsStackAndNoRequest(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	reporter := New(platform.ReporterConfig{DSN: in.dsn(), Environment: "development", Release: "0123abc"}, platform.NewSlogLogger(slog.New(slog.DiscardHandler)))
	api := httptest.NewServer(platform.RequestPipeline(slog.New(slog.DiscardHandler), reporter, http.HandlerFunc(panicking)))
	defer api.Close()

	req, _ := http.NewRequest(http.MethodPost, api.URL+"/orders?email=buyer@example.com", strings.NewReader(`{"email":"buyer@example.com"}`))
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set(platform.ClientIPHeader, "203.0.113.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("reports not flushed")
	}

	events, _ := in.received()
	if len(events) != 1 {
		t.Fatalf("ingest received %d events, want 1", len(events))
	}
	event := events[0]
	if event["release"] != "0123abc" || event["environment"] != "development" {
		t.Errorf("release, environment = %v, %v; want 0123abc, development", event["release"], event["environment"])
	}
	if tags, _ := event["tags"].(map[string]any); tags["request_id"] != resp.Header.Get(platform.RequestIDHeader) {
		t.Errorf("tags = %v, want the response's request id", event["tags"])
	}

	raw, _ := json.Marshal(event)
	if event["server_name"] != serverName {
		t.Errorf("server_name = %v, want %q rather than the host's name", event["server_name"], serverName)
	}
	for _, leaked := range []string{"buyer@example.com", "session=secret", "203.0.113.7"} {
		if bytes.Contains(raw, []byte(leaked)) {
			t.Errorf("event carries %q: %s", leaked, raw)
		}
	}
	if _, ok := event["request"]; ok {
		t.Errorf("event carries the request: %v", event["request"])
	}

	// The innermost frame is the one that panicked, not the reporter's
	// goroutine.
	var stack struct {
		Exception []struct {
			Value      string `json:"value"`
			Stacktrace struct {
				Frames []struct {
					Function string `json:"function"`
				} `json:"frames"`
			} `json:"stacktrace"`
		} `json:"exception"`
	}
	_ = json.Unmarshal(raw, &stack)
	values := stack.Exception
	if len(values) == 0 {
		t.Fatalf("event has no exception: %s", raw)
	}
	frames := values[len(values)-1].Stacktrace.Frames
	if len(frames) == 0 || frames[len(frames)-1].Function != "panicking" {
		t.Fatalf("stack = %+v, want it to end at panicking", frames)
	}
	if !strings.Contains(values[len(values)-1].Value, "handler exploded") {
		t.Errorf("exception value = %q, want the panic value", values[len(values)-1].Value)
	}
}

func TestAFailingDeliveryIsLoggedOnceAtWarn(t *testing.T) {
	in := newIngest(t, http.StatusInternalServerError)
	var logBuf syncBuffer
	reporter := New(platform.ReporterConfig{DSN: in.dsn(), Environment: "development"}, platform.NewSlogLogger(slog.New(slog.NewJSONHandler(&logBuf, nil))))
	api := httptest.NewServer(platform.RequestPipeline(slog.New(slog.DiscardHandler), reporter, http.HandlerFunc(panicking)))
	defer api.Close()

	for range 3 {
		resp, err := http.Get(api.URL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want the panic's 500", resp.StatusCode)
		}
		reporter.Flush(5 * time.Second)
	}

	if _, posts := in.received(); posts < 2 {
		t.Fatalf("ingest refused %d deliveries, want several", posts)
	}
	if got := strings.Count(logBuf.String(), `"level":"WARN"`); got != 1 {
		t.Fatalf("delivery failures logged %d times at Warn, want once: %s", got, logBuf.String())
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
