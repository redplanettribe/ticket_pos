package reporter

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

const (
	seededEmail          = "ana.perez@example.com"
	seededIdentityNumber = "1710034065"
	seededCookie         = "staff_session=s3cr3t-cookie"
	seededHeader         = "X-Buyer-Note"
	seededHeaderValue    = "deliver-to-ana-at-home"
	seededToken          = "bearer-t0ken-value"
	seededIP             = "203.0.113.7"
	seededForwardedIP    = "198.51.100.23"
	memberID             = "0b6f4a52-3c1e-4c8e-9d43-2f1a7e5b9c10"
	organizationID       = "a3c5e7f9-1b2d-4e6f-8a0c-2e4f6a8c0e1b"
	eventID              = "5e8a1c3d-7f9b-4d2e-a6c8-0b1d3f5a7c9e"
)

// A FAILING REQUEST SEEDED WITH A PERSON SENDS NOTHING OF THEM (ADR 0079).
// The request carries an email and an identity number in its query, body and
// the error its handler fails with, a cookie, a header of its own, a bearer
// token and the client's address; the event that leaves the process carries
// the request id, the route, the Member id and the ids the request concerns,
// and none of them.
func TestAFailingRequestSeededWithPersonalDataSendsNoneOfIt(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	reporter := New(platform.ReporterConfig{DSN: in.dsn(), Environment: "development", Release: "0123abc"}, platform.NewSlogLogger(slog.New(slog.DiscardHandler), platform.NoopReporter{}))
	logger := platform.NewSlogLogger(slog.New(slog.DiscardHandler), reporter)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/staff/events/{id}/sales", func(w http.ResponseWriter, r *http.Request) {
		// What the staff session middleware supplies.
		platform.SetReportActor(r.Context(), platform.AudienceStaff, memberID)
		platform.SetReportTag(r.Context(), platform.ReportTagOrganization, organizationID)
		cause := fmt.Errorf("upsert customer %s (cédula %s) via https://pay.example.com/confirm?email=%s&id=%s: connection refused",
			seededEmail, seededIdentityNumber, seededEmail, seededIdentityNumber)
		logger.Error(r.Context(), "could not record the in-person sale", "email", seededEmail, "tax_id", seededIdentityNumber, "error", cause)
		_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), cause)
	})
	api := httptest.NewServer(platform.RequestPipeline(slog.New(slog.DiscardHandler), reporter, mux))
	defer api.Close()

	path := "/api/v1/staff/events/" + eventID + "/sales"
	body := fmt.Sprintf(`{"email":%q,"tax_id_number":%q}`, seededEmail, seededIdentityNumber)
	req, _ := http.NewRequest(http.MethodPost, api.URL+path+"?email="+seededEmail+"&tax_id="+seededIdentityNumber, strings.NewReader(body))
	req.Header.Set("Cookie", seededCookie)
	req.Header.Set(seededHeader, seededHeaderValue)
	req.Header.Set("Authorization", "Bearer "+seededToken)
	req.Header.Set(platform.ClientIPHeader, seededIP)
	req.Header.Set("X-Forwarded-For", seededForwardedIP)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("reports not flushed")
	}

	events, _ := in.received()
	if len(events) != 1 {
		t.Fatalf("ingest received %d events, want 1", len(events))
	}
	raw, _ := json.Marshal(events[0])
	for _, leaked := range []string{
		seededEmail, "ana.perez", seededIdentityNumber, seededCookie, "s3cr3t-cookie", seededHeader, seededHeaderValue,
		seededToken, seededIP, seededForwardedIP, path, "?email", "pay.example.com/confirm?",
	} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the event carries %q: %s", leaked, raw)
		}
	}

	var event struct {
		User      map[string]any    `json:"user"`
		Tags      map[string]string `json:"tags"`
		Request   any               `json:"request"`
		Exception []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"exception"`
	}
	_ = json.Unmarshal(raw, &event)
	if len(event.User) != 1 || event.User["id"] != memberID {
		t.Errorf("user = %v, want the Member id alone", event.User)
	}
	wantTags := map[string]string{
		"request_id":      resp.Header.Get(platform.RequestIDHeader),
		"route":           "POST /api/v1/staff/events/{id}/sales",
		"audience":        "staff",
		"organization_id": organizationID,
		"event_id":        eventID,
	}
	for key, want := range wantTags {
		if event.Tags[key] != want {
			t.Errorf("tag %s = %q, want %q (tags %v)", key, event.Tags[key], want, event.Tags)
		}
	}
	if event.Request != nil {
		t.Errorf("the event carries the request: %v", event.Request)
	}
	if len(event.Exception) != 1 {
		t.Fatalf("exception = %+v, want one", event.Exception)
	}
	wantValue := "could not record the in-person sale: upsert customer [email] (cédula [identity-number]) via https://pay.example.com/confirm connection refused"
	// The query runs to the next space, and goes with the colon after it.
	if event.Exception[0].Value != wantValue {
		t.Errorf("exception value = %q, want %q", event.Exception[0].Value, wantValue)
	}
	if event.Exception[0].Type != "*errors.errorString" {
		t.Errorf("exception type = %q, want the error's class, the type of the innermost error", event.Exception[0].Type)
	}
}

// Every string an event or a transaction can carry text in is scrubbed before
// it is sent, so a span the tracing tickets name after a statement or a URL
// is held to the same rules as an error.
func TestEveryTextOfAnEventIsScrubbedBeforeSend(t *testing.T) {
	event := &sentry.Event{
		Message:     "mail to " + seededEmail + " failed",
		Transaction: "GET /checkout?email=" + seededEmail,
		Fingerprint: []string{"customer " + seededIdentityNumber},
		User:        sentry.User{ID: memberID, Email: seededEmail, IPAddress: seededIP, Username: "ana", Name: "Ana Pérez"},
		Request:     &sentry.Request{URL: "https://api.example.com/x?email=" + seededEmail, Cookies: seededCookie},
		Tags:        map[string]string{"note": seededEmail},
		Contexts:    map[string]sentry.Context{"buyer": {"tax_id": seededIdentityNumber, "nested": map[string]any{"emails": []any{seededEmail}}}},
		Exception: []sentry.Exception{{
			Type:       "no customer " + seededEmail,
			Value:      "tax id " + seededIdentityNumber + " refused",
			Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{Function: "f", Vars: map[string]any{"email": seededEmail}, AbsPath: "/app/x.go?" + seededEmail}}},
		}},
		Threads: []sentry.Thread{{Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{Vars: map[string]any{"cedula": seededIdentityNumber}}}}}},
		Breadcrumbs: []*sentry.Breadcrumb{{
			Message: "signed in as " + seededEmail,
			Data:    map[string]any{"url": "/p?token=" + seededToken, "to": seededEmail},
		}},
		Spans: []*sentry.Span{{
			Description: "GET https://pay.example.com/confirm?id=" + seededIdentityNumber,
			Tags:        map[string]string{"buyer": seededEmail},
			Name:        "GET /checkout?token=" + seededToken,
			Data:        map[string]any{"http.query": "token=" + seededToken, "http.fragment": "t=" + seededToken, "http.url": "https://pay.example.com/c?email=" + seededEmail, "db.statement": "select 1 where email = '" + seededEmail + "'", "count": 3},
		}},
	}

	scopesBreadcrumb := event.Breadcrumbs[0]
	scopesTags := event.Tags

	out := stripPersonalData(event, nil)

	// What the event shares with the hub's scope is replaced, not edited.
	if scopesBreadcrumb.Message != "signed in as "+seededEmail || scopesTags["note"] != seededEmail {
		t.Errorf("the scope's breadcrumb or tags were edited in place: %q, %v", scopesBreadcrumb.Message, scopesTags)
	}

	raw, _ := json.Marshal(out)
	for _, leaked := range []string{seededEmail, seededIdentityNumber, seededToken, seededIP, seededCookie, "Ana Pérez", "token="} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("the scrubbed event carries %q: %s", leaked, raw)
		}
	}
	if out.User.ID != memberID {
		t.Errorf("user id = %q, want the opaque id kept", out.User.ID)
	}
	if got := out.Spans[0].Description; got != "GET https://pay.example.com/confirm" {
		t.Errorf("span description = %q, want the URL without its query", got)
	}
	if got := out.Spans[0].Data["count"]; got != 3 {
		t.Errorf("span data count = %v, want a number left alone", got)
	}
}
