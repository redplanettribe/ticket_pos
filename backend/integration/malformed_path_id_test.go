package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/server"
)

// A malformed id in a route's path names a resource that cannot exist, so it is
// answered exactly as an id that names nothing is: 404 with the resource's own
// *_NOT_FOUND code. It is never a 500, and it is never told apart from an
// unknown id (the path-id rule: the api-errors skill and
// docs/technical-design.md, "Handler errors").
//
// The sweep walks the REGISTERED route table rather than a list kept here, so a
// route added later is covered the day it lands. For every id wildcard of every
// route it sends two requests from a caller the route's gate admits: one with
// that wildcard malformed and one with it a well-formed id that names nothing.
// Every other id wildcard is a real id wherever the sweep has one (a real Event,
// Ticket Type, question, option, Sale, Ticket, Customer, Affiliate Link and
// Member), so that on a read or a delete the two answers are both about the
// probed id and must be the same; elsewhere it is a well-formed id naming
// nothing.

// unguardedNonIDWildcards are the path wildcards that are not UUIDs on the
// namespaces the path-id guard does not cover (public, storefront, customer,
// partner), each with a value that is well-formed for it. On the staff and
// operator namespaces the guard's own exemptions say which wildcards are not
// ids (server.NonIDPathWildcard), and this file keeps no copy of them.
var unguardedNonIDWildcards = map[string]string{
	"slug":                "test-org",
	"eventSlug":           "no-such-event",
	"locale":              "en",
	"canonicalKey":        "no-such-tag",
	"purpose":             "marketing",
	"clientTransactionId": "no-such-transaction",
}

// nonIDValue reports whether the wildcard named name in pattern is not an id,
// and a well-formed value for it if so.
func nonIDValue(pattern, name string) (string, bool) {
	if example, nonID, guarded := server.NonIDPathWildcard(pattern, name); guarded {
		return example, nonID
	}
	value, ok := unguardedNonIDWildcards[name]
	return value, ok
}

// patternRecorder lists every pattern RegisterRoutes registers.
type patternRecorder struct{ patterns []string }

func (p *patternRecorder) Handle(pattern string, _ http.Handler) {
	p.patterns = append(p.patterns, pattern)
}

func (p *patternRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	p.patterns = append(p.patterns, pattern)
}

// registeredPatterns returns every "METHOD /path" pattern the API serves.
func registeredPatterns(t *testing.T) []string {
	t.Helper()
	rec := &patternRecorder{}
	server.RegisterRoutes(rec, sharedApp)
	sort.Strings(rec.patterns)
	return rec.patterns
}

// fillPattern substitutes each of pattern's wildcards, as the guard walks them
// (server.PatternWildcards): probed gets probeValue, the other id wildcards the
// fixed ids in others, the rest their nonIDValue.
func fillPattern(pattern, probed, probeValue string, others map[string]string) string {
	_, path, wildcards := server.PatternWildcards(pattern)
	nameOf := make(map[string]string, len(wildcards))
	for _, wc := range wildcards {
		nameOf[wc.Segment] = wc.Name
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		name, ok := nameOf[segment]
		if !ok {
			continue
		}
		switch value, isNonID := nonIDValue(pattern, name); {
		case name == probed:
			segments[i] = probeValue
		case isNonID:
			segments[i] = value
		default:
			segments[i] = others[name]
		}
	}
	return strings.Join(segments, "/")
}

type routeAnswer struct {
	status    int
	code      string
	requestID string
}

func (a routeAnswer) String() string {
	return strconv.Itoa(a.status) + " " + a.code
}

// callRoute sends method path as the holder of token, with an empty JSON object
// as the body of a write.
func callRoute(t *testing.T, env *testEnv, method, path, token string) routeAnswer {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, env.server.URL+path, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var decoded envelope
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s: status %d with a body that is not an envelope: %v", method, path, resp.StatusCode, err)
	}
	answer := routeAnswer{status: resp.StatusCode, requestID: decoded.RequestID}
	if decoded.Error != nil {
		answer.code = decoded.Error.Code
	}
	return answer
}

