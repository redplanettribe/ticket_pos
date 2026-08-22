package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Answer Link is RETIRED (#346, ADR 0049). ADR 0044's signed, proof-free
// link that a buyer forwarded so that whoever would hold a Ticket could answer
// its questions no longer exists: its signer, its routes, its handlers and its
// error codes were deleted rather than disabled, and a Ticket's questions are
// reached only through the Ticket Assignment — the Holder accepts by Assignment
// Link and answers from their own Customer Area.
//
// These tests are the tombstone. They assert on the wire that the two old
// routes are gone and that no payload a buyer or a Holder can read carries an
// `answer_link`, so a build that quietly brought either back fails here.

// publicLinkRequest sends a JSON body to a public, unauthenticated route and
// returns the response, the decoded envelope and the RAW bytes.
//
// NO AUTHORIZATION HEADER AND NO COOKIE JAR: whoever holds a signed link has no
// session, and a test that signed in first would be testing a different thing.
// The raw bytes are returned alongside the envelope because the central
// assertion of every link surface is about what the response does NOT contain,
// and a struct can only assert on fields somebody remembered to declare.
func publicLinkRequest(t *testing.T, env *testEnv, method, path string, body any) (*http.Response, envelope, []byte) {
	t.Helper()
	resp, raw := rawPublicRequest(t, env, method, path, body)
	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode envelope from %q: %v", raw, err)
	}
	return resp, decoded, raw
}

// rawPublicRequest is publicLinkRequest without the envelope, for a route that
// may not answer with one — a 404 from the mux is plain text.
func rawPublicRequest(t *testing.T, env *testEnv, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(method, env.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

// BOTH ANSWER LINK ROUTES ARE GONE, with every feature flag open and a token of
// the old shape in the body. Not a 401, not a 410 with an envelope: there is no
// handler, so the mux answers as it does for any address that never existed.
func TestTheAnswerLinkRoutesNoLongerExist(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	for _, call := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/public/answer-link"},
		{http.MethodPut, "/api/v1/public/answer-link/questions/" + f.sizeQuestion.ID},
	} {
		resp, raw := rawPublicRequest(t, env, call.method, call.path,
			map[string]any{"token": "YW5zd2VyOmFueXRoaW5n.c2lnbmVk", "text": "XXL"})
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s answered %d, want 404: the Answer Link is retired (ADR 0049).\nbody: %s",
				call.method, call.path, resp.StatusCode, raw)
		}
		if strings.Contains(string(raw), "ANSWER_LINK") {
			t.Errorf("%s %s still speaks an ANSWER_LINK_* code: %s", call.method, call.path, raw)
		}
	}
}

// NO PAYLOAD A BUYER OR A HOLDER READS CARRIES AN `answer_link`, before or after
// a Ticket is assigned and accepted: not the buyer's sale-scoped list, not the
// response to naming an address, not the Holder's own list, and not the
// Assignment Link page. There is nothing to mint one from any more, and this
// asserts the bytes rather than trusting the deletion.
func TestNoResponseCarriesAnAnswerLink(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	payloads := map[string][]byte{}

	readResp, readBody := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(f.ana))
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", readResp.StatusCode, readBody.Error)
	}
	payloads["the buyer's list before assigning"] = readBody.Data

	assignResp, assignBody := assignTicket(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	if assignResp.StatusCode != http.StatusOK {
		t.Fatalf("assign status=%d error=%+v", assignResp.StatusCode, assignBody.Error)
	}
	payloads["the response to the assignment"] = assignBody.Data

	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptResp, acceptBody, acceptRaw := acceptAssignment(t, env, token)
	if acceptResp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", acceptResp.StatusCode, acceptBody.Error)
	}
	payloads["the Assignment Link page"] = acceptRaw

	readResp, readBody = env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(f.ana))
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", readResp.StatusCode, readBody.Error)
	}
	payloads["the buyer's list after acceptance"] = readBody.Data

	_, anaHeld := listHeldTickets(t, env, f.ana)
	payloads["the buyer's held list"] = anaHeld
	_, carlaHeld := listHeldTickets(t, env, customerSignIn(t, env, "carla@example.com"))
	payloads["the Holder's held list"] = carlaHeld

	for name, raw := range payloads {
		for _, forbidden := range []string{"answer_link", "answer-link", "/answer?token="} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("%s carries %q.\nThe Answer Link is retired: no surface mints or returns one (ADR 0049).\n%s",
					name, forbidden, raw)
			}
		}
	}
}

// setEventStart moves an Event's start directly, because the staff route refuses
// a start in the past and the whole point of a window test is to stand on the
// far side of one.
func setEventStart(t *testing.T, env *testEnv, eventID string, startsAt time.Time) {
	t.Helper()
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $1 WHERE id = $2`, startsAt, eventID); err != nil {
		t.Fatalf("set Event start: %v", err)
	}
}

// mustGetTicket reads a Ticket through the staff route, failing on any refusal.
func mustGetTicket(t *testing.T, env *testEnv, sessionID, eventID, ticketID string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, ticketPath(eventID, ticketID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff read of the Ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return body.Data
}
