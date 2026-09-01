package service

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
)

// THE CONSENT ACCESS LOG AND ITS READER (#569, parent #556, ADR 0067): the
// platform's record of its own reads of people's data, and the one screen it is
// read on.
//
// WHY IT EXISTS AT ALL. Every surface in the Legal Center can be defended by
// pointing at a row: a publication has its provenance columns, a withdrawal has
// its Consent Record, a handover has its `consent_evidence_packs` row. A READ
// HAS NOTHING. Somebody looked at somebody's file, and by the ordinary workings
// of this platform that leaves no trace whatsoever — so the one thing a data
// subject is most likely to ask ("who has looked at this?") is the one thing it
// could not answer. This file is that answer.
//
// FOUR ACTS ARE LOGGED HERE AND FOUR ONLY, and each is logged where it actually
// happens rather than in a middleware that guessed from a URL:
//
//   - `list_read` — a page of either acceptance browser (#565), recorded as THE
//     QUESTION and never the roster.
//   - `subject_read` — one person's record being opened (#566), recorded by
//     name, because there the subject IS the act.
//   - `evidence_export` — a Consent Evidence Pack handed over (#568), by name
//     and with the file's own fingerprint.
//   - `audit_read` — this log being read, because a touch of people's data is
//     recorded however it is reached.
//
// AND THE ACTS DELIBERATELY NOT LOGGED, which took as long to decide: a
// WITHDRAWAL (the Consent Record it writes is the same fact with more of it), a
// PREVIEW (#562 made it a platform.Logger line), and PUBLISH, CORRECT, SCHEDULE
// and CANCEL (provenance columns on the version row, #563 and #564). The rule
// that falls out of those three is the one that makes this table readable: EVERY
// ROW IN IT IS A TOUCH OF SOMEBODY'S DATA. A log that also carried the platform's
// housekeeping would be a log nobody could scan for the thing it exists to show.
//
// THE HISTORY ENDPOINT IS NOT SEPARATELY LOGGED. `GET .../customers/{id}/records`
// is "page two" of a record the operator has already opened — the screen fires
// the record read first, and #566's service refuses the history for an id whose
// record does not exist — so logging both would put two `subject_read` rows in
// the log for one screen and three for an operator who clicked "load more". The
// act being recorded is OPENING SOMEBODY'S RECORD, and it is recorded once.

// LegalAccessLog is what this surface needs from the consent module to record
// its own reads, and to read them back.
//
// FOUR WRITES AND ONE READ, and the shape of the interface is the guarantee:
// there is no Update, no Delete, no Purge and no Export on it, so no future
// caller on this side can reach one — the table's retention is unbounded and
// its reader is the only way out of it (#569). There is also no read that takes
// a subject: filtering the log by the person it is about would be a second way
// to look people up, keyed on the record of people being looked up.
type LegalAccessLog interface {
	RecordConsentListRead(ctx context.Context, actor string, question consentsvc.ConsentAccessListQuestion) error
	RecordConsentAuditRead(ctx context.Context, actor string, question consentsvc.ConsentAccessListQuestion) error
	RecordConsentSubjectRead(ctx context.Context, actor string, subject consentsvc.ConsentAccessSubject) error
	RecordConsentEvidenceExport(ctx context.Context, actor string, subject consentsvc.ConsentAccessSubject) error
	ConsentAccessLog(ctx context.Context, query consentsvc.ConsentAccessQuery) ([]consentsvc.ConsentAccessEntryItem, error)
}

// WithLegalAccessLog gives this service the access log (#569). Tied on after
// construction beside the browsers and the records, following the file it
// audits.
func (s *Service) WithLegalAccessLog(log LegalAccessLog) *Service {
	s.accessLog = log
	return s
}

// LegalAccessLogPageSize is how many entries one page of the reader carries.
// Fifty, the browsers' size and the house default, and not client-settable.
const LegalAccessLogPageSize = consentsvc.ConsentAccessPageSize