// bodyGatedRoutes read their credential from the request body, so a request
// without one is refused before any path id is looked at. The sweep holds them
// to "never a 5xx" only.
var bodyGatedRoutes = map[string]string{
	// The signed Assignment Link token travels in the body (#336).
	"PUT /api/v1/public/assignment-link/questions/{questionId}": "the Assignment Link token is in the body",
}

// TestEveryRouteRefusesAMalformedPathIDAsNotFound: a malformed id in any
// route's path is a client error and never a 500.
//
// On a read or a delete it is answered exactly as a well-formed id that names
// nothing: the same status and the same code, whether that is a 404 or, on the
// two Customer lists where "not yours" and "not there" are one empty answer, a
// 200. On a write it is refused as 404 *_NOT_FOUND before the body is read,
// because no body can make a resource that cannot exist into one that does.
func TestEveryRouteRefusesAMalformedPathIDAsNotFound(t *testing.T) {
	env := setupTest(t)
	// Every flag-gated surface open, so no route answers "this feature is dark"
	// before it has looked at its path.
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	// One staff session serves the staff and operator namespaces: an Org Admin
	// of an Organization who is also on the operator allowlist.
	staff := orgAdminSession(t, env)
	seedPlatformOperator(t, env, "admin@example.com")
	customer := customerSignIn(t, env, "buyer@example.com")
	// A real Event and everything an id can name beneath it, so that a probe of
	// an id nested beneath real parents is answered about that id and not about
	// an unknown parent, and is held to "the same answer" rather than only "the
	// same status".
	eventID := createDraftEvent(t, env, staff, "Sweep Event", "sweep-event")
	ticketTypeID := createTicketType(t, env, staff, eventID)
	question := createTicketQuestion(t, env, staff, eventID, ticketTypeID, map[string]any{
		"label": "Pick one", "kind": "single_choice", "option_labels": []string{"A", "B"},
	})
	sale := recordManualSaleOK(t, env, staff, eventID, manualSaleBody(
		"buyer@example.com", "Ana", "Lopez", ticketTypeID, 1, "cash", env.fixedClock.Add(-time.Hour).Format(time.RFC3339)))
	resp, body := createAffiliateLink(t, env, staff, eventID, map[string]any{"name": "Sweep link"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create affiliate link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	linkID := decodeAffiliateLink(t, body.Data).ID
	resp, body = env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "owner@example.com", "role": "event_owner",
	}, authHeader(staff))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var member struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	real := map[string]string{
		"id":           eventID,
		"eventID":      eventID,
		"ticketTypeId": ticketTypeID,
		"questionId":   question.ID,
		"optionId":     question.Options[0].ID,
		"ticketSaleId": sale.SaleID,
		"saleId":       sale.SaleID,
		"ticketId":     ticketIDsOfSale(t, env, sale.SaleID)[0],
		"customerId":   customerIDOnSalesList(t, env, staff, eventID, "buyer@example.com", "active"),
		"linkId":       linkID,
		"memberID":     member.ID,
	}
	// realFor reports the real id a wildcard of path names, if the sweep has
	// one: the staff Event routes nest under the real Event, and the signed-in
	// Customer is the buyer of the real Sale. A name means one thing only there
	// ("{id}" is an Event under /staff/events and a Tax Invoice elsewhere).
	realFor := func(path, name string) (string, bool) {
		id, ok := real[name]
		switch {
		case !ok:
			return "", false
		case strings.HasPrefix(path, "/api/v1/staff/events/{"):
			return id, name != "id" || strings.HasPrefix(path, "/api/v1/staff/events/{id}")
		case strings.HasPrefix(path, "/api/v1/customer/"):
			return id, name == "ticketSaleId" || name == "ticketId"
		}
		return "", false
	}

	tested := 0
	var failures []string
	for _, pattern := range registeredPatterns(t) {
		method, path, wildcards := server.PatternWildcards(pattern)
		if method == "" {
			continue
		}
		names := make([]string, 0, len(wildcards))
		for _, wc := range wildcards {
			names = append(names, wc.Name)
		}
		var token string
		switch {
		case strings.HasPrefix(path, "/api/v1/staff/"), strings.HasPrefix(path, "/api/v1/operator/"):
			token = staff
		case strings.HasPrefix(path, "/api/v1/customer/"):
			token = customer
		}
		// Every id the sweep has a real one for is that; every other id is a
		// well-formed one naming nothing.
		others := map[string]string{}
		for _, name := range names {
			if id, ok := realFor(path, name); ok {
				others[name] = id
			} else {
				others[name] = uuid.NewString()
			}
		}
		for _, name := range names {
			if _, isNonID := nonIDValue(pattern, name); isNonID {
				continue
			}
			// Whether every OTHER id in the path names something real. Only then
			// is the unknown-id answer about this wildcard rather than a parent.
			othersReal := true
			for _, other := range names {
				if _, isNonID := nonIDValue(pattern, other); other != name && !isNonID && others[other] != real[other] {
					othersReal = false
				}
			}
			tested++
			malformed := callRoute(t, env, method, fillPattern(pattern, name, "not-a-uuid", others), token)
			unknown := callRoute(t, env, method, fillPattern(pattern, name, uuid.NewString(), others), token)

			report := func(want string) {
				failures = append(failures, pattern+" {"+name+"}: malformed answered "+malformed.String()+
					", a well-formed unknown id "+unknown.String()+"; want "+want)
			}
			switch _, bodyGated := bodyGatedRoutes[pattern]; {
			case malformed.status >= http.StatusInternalServerError || unknown.status >= http.StatusInternalServerError:
				report("no 5xx")
			case malformed.requestID == "":
				report("a request_id in the envelope")
			case bodyGated:
			case (method == http.MethodGet || method == http.MethodDelete) && othersReal:
				if malformed.status != unknown.status || malformed.code != unknown.code {
					report("the same answer")
				}
			case method == http.MethodGet || method == http.MethodDelete:
				// An unknown parent answers first for a well-formed id; the
				// malformed one is refused as itself. Both are a 404.
				if malformed.status != unknown.status || (malformed.status == http.StatusNotFound && !strings.HasSuffix(malformed.code, "_NOT_FOUND")) {
					report("the same status")
				}
			default:
				if malformed.status != http.StatusNotFound || !strings.HasSuffix(malformed.code, "_NOT_FOUND") {
					report("404 *_NOT_FOUND")
				}
			}
		}
	}
	if tested < 100 {
		t.Fatalf("swept only %d route wildcards; the route table walk has gone wrong", tested)
	}
	if len(failures) > 0 {
		t.Fatalf("%d of %d path ids answered wrongly:\n%s", len(failures), tested, strings.Join(failures, "\n"))
	}
}

