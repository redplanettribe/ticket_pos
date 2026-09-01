package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The per-subject consent record's HTTP seam (#566, parent #556, ADR 0067).
//
// THREE READS AND ONE ACT, and every one of them addresses a person by
// SOMETHING THAT IS NOT AN EMAIL: a Customer by their opaque UUID, a staff
// person by their Staff Digest. That is the whole reason these routes exist in
// the shape they do — the two they replace, GET and POST on
// /operator/customers/{email}/consent*, put a data subject's address into every
// access log, proxy log, browser history entry and Referer header of whatever
// the operator clicked next. They are deleted in the same change.
//
// THESE ARE PLAIN GETs AND NOT POSTs-THAT-READ, unlike the acceptance browsers
// beside them, and the difference is the same rule reaching a different answer.
// The browsers page by `email ASC`, so their cursor IS somebody's address and
// had to travel in a body. Nothing here is an address: a customer id is opaque,
// a digest is opaque, and the history's cursor is a timestamp and a row id. The
// rule is "no email in a request line", not "no query strings", and a GET that
// leaks nothing is the honest verb for a read.
//
// THE HISTORY IS ITS OWN ENDPOINT rather than a cursor folded into the record
// read, so THE LANDING REQUEST AND PAGE 2 DO NOT RETURN DIFFERENT SHAPES. A
// caller that fetched page two would otherwise be handed the person's identity
// again, or — worse — a record read that omitted it, and "load more" would be
// the one request in the feature that could not be replayed on its own.