// LegalAccessEntryView is one logged act on the wire.
//
// EVERY OPTIONAL FIELD IS NULLABLE AND MEANS "THIS ACT HAS NO SUCH THING",
// never "empty". A `subject_read` has no result count, and a zero rendered in
// its place would be a claim the row does not make.
type LegalAccessEntryView struct {
	// ID is the row's own identifier, a monotonically increasing integer. It is
	// on the wire because the screen needs a stable React key over an
	// append-only list, and because two acts inside one clock tick are
	// otherwise indistinguishable to a reader.
	ID int64 `json:"id"`
	// Act is `list_read`, `subject_read`, `evidence_export` or `audit_read`.
	Act        string    `json:"act"`
	ActorEmail string    `json:"actor_email"`
	OccurredAt time.Time `json:"occurred_at"`

	// The question, on the two acts that asked one.
	Population   *string `json:"population"`
	Document     *string `json:"document"`
	StatusFilter *string `json:"status_filter"`
	// Searched is WHETHER the page was narrowed by a term, and never what the
	// term was. The log has never held one and this payload could not carry one.
	Searched    *bool `json:"searched"`
	ResultCount *int  `json:"result_count"`

	// The subject, on the two acts that named one.
	//
	// SubjectCustomerID is present where the act was reached by a Customer's
	// opaque id, so the screen can link back to the record that was read
	// without putting an address in a URL. SubjectEmail is the person as the
	// row records them — a plain address, never a Staff Digest.
	SubjectCustomerID *string `json:"subject_customer_id"`
	SubjectEmail      *string `json:"subject_email"`

	// PackSHA256 is the fingerprint of the file handed over, on an
	// `evidence_export`: what resolves a ZIP in somebody's mailbox to the act
	// that produced it.
	PackSHA256 *string `json:"pack_sha256"`
}

// LegalAccessLogPage is one page of the reader.
//
// The browsers' envelope and ADR 0067's departure, for its reasons: no `total`,
// no offset, and `next_cursor` null on the last page. A total over an
// append-only log that is never purged is the most expensive and least
// actionable number the screen could carry.
type LegalAccessLogPage struct {
	Entries    []LegalAccessEntryView `json:"entries"`
	NextCursor *string                `json:"next_cursor"`
}

// LegalAccessLogInput is one page request.
//
// ACTOR, ACT AND DATE — and there is no fourth field, because there is no
// subject filter and there is not going to be one. The audit log must not
// become a second way to look people up: the operator who could use it already
// has the acceptance browsers, and a subject filter here would let the record of
// somebody being looked at be searched by their name.
type LegalAccessLogInput struct {
	// Actor narrows to one operator's acts, empty for everybody. An EXACT
	// address and not a fragment: the operator allowlist is a handful of people
	// picked from a list, and a fragment match would be a search box over
	// addresses on the screen whose entire subject is not storing addresses.
	Actor string
	// Act narrows to one of the four. An unrecognised value is REFUSED rather
	// than widened to "everything": silently ignoring it would show an operator
	// a full log they believe is filtered, which on an audit screen is the
	// wrong kind of wrong.
	Act string
	// From and To are calendar days (YYYY-MM-DD) as the screen sends them, or
	// empty. TO IS INCLUSIVE OF ITS WHOLE DAY — see parseAccessLogDay.
	From string
	To   string
	// Cursor is the previous page's next_cursor. AN UNPARSEABLE CURSOR IS
	// TREATED AS ABSENT and serves the first page, the rule everywhere else on
	// this path: a bad cursor is a stale bookmark.
	Cursor string
}

