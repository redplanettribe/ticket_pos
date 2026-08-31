package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The two acceptance browsers' HTTP seam (#565, parent #556, ADR 0067).
//
// BOTH ARE POSTS THAT READ NOTHING AND WRITE NOTHING, which is unusual enough
// to state plainly: a POST here creates no resource, records no act and changes
// no state. It is a read whose PARAMETERS MUST NOT TRAVEL IN A URL.
//
// #565's rule is that a data subject's email address must never appear in a
// URL, a query string or a referer — reading about somebody must not leak them
// into an access log — and TWO of this screen's parameters are addresses. The
// obvious one is the search fragment. The other is the CURSOR: paging is keyset
// on `email ASC`, so the cursor IS the last address of the previous page, and
// `?cursor=YW5hQGV4YW1wbGUuY29t` is an email in a query string wearing a hat.
// Base64 is an encoding, not a disguise, and it would be logged by the reverse
// proxy, kept in the browser's history, and sent on in the Referer header of
// whatever the operator clicked next.
//
// The staff app's operator lookups USED to put an address in a path — GET
// /api/v1/operator/customers/{email}/consent — and that is the surface these
// screens were built to replace for browsing. A new screen was not going to
// inherit it, and #566 finished the job: that route and its withdrawal are
// deleted, so no request line on this platform carries a data subject's address
// any more.

// legalAcceptanceBrowseBody is one page request. Everything the screen asks
// for, in a body.
type legalAcceptanceBrowseBody struct {
	// Standing is `current`, `outstanding`, `never_seen` or (staff only)
	// `former`. ABSENT MEANS OUTSTANDING — both screens default to it, because
	// "who owes something" is the question the feature exists to answer.
	Standing string `json:"standing"`
	// Cursor is the previous page's `next_cursor`. Unparseable is treated as
	// ABSENT and serves the first page.
	Cursor string `json:"cursor"`
	// SearchEmail narrows to addresses containing this fragment.
	SearchEmail string `json:"search_email"`
}

