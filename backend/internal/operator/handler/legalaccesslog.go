package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The consent access log's HTTP seam (#569, parent #556, ADR 0067).
//
// ONE ROUTE. There is no purge, no retention setting, no CSV and no export
// beside it, and the shortness of this file is the feature: the log is read
// where it lives, on one screen, because an audit log that can be downloaded is
// an audit log that can be circulated — and because the acts it records are
// exactly the acts a downloader would be performing.
//
// A PLAIN GET WITH A QUERY STRING, unlike the acceptance browsers' POSTs, and
// the difference is the same rule (#565) reaching a different answer. Theirs
// carry a search fragment and a cursor that IS an email address; nothing here
// is a data subject's address — the ACTOR filter is a platform operator, whose
// address is on an allowlist rather than in the population being audited, the
// act is a vocabulary word, the dates are dates, and the cursor is a timestamp
// and a row id. The rule is no data subject in a request line, not "no query
// strings".
//
// AND THERE IS NO SUBJECT PARAMETER, on this route or any other. Filtering the
// log by the person it is about would make it a second way to look people up —
// keyed on the record of people being looked up, and available to precisely the
// role whose looking it exists to record.

// GetLegalAccessLog returns one page of the consent access log.
//
// @Summary      Read the consent access log
// @Description  Returns ONE KEYSET PAGE of the platform's own reads of people's consent data, NEWEST FIRST (#569, spec #556, ADR 0067). FOUR ACTS ARE RECORDED: `list_read` (a page of either acceptance browser), `subject_read` (one person's record opened), `evidence_export` (a Consent Evidence Pack handed over) and `audit_read` (this log being read — a touch of people's data is recorded however it is reached, and the reader of the log is not exempt from it). Each row carries the ACTOR, taken from the Staff Session and never from a body, and when it happened. A `list_read` records THE QUESTION AND NEVER THE ROSTER — population, document, filter, how many rows came back, and `searched`, which is a BOOLEAN so that looking one person up cannot deposit their address in an audit log. A `subject_read` and an `evidence_export` record the subject BY NAME, because there the subject is the act, and an export also records `pack_sha256`, the fingerprint that resolves a ZIP in somebody's mailbox to the act that produced it. A STAFF SUBJECT IS RECORDED AS A PLAIN ADDRESS AND NEVER AS THEIR STAFF DIGEST: the digest depends on a rotatable key, and a rotation must not orphan a log meant to stay meaningful for years. WHAT IS DELIBERATELY ABSENT MATTERS AS MUCH: a Consent Withdrawal writes NO row (the Consent Record it produces is the same fact with more of it), a preview writes NO row (#562 made it a log line), and publishing, correcting, scheduling and cancelling an edition write NO rows — they are provenance columns on the version row, so provenance cannot drift from the edition it describes. Every row here is therefore a touch of somebody's data and nothing else. FILTERS ARE ACTOR, ACT AND DATE. `actor` is an exact address; `act` must be one of the four or 400 LEGAL_ACCESS_ACT_UNKNOWN — an unrecognised filter is refused rather than widened, because a screen that says it is narrowed while showing everything is a lie about what happened; `from` and `to` are calendar days (YYYY-MM-DD) in UTC or 400 LEGAL_ACCESS_DATE_INVALID, and `to` INCLUDES ITS WHOLE DAY. THERE IS NO SUBJECT FILTER and there will not be one: the audit log must not become a second way to look people up. An unparseable `cursor` is treated as ABSENT and serves the first page. The response is `{entries, next_cursor}` and NOT the ADR-0006 envelope, following the acceptance browsers: no total, no offset, page size 50 and not client-settable. RETENTION IS UNBOUNDED — there is no purge, no retention window and no archival — and THERE IS NO EXPORT: the log is read where it lives. Reading it writes an `audit_read` row AFTER the page is served, so a read never appears in its own results and the recorded count is the count that was actually shown. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        actor   query  string  false  "One operator's acts; an exact address"
// @Param        act     query  string  false  "list_read, subject_read, evidence_export or audit_read"
// @Param        from    query  string  false  "Earliest calendar day, YYYY-MM-DD (UTC), inclusive"
// @Param        to      query  string  false  "Latest calendar day, YYYY-MM-DD (UTC), inclusive of the whole day"
// @Param        cursor  query  string  false  "The previous page's next_cursor; unparseable is treated as absent"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalAccessLogPage
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/access-log [get]
func (h *Handler) GetLegalAccessLog(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// Who is reading the log, from the Staff Session: this read is logged too.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	query := r.URL.Query()
	page, err := h.svc.LegalAccessLog(r.Context(), session.Email, service.LegalAccessLogInput{
		Actor:  query.Get("actor"),
		Act:    query.Get("act"),
		From:   query.Get("from"),
		To:     query.Get("to"),
		Cursor: query.Get("cursor"),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}