// LegalAccessLog answers one page of the log, and then records that it did.
//
// THE `audit_read` ROW IS WRITTEN AFTER THE PAGE IS BUILT, which is not an
// implementation detail: written first, a read would appear in its own results
// and every first page would be about itself, and the count on the row could
// only be a guess. Written after, the row says exactly what was shown.
//
// A FAILURE TO LOG FAILS THE READ. The alternative — serve the page and swallow
// the error — is a read of somebody's data that happened and was not recorded,
// which is the single thing this feature exists to prevent; and it would fail
// silently, so nobody would learn the log had stopped working until they needed
// it. The operator sees an error and reloads.
func (s *Service) LegalAccessLog(ctx context.Context, actor string, input LegalAccessLogInput) (*LegalAccessLogPage, error) {
	act, err := parseAccessLogAct(input.Act)
	if err != nil {
		return nil, err
	}
	from, err := parseAccessLogDay(input.From, false)
	if err != nil {
		return nil, err
	}
	// THE `to` BOUND IS THE START OF THE NEXT DAY and the query compares with
	// `<`. An operator asking for "up to the 3rd" means the 3rd included; a
	// naive `occurred_at <= '2026-09-03'` would silently mean "up to midnight
	// at the start of it" and hide a whole day of acts on an audit screen.
	to, err := parseAccessLogDay(input.To, true)
	if err != nil {
		return nil, err
	}

	items, err := s.accessLog.ConsentAccessLog(ctx, consentsvc.ConsentAccessQuery{
		ActorEmail: strings.ToLower(strings.TrimSpace(input.Actor)),
		Act:        act,
		From:       from,
		To:         to,
		After:      decodeAccessLogCursor(input.Cursor),
		// One more than the page size — the browsers' trick — so "is there
		// another page?" is answered without a COUNT. The extra row is never
		// rendered and never leaves this function.
		Limit: LegalAccessLogPageSize + 1,
	})
	if err != nil {
		return nil, err
	}

	page := &LegalAccessLogPage{Entries: make([]LegalAccessEntryView, 0, LegalAccessLogPageSize)}
	for i, item := range items {
		if i == LegalAccessLogPageSize {
			page.NextCursor = pointerTo(encodeAccessLogCursor(items[i-1]))
			break
		}
		page.Entries = append(page.Entries, LegalAccessEntryView{
			ID:                item.ID,
			Act:               item.Act,
			ActorEmail:        item.ActorEmail,
			OccurredAt:        item.OccurredAt,
			Population:        item.Population,
			Document:          item.Document,
			StatusFilter:      item.StatusFilter,
			Searched:          item.Searched,
			ResultCount:       item.ResultCount,
			SubjectCustomerID: item.SubjectCustomerID,
			SubjectEmail:      item.SubjectEmail,
			PackSHA256:        item.PackSHA256,
		})
	}

	if err := s.accessLog.RecordConsentAuditRead(ctx, actor, consentsvc.ConsentAccessListQuestion{
		// The act filter is recorded because it says what the reader was
		// looking for; the ACTOR filter is recorded only as `searched`, since
		// it is an address and this log does not accumulate addresses — not
		// even operators' own, and not even on the row that says who was
		// reading. The DATE RANGE is not recorded either: it is a window on the
		// same rows, and a row per window would make the log about its own
		// browsing rather than about people's data.
		StatusFilter: act,
		Searched:     strings.TrimSpace(input.Actor) != "",
		ResultCount:  len(page.Entries),
	}); err != nil {
		return nil, err
	}
	return page, nil
}

// recordListRead logs one page of an acceptance browser.
//
// A HELPER RATHER THAN A LINE IN EACH BROWSER, so the two screens cannot come to
// record the same act differently — and so "the roster is never written" is a
// property of one function instead of two call sites.
//
// NIL LOG IS A COMPOSITION THAT HAS NONE. server.NewApp always ties one on; a
// unit test that builds a bare Service does not, and that build audits nothing
// because it also serves nobody.
func (s *Service) recordListRead(ctx context.Context, actor, population, document, standing string, searched bool, count int) error {
	if s.accessLog == nil {
		return nil
	}
	return s.accessLog.RecordConsentListRead(ctx, actor, consentsvc.ConsentAccessListQuestion{
		Population:   population,
		Document:     document,
		StatusFilter: standing,
		// WHETHER, NEVER WHAT. The fragment an operator typed is an email
		// address, and it stops at this boundary: no signature below this point
		// can carry it.
		Searched:    searched,
		ResultCount: count,
	})
}

// recordSubjectRead logs that one person's record was opened.
func (s *Service) recordSubjectRead(ctx context.Context, actor, customerID, email string) error {
	if s.accessLog == nil {
		return nil
	}
	return s.accessLog.RecordConsentSubjectRead(ctx, actor, consentsvc.ConsentAccessSubject{
		CustomerID: customerID,
		Email:      email,
	})
}