// BrowseCustomerAcceptances returns one page of the customer acceptance
// browser.
//
// @Summary      Browse Customers by where they stand against a legal document
// @Description  Returns ONE KEYSET PAGE of Customers ordered by email ascending, filtered by where they stand against one document's gate (#565, spec #556, ADR 0067). `document` in the path is `policy` or `terms`; anything else is 404 LEGAL_DOCUMENT_NOT_FOUND. The body carries `standing` (`current`, `outstanding` or `never_seen`; ABSENT MEANS `outstanding`, which is the screen's default and the question it exists to answer), `cursor` (the previous page's `next_cursor`; an unparseable one is treated as ABSENT and serves the first page) and `search_email` (a fragment, matched case-insensitively anywhere in the address). `former` is refused here with 400 LEGAL_STANDING_NOT_AVAILABLE — a Customer record is never deleted, so there is no departure to observe — and an unrecognised state is 400 LEGAL_STANDING_UNKNOWN rather than being silently widened. IT IS A POST THAT READS: nothing is created, recorded or changed. The parameters travel in a BODY because two of them are email addresses — the search fragment, and the CURSOR, which under keyset paging on `email ASC` is the last address of the previous page — and a data subject's address must never reach a URL, a query string, a referer or an access log. Each row is ONE PERSON with TWO STATUS COLUMNS, so somebody who owes the Terms and not the Policy is one row rather than two, plus their `customer_id` (opaque; the per-subject record is reached by it) and their name and address IN THE BODY. There is deliberately NO optional-consent column — a filterable roster with a marketing-consent column is a segmentation tool — NO per-edition filter, NO `total` and NO export of any kind. Page size is 50 and is not client-settable. The response is `{rows, next_cursor}` and NOT the ADR-0006 `{data, pagination}` envelope: ADR 0067 records the departure, because after a gating publication the outstanding set is the entire customer base, so deep offsets go quadratic exactly when the screen matters most, and the total is the expensive half of the query and the least actionable number on it. `next_cursor` null means this is the last page. Standing is MEMBERSHIP of the satisfying set and never equality with the current edition (#560), so publishing a CORRECTION moves nobody into `outstanding`. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string                       true   "policy or terms"
// @Param        body      body  legalAcceptanceBrowseBody    false  "Filter, cursor and search fragment"
// @Success      200  {object}  openapi.EnvelopeOperatorCustomerAcceptancePage
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/acceptances/customers/{document} [post]
func (h *Handler) BrowseCustomerAcceptances(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	body, ok := decodeBrowseBody(w, r, reqID)
	if !ok {
		return
	}

	page, err := h.svc.BrowseCustomerAcceptances(r.Context(), service.LegalAcceptanceBrowseInput{
		Document:    r.PathValue("document"),
		Standing:    body.Standing,
		Cursor:      body.Cursor,
		SearchEmail: body.SearchEmail,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}

// BrowseStaffAcceptances returns one page of the staff acceptance browser.
//
// @Summary      Browse Staff platform people by where they stand against the Terms
// @Description  Returns ONE KEYSET PAGE of everybody who signs into the Staff platform, ordered by email ascending, filtered by where they stand against the Términos y Condiciones (#565, spec #556, ADR 0067). The population is `SELECT email FROM members UNION SELECT email FROM platform_operators` — one row per person, deduplicated by email, with the org-less Platform Operator arriving through the second arm. `document` in the path is `terms` and nothing else: there is exactly ONE staff gate (§3, ADR 0066), staff accept no Privacy Policy, and `policy` here is 404 LEGAL_DOCUMENT_NOT_FOUND rather than an empty list. FOUR STATES: `current`, `outstanding`, `never_seen`, and `former` — somebody who accepted and is now on neither membership table, COMPUTED AND NEVER STORED, a filter value rather than a hidden state, because a leaver owes nothing and listing them among the outstanding would fill the default filter with people nobody can chase. ABSENT `standing` means `outstanding`. Each row carries a `digest` — 32 hex chars of HMAC-SHA256 over "staff:" + the normalised address, under a purpose-derived subkey of the deployment's link secret (ADR 0046) — which is how the per-subject record is linked to, because a staff person has no id and their address must never reach a URL. THE DIGEST IS A URL KEY AND A SCREEN LABEL: it is written to no row, no log, no file and no export. A deployment with no link secret gets 503 STAFF_DIGEST_UNAVAILABLE — the screen REFUSES TO SERVE rather than fall back to an empty key — which is unreachable in production, where the server will not start without one. Same POST-that-reads shape, same `{rows, next_cursor}`, same absent total, same page size of 50, same unparseable-cursor-is-absent rule as the customer browser, and the same correction-re-gates-nobody guarantee (#560). No optional-consent column and no export. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string                       true   "terms"
// @Param        body      body  legalAcceptanceBrowseBody    false  "Filter, cursor and search fragment"
// @Success      200  {object}  openapi.EnvelopeOperatorStaffAcceptancePage
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/acceptances/staff/{document} [post]
func (h *Handler) BrowseStaffAcceptances(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	body, ok := decodeBrowseBody(w, r, reqID)
	if !ok {
		return
	}

	page, err := h.svc.BrowseStaffAcceptances(r.Context(), service.LegalAcceptanceBrowseInput{
		Document:    r.PathValue("document"),
		Standing:    body.Standing,
		Cursor:      body.Cursor,
		SearchEmail: body.SearchEmail,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}

// decodeBrowseBody reads a browse body, treating an EMPTY body as the default
// page: no filter, no cursor, no search.
//
// A screen's first request carries nothing to say, and requiring it to POST
// `{}` would make "give me the default page" a thing a client could get wrong.
// A body that is present but malformed is still refused — that is a client bug
// and not an empty request.
func decodeBrowseBody(w http.ResponseWriter, r *http.Request, reqID string) (legalAcceptanceBrowseBody, bool) {
	var body legalAcceptanceBrowseBody
	if r.Body == nil {
		return body, true
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return body, true
		}
		_ = platform.WriteInvalidJSON(w, reqID)
		return body, false
	}
	return body, true
}