// The report that opened this: the Holder List and the Holder Export answered a
// malformed Event id with 500 INTERNAL_ERROR. Both now answer it exactly as an
// Event id that names nothing, message and all.
func TestAMalformedEventIDOnTheHolderListAndExportIsEventNotFound(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)

	for _, route := range []struct {
		name string
		path func(eventID string) string
	}{
		{"holder list", holderListPath},
		{"holder export", holderExportPath},
	} {
		t.Run(route.name, func(t *testing.T) {
			resp, unknown := env.get(t, route.path(uuid.NewString()), authHeader(sessionID))
			assertAPIError(t, resp, unknown, http.StatusNotFound, "EVENT_NOT_FOUND")

			resp, malformed := env.get(t, route.path("not-a-uuid"), authHeader(sessionID))
			assertAPIError(t, resp, malformed, http.StatusNotFound, "EVENT_NOT_FOUND")
			if malformed.Error.Message != unknown.Error.Message {
				t.Fatalf("malformed message %q, want the unknown Event's %q", malformed.Error.Message, unknown.Error.Message)
			}
		})
	}
}

// The path is looked at only once the gate has admitted the caller: without a
// Session a malformed id is 401 as any other request is, and a Member the gate
// refuses hears 403, not whether the id could have named something.
func TestAMalformedPathIDIsRefusedOnlyAfterTheGate(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)

	resp, body := env.get(t, holderListPath("not-a-uuid"), nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "UNAUTHORIZED")

	resp, body = env.get(t, "/api/v1/operator/organizations/not-a-uuid", authHeader(orgAdminSession(t, env)))
	assertAPIError(t, resp, body, http.StatusForbidden, "FORBIDDEN")
}