// recordEvidenceExport logs that a pack was handed over, with its fingerprint.
func (s *Service) recordEvidenceExport(ctx context.Context, actor, customerID, email, sha256 string) error {
	if s.accessLog == nil {
		return nil
	}
	return s.accessLog.RecordConsentEvidenceExport(ctx, actor, consentsvc.ConsentAccessSubject{
		CustomerID: customerID,
		Email:      email,
		PackSHA256: sha256,
	})
}

// parseAccessLogAct reads the act filter. Empty is "every act"; anything else
// must be one of the four.
//
// AN UNRECOGNISED ACT IS REFUSED AND NEVER WIDENED, unlike the cursor beside it.
// A stale cursor is a bookmark and the useful answer is the top of the list; a
// filter value the server does not know is a client that believes it is showing
// a narrowed log, and showing them the whole thing under that belief is how an
// audit screen tells somebody a lie.
func parseAccessLogAct(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	switch consentsvc.ConsentAccessAct(trimmed) {
	case "":
		return "", nil
	case consentsvc.ConsentAccessListRead,
		consentsvc.ConsentAccessSubjectRead,
		consentsvc.ConsentAccessEvidenceExport,
		consentsvc.ConsentAccessAuditRead:
		return trimmed, nil
	default:
		return "", consent.ErrLegalAccessActUnknown(trimmed)
	}
}

// accessLogDayFormat is the calendar day the two date filters are named in,
// following the Payout's own date parameter: a day, not an instant, because
// "what happened on the 3rd" is the question an operator asks.
const accessLogDayFormat = "2006-01-02"

// parseAccessLogDay reads one end of the date range.
//
// EXCLUSIVE END, AND THE WHOLE DAY IS IN THE RANGE. `to` is turned into the
// START OF THE FOLLOWING DAY and the query compares with `<`, so an operator
// who asks for "the 1st to the 3rd" gets everything through the end of the 3rd.
// The obvious alternative, `occurred_at <= '2026-09-03'`, means midnight at the
// START of the 3rd and would hide a whole day of acts — on most screens an
// annoyance, on an audit log a wrong answer.
//
// UTC, because the column is TIMESTAMPTZ and there is no per-operator timezone
// anywhere on this platform to read one from. An empty string is an unbounded
// end and not an error: half a range is an ordinary thing to ask for.
func parseAccessLogDay(raw string, endOfDay bool) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, nil
	}
	day, err := time.ParseInLocation(accessLogDayFormat, trimmed, time.UTC)
	if err != nil {
		return time.Time{}, consent.ErrLegalAccessDateInvalid(trimmed)
	}
	if endOfDay {
		return day.AddDate(0, 0, 1), nil
	}
	return day, nil
}

// accessLogCursorSeparator splits the two halves of a keyset position. A
// character that appears in neither an RFC 3339 timestamp nor a decimal id.
const accessLogCursorSeparator = "|"

// encodeAccessLogCursor renders the keyset position — the last entry of the
// page just served — as an opaque token.
//
// Base64 RawURL, following every other cursor in this codebase. It is an
// ENCODING AND NOT A SECRET, and unlike the acceptance browsers' cursor there is
// nothing inside worth hiding: a timestamp and a row id name nobody, which is
// why this one may travel in a query string.
//
// BOTH HALVES, because `occurred_at` is not unique: two acts inside one clock
// tick would be skipped or repeated under a timestamp-only cursor.
func encodeAccessLogCursor(entry consentsvc.ConsentAccessEntryItem) string {
	raw := entry.OccurredAt.UTC().Format(time.RFC3339Nano) + accessLogCursorSeparator + strconv.FormatInt(entry.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeAccessLogCursor reads a cursor back, or nil for the first page. An
// unparseable one is ABSENT rather than an error, the rule everywhere else here.
func decodeAccessLogCursor(cursor string) *consentsvc.ConsentAccessCursor {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return nil
	}
	at, id, found := strings.Cut(string(raw), accessLogCursorSeparator)
	if !found {
		return nil
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil
	}
	rowID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil
	}
	return &consentsvc.ConsentAccessCursor{OccurredAt: occurredAt, ID: rowID}
}
