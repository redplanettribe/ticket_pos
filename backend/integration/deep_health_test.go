package integration

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// deepHealthPath is the API's deep health route, the one the Storefront's
// uptime target asks (#775).
const deepHealthPath = "/api/v1/public/health"

// linkMode is how a databaseLink treats a connection the app opens.
type linkMode string

const (
	linkUp      linkMode = "up"
	linkRefused linkMode = "refused"
	linkHung    linkMode = "hung"
)

// databaseLink stands between the app and the test database, so a test can
// take the database away from a running app without touching the shared
// container: "up" forwards, "refused" closes every connection as it arrives,
// "hung" accepts and never answers, the database that stopped responding.
// Changing the mode drops the connections already open, as an outage would.
type databaseLink struct {
	listener net.Listener
	target   string
	mode     atomic.Value // a linkMode

	mu    sync.Mutex
	conns map[net.Conn]bool
	// hungOpen counts the connections held in "hung" mode that the app has
	// not closed yet, other than cancel requests: when a ping is cut short
	// on a connection the outage broke, pgx sends the server a cancel request
	// on a connection of its own, in the background and outside the pool,
	// and gives up on it after 15 seconds (pgconn's asyncClose). That one is
	// pgx's cleanup, not the check's connection, and is not counted.
	hungOpen atomic.Int64
}

// cancelRequestCode is the code a PostgreSQL CancelRequest message carries
// after its length, where a StartupMessage carries its protocol version.
const cancelRequestCode = 80877102

func newDatabaseLink(t *testing.T, target string) *databaseLink {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	link := &databaseLink{listener: listener, target: target, conns: map[net.Conn]bool{}}
	link.mode.Store(linkUp)
	go link.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		link.dropAll()
	})
	return link
}

func (l *databaseLink) setMode(mode linkMode) {
	l.mode.Store(mode)
	l.dropAll()
}

func (l *databaseLink) dropAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for conn := range l.conns {
		_ = conn.Close()
	}
	clear(l.conns)
}

func (l *databaseLink) track(conn net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns[conn] = true
}

func (l *databaseLink) serve() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return
		}
		switch l.mode.Load() {
		case linkRefused:
			_ = conn.Close()
		case linkHung:
			l.track(conn)
			go func() {
				// Read until the app lets go; nothing is ever answered.
				var head [8]byte
				if _, err := io.ReadFull(conn, head[:]); err != nil {
					return
				}
				if binary.BigEndian.Uint32(head[4:]) == cancelRequestCode {
					_, _ = io.Copy(io.Discard, conn)
					return
				}
				l.hungOpen.Add(1)
				_, _ = io.Copy(io.Discard, conn)
				l.hungOpen.Add(-1)
			}()
		default:
			upstream, err := net.Dial("tcp", l.target)
			if err != nil {
				_ = conn.Close()
				continue
			}
			l.track(conn)
			l.track(upstream)
			go func() { _, _ = io.Copy(upstream, conn); _ = upstream.Close() }()
			go func() { _, _ = io.Copy(conn, upstream); _ = conn.Close() }()
		}
	}
}

// startAppBehindLink is the app with the fake reporter, its database reached
// through a databaseLink, and a draw that would trace any ordinary request.
func startAppBehindLink(t *testing.T, fake *reportertest.Reporter) (*testEnv, *server.App, *databaseLink) {
	t.Helper()
	parsed, err := url.Parse(sharedConnStr)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	link := newDatabaseLink(t, parsed.Host)
	parsed.Host = link.listener.Addr().String()
	var now atomic.Int64
	now.Store(fixedClock.UnixNano())
	env, app := startAppReportingToDatabase(t, fake, &now, parsed.String(),
		server.WithTraceRandom(func() float64 { return 0 }))
	return env, app, link
}

func getDeepHealth(t *testing.T, env *testEnv, sentryTrace string) (*http.Response, []byte, time.Duration) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, env.server.URL+deepHealthPath, nil)
	if sentryTrace != "" {
		req.Header.Set("sentry-trace", sentryTrace)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", deepHealthPath, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body, time.Since(start)
}

