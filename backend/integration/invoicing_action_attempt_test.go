package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Check status and Resend name the answer they got (#736, parent #731).
// Both actions answer 200 whatever the SRI said, and an answer that decides
// nothing - or a call that never reached it - leaves the status as it was,
// so the status cannot tell the page what this press learned. The response
// carries `attempt_id`, the attempts row the press itself wrote last, and
// that row's outcome is the SRI's answer. Asserted at the HTTP seam.

// pressedAttempt decodes `attempt_id` and returns the row it names in the
// same response's `attempts`, failing when it is missing or names nothing.
func pressedAttempt(t *testing.T, env envelope, view invoiceDetailView) (operation, outcome string) {
	t.Helper()
	var named struct {
		AttemptID *int64 `json:"attempt_id"`
	}
	if err := json.Unmarshal(env.Data, &named); err != nil {
		t.Fatalf("decode attempt_id: %v", err)
	}
	if named.AttemptID == nil {
		t.Fatal("attempt_id is null: the press must name the attempt it recorded")
	}
	for _, a := range view.Attempts {
		if a.ID == *named.AttemptID {
			return a.Operation, a.Outcome
		}
	}
	t.Fatalf("attempt_id %d names no row in attempts", *named.AttemptID)
	return "", ""
}

// TestCheckNamesItsAttemptOnARejectedDocument: the exact press the Abandon
// flow asks for. The status stays rejected; the attempt says `unknown`.
func TestCheckNamesItsAttemptOnARejectedDocument(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)

	sriStub.setAuthorization(func(accessKey string) (int, string) {
		return http.StatusOK, emptyAuthorizationSOAP(accessKey)
	})
	resp, body := checkInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "check", resp, body)
	if view.Status != "rejected" {
		t.Fatalf("status=%q, want rejected still", view.Status)
	}
	if op, outcome := pressedAttempt(t, body, view); op != "query" || outcome != "unknown" {
		t.Fatalf("pressed attempt = %s/%s, want query/unknown", op, outcome)
	}
}

// TestCheckNamesItsAttemptWhenTheCallFails: autorización fails in
// transport. The check still answers 200 and the status is untouched; the
// attempt it names is the `error` row, so the page can say nothing was
// learned rather than restate the status.
func TestCheckNamesItsAttemptWhenTheCallFails(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)

	for _, setup := range []struct {
		name  string
		issue func(*testing.T, string) invoiceDetailView
	}{
		{"rejected", issueRejected},
		{"pending", issuePending},
	} {
		sriStub.reset()
		doc := setup.issue(t, sessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusInternalServerError, "" })
		resp, body := checkInvoice(t, sessionID, doc.ID)
		view := actionOK(t, "check "+setup.name, resp, body)
		if view.Status != setup.name {
			t.Fatalf("%s: status=%q, want it untouched", setup.name, view.Status)
		}
		if op, outcome := pressedAttempt(t, body, view); op != "query" || outcome != "error" {
			t.Fatalf("%s: pressed attempt = %s/%s, want query/error", setup.name, op, outcome)
		}
	}
}

// TestCheckNamesEachAnswer: every answer autorización can give is the
// outcome of the attempt the check names.
func TestCheckNamesEachAnswer(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	pending := issuePending(t, sessionID)

	for _, answer := range []struct {
		want string
		soap func(string) string
	}{
		{"received", inProcessingSOAP},
		{"not_authorized", func(k string) string {
			return notAuthorizedSOAP(k, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
		}},
		{"authorized", authorizedSOAP},
	} {
		sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, answer.soap(accessKey) })
		resp, body := checkInvoice(t, sessionID, pending.ID)
		view := actionOK(t, "check "+answer.want, resp, body)
		if op, outcome := pressedAttempt(t, body, view); op != "query" || outcome != answer.want {
			t.Fatalf("pressed attempt = %s/%s, want query/%s", op, outcome, answer.want)
		}
	}
}

// TestResendNamesItsSubmitWhenRecepcionFails: the resend's submit dies in
// transport on a rejected document. Nothing is polled after it, the status
// is untouched, and the attempt named is the `submit / error` row.
func TestResendNamesItsSubmitWhenRecepcionFails(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)

	sriStub.setReception(func(accessKey string) (int, string) { return http.StatusInternalServerError, "" })
	resp, body := resendInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "resend", resp, body)
	if view.Status != "rejected" {
		t.Fatalf("status=%q, want rejected still", view.Status)
	}
	if op, outcome := pressedAttempt(t, body, view); op != "submit" || outcome != "error" {
		t.Fatalf("pressed attempt = %s/%s, want submit/error", op, outcome)
	}
}

// TestResendNamesItsLastPoll: a resend the SRI takes and then authorizes
// ends on the poll that found it authorized, and that is the row it names.
func TestResendNamesItsLastPoll(t *testing.T) {
	env := setupTest(t)
	sessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sessionID)
	rejected := issueRejected(t, sessionID)

	sriStub.reset()
	resp, body := resendInvoice(t, sessionID, rejected.ID)
	view := actionOK(t, "resend", resp, body)
	if view.Status != "authorized" {
		t.Fatalf("status=%q, want authorized", view.Status)
	}
	if op, outcome := pressedAttempt(t, body, view); op != "query" || outcome != "authorized" {
		t.Fatalf("pressed attempt = %s/%s, want query/authorized", op, outcome)
	}
}