// GetCustomerLegalRecord returns one Customer's consent record.
//
// @Summary      Read one Customer's consent record
// @Description  Returns one Customer's identity, where they stand against BOTH legal gates, and the link across to their staff record where the same human being is also on the Staff platform (#566, spec #556, ADR 0067). KEYED ON THE CUSTOMER'S OPAQUE UUID: no email address appears in this request line, or in any other on this path, which is the whole reason this route replaces GET /operator/customers/{email}/consent. The payload identifies the person (id, email, name — in the BODY, where it belongs) and states what is TRUE NOW: for each of the Privacy Policy and the Términos y Condiciones, when it was last accepted, the EXACT EDITION accepted as an id and a label so it resolves to the exact bytes, and the standing that edition produces — `current`, `outstanding` or `never_seen`, computed as MEMBERSHIP of the satisfying set and never as equality with the current edition (#560), so publishing a CORRECTION moves nobody. Never `former`: a Customer record is never deleted. The two optional consents are `granted`, `denied` or `pending_confirmation`, and NULL where the Customer was never asked — null is UNANSWERED and is published as the different fact it is, so nobody is shown a refusal they never made. `staff_digest` is the cross-link, or null where this person is not on the Staff platform; the two records are cross-linked and NEVER MERGED, because there is no row anywhere saying these two are one person, only an address that matches. THE HISTORY IS NOT ON THIS PAYLOAD — it is its own endpoint, so page 1 and page 2 have the same shape. READING WRITES NOTHING: no consent is captured and no Consent Record appears, because a read that recorded something would put an act in the evidence log that nobody performed. An id nobody holds is 404 LEGAL_SUBJECT_NOT_FOUND rather than a blank record somebody might act on. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        customerID  path  string  true  "Customer id (UUID)"
// @Success      200  {object}  openapi.EnvelopeOperatorCustomerLegalRecord
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/customers/{customerID} [get]
func (h *Handler) GetCustomerLegalRecord(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// Who read this record, from the Staff Session and never from a body: the
	// read is recorded in the consent access log (#569).
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	view, err := h.svc.CustomerLegalRecord(r.Context(), session.Email, strings.TrimSpace(r.PathValue("customerID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// ListCustomerConsentRecords returns one page of one Customer's consent acts.
//
// @Summary      Read one page of a Customer's consent history
// @Description  Returns one KEYSET PAGE of one Customer's consent acts, NEWEST FIRST, so the record is the evidence rather than a summary of it (#566, spec #556, ADR 0067). Paged on `(captured_at DESC, id)` at page size 25, served by the existing `(customer_id, captured_at DESC)` index; `limit` may LOWER the page size and can never raise it, and an unreadable one is simply the page size — an evidence log downloaded in one request would be an export by another name, and the export is a deliberate, named act (#568) rather than a query parameter. An unparseable `cursor` is treated as ABSENT and serves the first page, the acceptance browsers' rule: a bad cursor is a stale bookmark, not a mistake worth an error page over somebody's evidence. `visible_count` is HOW MANY ACTS ARE ON THIS PAGE — read with `next_cursor` it answers the one question that matters about a record presented as evidence, "is this all of it?" — and it is deliberately NOT a total: a total over the whole history is the expensive half of the query and ADR 0067 records why this feature does without one. EACH ACT NAMES THE EXACT EDITION it was captured against, as an id and a label, so it resolves to the exact bytes. Every nullable field means something specific and travels as NULL rather than as a blank: a null ANSWER is a box that was NOT SHOWN on that surface and is not a No; a null IP, user agent, session or origin is something the surface did not collect, which is not a blank it collected; `prior_marketing_consent` and `prior_networking_consent` are what each consent was immediately before the act, and are the only way to read whether it took anything away, since `denied` looks identical whether somebody gave something up or refused twice. `presented_locale` (#567) is THREE-STATE: the key is ABSENT ENTIRELY on the channels that present no document — the Customer Area toggles, unsubscribe, the digest, an operator-recorded withdrawal, a passcode withdrawal, and `email_confirmation`, which confirms an earlier act rather than showing new text — because a field rendered there would claim text was displayed and its language forgotten; it is NULL on a channel that did show a document and has no locale recorded, which is every row written before migration 115 and which the screen spells "not recorded"; and otherwise it is the language of the ARTIFACT RENDERED, never of the page it was rendered on. NO CONSENT IS CAPTURED AND NO CONSENT RECORD APPEARS: a read that recorded one would put an act in the evidence log that nobody performed. THE READ ITSELF IS LOGGED, by name, as a `subject_read` in the consent access log (#569) — the subject IS the act here, and this route serves twenty-five of somebody's consent acts to whoever calls it, record open or not. Paging therefore writes a row per page, which is a duplicate in the log and honest; an unlogged read of somebody's data is neither. A FAILURE TO LOG FAILS THE READ. An id nobody holds is 404 LEGAL_SUBJECT_NOT_FOUND — the same refusal the record read gives, so a stale link is answered identically whichever endpoint the screen fires first — and a Customer with no acts is an EMPTY PAGE rather than a 404, because somebody created before the evidence log existed has a record and no history. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        customerID  path   string  true   "Customer id (UUID)"
// @Param        cursor      query  string  false  "The previous page's next_cursor; unparseable is treated as absent"
// @Param        limit       query  int     false  "At most 25, which is also the default; a larger value is capped"
// @Success      200  {object}  openapi.EnvelopeOperatorConsentActPage
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/customers/{customerID}/records [get]
func (h *Handler) ListCustomerConsentRecords(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// Who read this page, from the Staff Session and never from a body: serving
	// somebody's consent acts is a read of their data and is recorded in the
	// consent access log, whichever route it was reached by (#569).
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	page, err := h.svc.CustomerConsentActs(r.Context(), service.CustomerConsentActsInput{
		Actor:      session.Email,
		CustomerID: strings.TrimSpace(r.PathValue("customerID")),
		Cursor:     r.URL.Query().Get("cursor"),
		Limit:      service.ParseConsentActLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}

// GetStaffLegalRecord returns one staff person's acceptance record.
//
// @Summary      Read one staff person's Terms Acceptance record
// @Description  Returns one Staff platform person's complete Terms Acceptance history, found by their STAFF DIGEST (#566, spec #556, ADR 0067). A staff person has no id — the person key of the Staff platform is an email, and migrations 067, 069 and 107 each concluded so independently — and a data subject's address must never appear in a URL, so the link carries 32 hex characters of HMAC-SHA256 over "staff:" + the normalised address, under a purpose-derived subkey of the deployment's link secret (ADR 0046). THE DIGEST IS RESOLVED BY MATCHING AND NOT BY REVERSING: it is one-way by design, so the digest is compared in constant time against every address in the staff population — `members`, `platform_operators` and, so a leaver's record stays reachable, everybody who has ever accepted — until one answers. A digest matching nobody is 404 STAFF_SUBJECT_NOT_FOUND. A deployment with no link secret gets 503 STAFF_DIGEST_UNAVAILABLE: with no key every address produces the same digest and the match would return an arbitrary person's record, which is not a degraded answer but the wrong one. Unreachable in production, where the server will not start without the secret. THE HISTORY IS UNPAGED and `visible_count` is therefore the WHOLE count: a person holds at most one acceptance per edition per capacity, so the history is a few rows and cannot grow without bound the way a Customer's does. Every capacity is listed, not just `organizer`: a record that filtered would hide evidence of an act the person really performed — but the STANDING is computed from `organizer` acceptances alone, exactly as the sign-in gate reads them, so a later capacity can never be misread as clearance for this gate. Standing is `current`, `outstanding`, `never_seen` or `former`, computed as membership of the satisfying set (#560) so a correction moves nobody; `former` is read from the POPULATION — holds an acceptance and is on neither membership table — because a DELETE from `members` is the only place a departure is ever recorded. Each acceptance names the exact edition as an id and a label, carries the capacity, the four technical-proof fields (null where the surface collected nothing, which is not a blank it collected) and `presented_locale`, the language of the acceptance label actually served (#567), null on rows written before migration 115. `customer_id` is the cross-link to the same human being's Customer record, or null; the two records are cross-linked and NEVER MERGED. READING CAPTURES NOTHING: no consent is recorded and no Consent Record appears, because a read that recorded something would put an act in the evidence log that nobody performed. The read itself IS logged, as a `subject_read` naming this person in the Consent Access Log (#569) — a touch of somebody's data is recorded however it is reached. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        digest  path  string  true  "Staff Digest (32 hex characters)"
// @Success      200  {object}  openapi.EnvelopeOperatorStaffLegalRecord
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/staff/{digest} [get]
func (h *Handler) GetStaffLegalRecord(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	view, err := h.svc.StaffLegalRecord(r.Context(), session.Email, r.PathValue("digest"))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