// THE DEEP HEALTH ROUTE ANSWERS 200 ONLY WHILE THE DATABASE ANSWERS, AND A
// FAILING ONE IS NOT AN ERROR EVENT (#775, ADR 0079). It is what the
// Storefront's uptime target asks, over HTTP, with the database reached
// through a link the test cuts: up, it is a 200 that is never cached; refused
// and hung, it is a 503 with a Retry-After inside the check's short bound,
// holding no connection afterwards; and every answer, up or down, is never
// traced (the draw and the Next app would both trace it) and reports nothing:
// an outage raises the uptime alert, not one issue per probe. The database
// back, it is a 200 again with no restart.
func TestTheDeepHealthRouteAnswersByTheDatabaseAndIsNeverTracedOrReported(t *testing.T) {
	setupTest(t)
	const sampled = "771a43a4192642f0b136d5159a501700-b7ad6b7169203331-1"
	fake := reportertest.New()
	env, app, link := startAppBehindLink(t, fake)

	resp, body, _ := getDeepHealth(t, env, sampled)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("up: status = %d, want 200; body %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("up: Cache-Control = %q, want no-store", got)
	}
	var envelope struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.Status != "ok" {
		t.Errorf("up: body = %s, want data.status ok", body)
	}

	for _, mode := range []linkMode{linkRefused, linkHung} {
		t.Run(string(mode), func(t *testing.T) {
			link.setMode(mode)
			resp, body, took := getDeepHealth(t, env, sampled)
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body %s", resp.StatusCode, body)
			}
			if resp.Header.Get("Retry-After") == "" {
				t.Errorf("no Retry-After on the 503")
			}
			if got := resp.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			var failed platform.Envelope
			if err := json.Unmarshal(body, &failed); err != nil || failed.Error == nil || failed.Error.Code != "DATABASE_UNREACHABLE" {
				t.Errorf("body = %s, want error DATABASE_UNREACHABLE", body)
			}
			if bound := platform.DeepHealthTimeout + time.Second; took > bound {
				t.Errorf("answered in %s, want within %s", took, bound)
			}
			if stats := app.DB.Pool.Stats(); stats.InUse != 0 {
				t.Errorf("connections in use after the answer = %d, want 0", stats.InUse)
			}
			if mode == linkHung {
				link.waitUntilLetGo(t)
			}
		})
	}

	link.setMode(linkUp)
	resp, body, _ = getDeepHealth(t, env, sampled)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("back up: status = %d, want 200; body %s", resp.StatusCode, body)
	}

	if reports := fake.Errors(); len(reports) != 0 {
		t.Errorf("reports = %d (%v), want none for a deep health check", len(reports), reports)
	}
	requests := requestSpans(fake)
	if len(requests) != 4 {
		t.Fatalf("request spans = %d, want 4", len(requests))
	}
	for _, span := range requests {
		if span.Options.Name != "GET "+deepHealthPath || span.Options.Sampled {
			t.Errorf("span %q sampled %v, want %q never sampled", span.Options.Name, span.Options.Sampled, "GET "+deepHealthPath)
		}
	}
	for _, span := range fake.Spans() {
		if span.Options.Op != "http.server" {
			t.Errorf("child span %q %q, want none: the check is never traced", span.Options.Op, span.Options.Name)
		}
	}
}

// THE SHALLOW HEALTH CHECK CLOUD RUN PROBES DOES NOT ASK THE DATABASE (#775):
// a database outage must not make Cloud Run restart the API.
func TestTheShallowHealthCheckAnswersWithTheDatabaseDown(t *testing.T) {
	setupTest(t)
	env, _, link := startAppBehindLink(t, reportertest.New())
	link.setMode(linkRefused)

	resp, err := http.Get(env.server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "{\"status\":\"ok\"}\n" {
		t.Errorf("GET /health = %d %s, want 200 {\"status\":\"ok\"}", resp.StatusCode, body)
	}
}

// waitUntilLetGo waits briefly for the app to close every hung connection
// it opened, cancel requests aside.
func (l *databaseLink) waitUntilLetGo(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for l.hungOpen.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the check still holds %d hung connection(s)", l.hungOpen.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
