package platform_test

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

const (
	seededQuery = "ana.perez%40example.com"
	seededBody  = `{"identity":"1710034065","name":"Ana Perez"}`
)

// thirdParty is a stand-in for a provider: it answers 201 and keeps the
// headers of every call it received.
type thirdParty struct {
	*httptest.Server
	mu      sync.Mutex
	headers []http.Header
}

func newThirdParty(t *testing.T) *thirdParty {
	t.Helper()
	party := &thirdParty{}
	party.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		party.mu.Lock()
		party.headers = append(party.headers, r.Header.Clone())
		party.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(party.Close)
	return party
}

func (p *thirdParty) received() []http.Header {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]http.Header(nil), p.headers...)
}

// callSeeded posts the seeded body to the third party's /v1/sales with the
// seeded query string, through a client from the shared constructor.
func callSeeded(t *testing.T, ctx context.Context, party *thirdParty) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, party.URL+"/v1/sales?email="+seededQuery+"#frag", strings.NewReader(seededBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := platform.NewOutboundClient(5 * time.Second).Do(req)
	if err != nil {
		t.Fatalf("outbound call: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want the third party's 201", resp.StatusCode)
	}
}

func spanTexts(span *reportertest.Span) []string {
	texts := []string{span.Options.Name, span.Options.SentryTrace, span.Options.Baggage}
	for key, value := range span.Tags {
		texts = append(texts, key, value)
	}
	return texts
}

// AN OUTBOUND CALL IN A SAMPLED REQUEST IS A CHILD SPAN NAMED BY ITS METHOD,
// HOST AND PATH (#769, ADR 0079). The call carries an email in its query
// string and an identity number and a name in its body: the span is under the
// request span, named "POST http://<host>/v1/sales", finished with the status
// the third party answered, and nothing seeded is in any span. The third
// party receives none of our trace headers.
func TestAnOutboundCallInASampledRequestIsAChildSpanNamedByMethodHostAndPath(t *testing.T) {
	fake := reportertest.New()
	party := newThirdParty(t)

	request := fake.InSampledRequest(func(ctx context.Context) { callSeeded(t, ctx, party) })

	children := fake.ChildrenOf(request)
	if len(children) != 1 {
		t.Fatalf("child spans = %d, want 1", len(children))
	}
	call := children[0]
	host := strings.TrimPrefix(party.URL, "http://")
	if call.Options.Op != "http.client" || call.Options.Name != "POST "+party.URL+"/v1/sales" {
		t.Errorf("span = %s %q, want http.client %q", call.Options.Op, call.Options.Name, "POST "+party.URL+"/v1/sales")
	}
	if call.Tags["http.request.method"] != http.MethodPost || call.Tags["server.address"] != host {
		t.Errorf("tags = %v, want method POST and host %s", call.Tags, host)
	}
	if !call.Finished || call.HTTPStatus != http.StatusCreated {
		t.Errorf("span finished %v with status %d, want finished with 201", call.Finished, call.HTTPStatus)
	}
	for _, span := range fake.Spans() {
		for _, text := range spanTexts(span) {
			for _, seeded := range []string{"email=", "ana.perez", "1710034065", "Ana Perez", "frag"} {
				if strings.Contains(text, seeded) {
					t.Errorf("span %q carries %q in %q", span.Options.Name, seeded, text)
				}
			}
		}
	}
	for _, headers := range party.received() {
		if headers.Get(platform.SentryTraceHeader) != "" || headers.Get(platform.BaggageHeader) != "" {
			t.Errorf("the third party received our trace headers: %v", headers)
		}
	}
}

// A CALL OUTSIDE ANY SAMPLED TRACE CREATES NO SPAN AND BEHAVES AS TODAY
// (#769). Outside a request, in a request the head did not sample, and with
// no DSN (the NoopReporter), the call reaches the third party and answers its
// status, and the only span is the request's own.
func TestACallOutsideASampledTraceCreatesNoSpan(t *testing.T) {
	party := newThirdParty(t)

	t.Run("outside a request", func(t *testing.T) {
		callSeeded(t, context.Background(), party)
	})

	t.Run("in a request the head did not sample", func(t *testing.T) {
		fake := reportertest.New()
		sampling := platform.TraceSampling{Random: func() float64 { return 0.99 }}
		pipeline := platform.TracedRequestPipeline(slog.New(slog.DiscardHandler), fake, sampling,
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { callSeeded(t, r.Context(), party) }))
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events", nil))
		if spans := fake.Spans(); len(spans) != 1 || spans[0].Options.Sampled {
			t.Errorf("spans = %d, want only the unsampled request span", len(spans))
		}
	})

	t.Run("with no DSN", func(t *testing.T) {
		sampling := platform.TraceSampling{Random: func() float64 { return 0 }}
		pipeline := platform.TracedRequestPipeline(slog.New(slog.DiscardHandler), platform.NoopReporter{}, sampling,
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { callSeeded(t, r.Context(), party) }))
		pipeline.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events", nil))
	})

	if got := len(party.received()); got != 3 {
		t.Errorf("third party received %d calls, want 3", got)
	}
}

// THE SHARED CONSTRUCTORS KEEP THE CALLER'S TIMEOUT, AND THE AWS ONE THE
// SDK'S CLIENT (#769). The AWS SDK's client does not follow a redirect that
// changes the method; through NewOutboundAWSClient it still does not.
func TestTheSharedConstructorsKeepTheTimeoutAndTheAWSClientsBehaviour(t *testing.T) {
	if got := platform.NewOutboundClient(7 * time.Second).Timeout; got != 7*time.Second {
		t.Errorf("outbound client timeout = %s, want 7s", got)
	}
	if got := platform.NewOutboundAWSClient(9 * time.Second).Timeout; got != 9*time.Second {
		t.Errorf("aws client timeout = %s, want 9s", got)
	}
	if got := platform.NewOutboundAWSClient(0).Timeout; got != 0 {
		t.Errorf("aws client timeout = %s, want none, the SDK's default", got)
	}

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)
	resp, err := platform.NewOutboundAWSClient(5*time.Second).Post(redirecting.URL, "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("aws client call: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("aws client answered %d, want the 302 itself, not followed", resp.StatusCode)
	}
}

// NO OUTBOUND CLIENT IS BUILT OUTSIDE THE SHARED CONSTRUCTORS (#769). A bare
// http.Client, or the package-level default client, in the backend's own code
// would be a third party's calls missing from the trace. Tests may build
// their own.
func TestNoOutboundClientIsBuiltOutsideTheSharedConstructors(t *testing.T) {
	bare := regexp.MustCompile(`http\.Client\{|http\.DefaultClient|NewBuildableClient\(|http\.(Get|Post|PostForm|Head)\(`)
	backend, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.WalkDir(backend, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "outbound.go" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(source), "\n") {
			if trimmed := strings.TrimSpace(line); !strings.HasPrefix(trimmed, "//") && bare.MatchString(line) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": "+trimmed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("outbound clients built outside platform.NewOutboundClient:\n%s", strings.Join(offenders, "\n"))
	}
}
