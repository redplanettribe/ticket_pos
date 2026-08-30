package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Paging for the Outstanding Answers list (#313).
//
// The same defaults and the same clamp as the Sales list (ADR-0006), because
// this is the same kind of screen read by the same people, and a list that paged
// fifty here and twenty-five there would be a difference nobody could explain.
const (
	outstandingDefaultPageSize = 50
	outstandingMaxPageSize     = 100
)

// outstandingPageParam floors the page at 1. A hand-edited or stale shared URL
// stays usable rather than erroring: there is nothing a caller could do about a
// `page=0` refusal except send 1, so this sends 1.
func outstandingPageParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// outstandingPageSizeParam defaults to 50 and clamps to [1, 100]. A value above
// the maximum is clamped DOWN rather than refused, so a caller asking for
// everything gets as much as this platform will serve instead of an error.
func outstandingPageSizeParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return outstandingDefaultPageSize
	}
	if n > outstandingMaxPageSize {
		return outstandingMaxPageSize
	}
	return n
}

// outstandingFilterParam reads the Outstanding Answers filter (#333): `true`
// narrows the Holder List to the Tickets that still owe a required Answer, and
// anything else — including absence, and including a hand-mangled value — is
// the whole roster. Lenient for the pagers' reason: there is nothing a caller
// could do about a refusal of `outstanding=yes` except send the roster request
// they were one typo away from.
func outstandingFilterParam(raw string) bool {
	return strings.TrimSpace(raw) == "true"
}

// holderChannels is what a Sales Channel can be, and the whole of it. Stated
// here rather than trusted from the URL because the value reaches an equality
// on `s.channel`: an unlisted word would not be a security problem — it is a
// bound parameter, not a fragment — but it would be a filter that silently
// matches nothing, which reads to an Organizer as "this Event sold nothing".
// Better to hand back the whole roster and let them see it did.
var holderChannels = []string{"online", "in_person", "import"}

// holderChannelParam reads the Sales Channel filter, IGNORING anything that is
// not one of the three. See holderDateParam for the argument; this is the same
// decision on an enum.
func holderChannelParam(raw string) string {
	value := strings.TrimSpace(raw)
	for _, channel := range holderChannels {
		if value == channel {
			return value
		}
	}
	return ""
}

// holderAssignmentStates is what the assignment-state filter may select, and
// the whole of it: the three states catalog.AssignmentState knows, plus
// `never_accepted` (#524, ADR 0065).
//
// `never_accepted` IS A VALUE HERE AND NOT A FOURTH STATE. It is the
// presentation the Holder List already derives at read time from the retention
// purge's marker — somebody was named, nobody claimed the Ticket — and #331
// rejected making it a state outright. Nothing about how it is derived changes;
// this list only makes it selectable, because "who did I name who never claimed
// their ticket" is the morning-after question and after the Event has started
// it otherwise reads as "nobody was named".
//
// STATED HERE AND ALSO IN THE REPOSITORY, which is a second copy of four words
// and is worth it: this one keeps an unlisted value out of the service
// entirely, and the repository's is what turns a value into a predicate. They
// cannot silently disagree — a value listed here and missing there is
// unfiltered, which the partition test notices.
var holderAssignmentStates = []string{"unassigned", "assigned", "accepted", "never_accepted"}

// holderAssignmentStateParam reads the assignment-state filter, IGNORING
// anything that is not one of the four. Same decision on an enum as
// holderChannelParam, for holderDateParam's reasons.
func holderAssignmentStateParam(raw string) string {
	value := strings.TrimSpace(raw)
	for _, state := range holderAssignmentStates {
		if value == state {
			return value
		}
	}
	return ""
}

// holderTicketTypeParam reads the Ticket Type filter, ignoring anything that is
// not a well-formed id.
//
// THE UUID CHECK IS NOT PEDANTRY. The column is `uuid NOT NULL`, so a malformed
// value reaching the comparison is an error from Postgres and a 500 on a screen
// — a hand-edited URL turning into a server fault. Dropping it here makes the
// same URL a roster.
//
// A well-formed id belonging to somebody else's Event is NOT checked and does
// not need to be: the query is already scoped to this Organization and this
// Event, so such a filter matches nothing and discloses nothing — it cannot even
// distinguish an id that exists elsewhere from one that exists nowhere.
func holderTicketTypeParam(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := uuid.Parse(value); err != nil {
		return ""
	}
	return value
}

// holderQuestionParam reads the named-question filter (#525), ignoring anything
// that is not a well-formed id.
//
// THE UUID CHECK IS holderTicketTypeParam's, FOR ITS REASON: `ticket_questions.id`
// is a `uuid`, so a hand-edited URL reaching the comparison would be a Postgres
// error and a 500 on a screen. Dropping it here makes the same URL a roster.
//
// A well-formed id belonging to another Organization's Event is not checked and
// needs no check: the sub-query it reaches is scoped to this Event and this
// Organization, so such a filter matches nothing and cannot even distinguish an
// id that exists elsewhere from one that exists nowhere.
//
// AND NOTHING HERE ASKS WHETHER THE QUESTION IS OWED BY ANYBODY — not whether
// it is required, not whether it has been retired, not whether the Operator
// approved it. That is the debt's definition, it lives once in
// catalog.IsOutstandingAnswer and in the SQL beside it, and a handler screening
// on any part of it would be the second statement of the rule this ticket
// exists to avoid. Naming a question nobody owes returns an empty roster, which
// is the truthful answer: nobody owes it.
func holderQuestionParam(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := uuid.Parse(value); err != nil {
		return ""
	}
	return value
}

// holderSearchParam reads the search term, trimmed and otherwise UNTOUCHED
// (#526).
//
// NOTHING IS VALIDATED AND NOTHING IS REJECTED. A search term is free text: an
// address, half a name, a fragment of a Sale Confirmation reference. There is no
// malformed value to screen for, and the term is bound as a query ARGUMENT with
// its LIKE metacharacters escaped one layer down (repository.holderRosterSearch
// and holderRosterLikeEscape), never interpolated into SQL — so `%`, `_` and a
// quote are all literals a reader may type and none of them is a hazard.
//
// TRIMMED so a stray space pasted with an address is not a search that matches
// nothing, and so a box holding only whitespace is the whole roster rather than
// a narrowing nobody asked for.
//
// AND IT IS NEVER LOGGED HERE OR ANYWHERE ON THIS PATH. It matches customer
// addresses, and a log aggregator is a wider audience than the database (ADR
// 0065). The request middleware logs `r.URL.Path` and not the query string,
// which is what keeps that true today; a handler-level log added later must
// redact this parameter.
func holderSearchParam(raw string) string {
	return strings.TrimSpace(raw)
}

// holderSorts is what the Holder List may be ordered by, and the whole of it
// (#527, ADR 0065): when the sale happened, the buyer, who is coming, the Ticket
// Type in the Event's catalog display order, and how much the Ticket owes.
//
// STATED HERE AND ALSO IN THE REPOSITORY, exactly as holderAssignmentStates is,
// and here the second copy earns more than it usually does: a sort key is the
// ONE parameter on this list that is interpolated into SQL rather than bound as
// an argument, because an ORDER BY cannot be a placeholder. This list keeps an
// unlisted word out of the service entirely; repository.holderSortColumns is a
// second allowlist that turns a key into columns and falls back to the default
// for anything it does not know. Either alone would be enough; both is what
// makes "a value from the URL never reaches the query" true by construction
// rather than by review.
var holderSorts = []string{"sold_at", "buyer", "holder", "ticket_type", "owes"}

// holderSortParam reads the sort key, IGNORING anything that is not one of the
// five — including `owes` never being screened against the Ticket Question flag
// here, which the handler cannot see and has no business knowing (ADR 0045). The
// service drops that one while the feature is dark, beside `outstanding` and
// `question_id`, and the roster comes back in the default order.
//
// Ignoring rather than refusing, on holderDateParam's argument below: a stale
// bookmark naming a sort that has been renamed is a roster in the default order,
// not an error page.
func holderSortParam(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, sort := range holderSorts {
		if value == sort {
			return value
		}
	}
	return ""
}

// holderDirParam reads the direction, IGNORING anything that is not `asc` or
// `desc`. Blank is the list's own default — ASCENDING, oldest sale first — and
// deliberately not the Sales list's descending: a ledger is watched from the
// newest end and a roster is worked through from the oldest.
func holderDirParam(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "asc" || value == "desc" {
		return value
	}
	return ""
}

// holderDateParam reads one calendar-date bound ("YYYY-MM-DD"), IGNORING
// anything else.
//
// THIS SURFACE IGNORES AN UNUSABLE FILTER; IT DOES NOT REFUSE ONE — and that is
// a deliberate departure from the Sales list next door, which answers a
// malformed `sold_from` with a VALIDATION_FAILED 400 (see the sales handler's
// validateDate). The argument, ticket by ticket:
//
//   - CONSISTENCY WITH THIS SURFACE BEATS CONSISTENCY WITH THAT ONE. Every
//     parser above is already lenient, and each says why in the same words:
//     there is nothing a caller could do about a refusal of `page=0` or
//     `outstanding=yes` except send the request they were one typo away from. A
//     Holder List that shrugged at four params and 400'd on the fifth would be
//     arbitrary, and the arbitrariness would land on the reader as an error page
//     from a screen that was working a moment ago.
//
//   - ADR 0065 HAS ALREADY RULED THIS WAY ONCE, for a different reason that
//     points the same direction: a filter belonging to a dark feature flag is
//     "ignored, not refused", because a refusal turns a stale bookmark into an
//     error page and forces the client to know a flag ADR 0045 exists to keep it
//     from knowing. #524 generalises that to every unusable filter on this list.
//     A malformed date is the same stale bookmark by another route.
//
//   - THE COST IS REAL AND IS ACCEPTED. An ignored filter shows MORE rows than
//     the reader asked for, and on this particular list the extra rows are
//     attendees' names and addresses. That is the right way round: this is a
//     read, and a roster wider than intended is a screen the reader can see is
//     wrong — the filter bar shows the date field empty — whereas a 400 hides
//     the roster entirely. It would NOT be the right way round on a write, or on
//     the Holder Export, where the file must describe only the filters actually
//     honoured (ADR 0065) precisely so that nobody reads a whole roster
//     believing it is a filtered one.
//
// The date is returned as the STRING it came as. The service resolves it against
// the Event's own timezone, because a day is only a span of time once you know
// where you are standing.
func holderDateParam(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

// ListHolderList returns the Event's Holder List: every Ticket of the Event,
// who is coming on each, and — where the Event asks Ticket Questions — what
// each still owes (#333; the Outstanding Answers read of #313, widened to the
// roster it was always standing on).
//
// ONE ROUTE AND NOT TWO. The Holder List rides on the read #313 built rather
// than on a second staff endpoint, because both walk the Event's Tickets and an
// Organizer asking "who is coming" is looking at the same list as an Organizer
// asking "who has not told me their size". What #333 changed is what a row IS —
// every Ticket of every live sale — and that Outstanding Answers became the
// `outstanding` FILTER on it rather than the list's definition; #519 then moved
// the path to `/holder-list` so the address says the same thing (ADR 0065). The
// old `/outstanding-answers` path is aliased to this handler for one release
// against deploy skew and is deleted by #531; it is not documented below,
// because a generated client must not learn a path that is about to go.
//
// A READ AND NOTHING ELSE. There is no act on this route and there is no state
// behind it: an Outstanding Answer is DERIVED on every read from what the Ticket
// Type asks and what the Ticket has said, so the debts clear by themselves as
// Answers arrive from any of the three routes — Event Staff, the checkout
// capture, or an Answer Link — and a reversed Ticket Sale's Tickets drop out of
// the roster whole.
//
// @Summary      List an event's holder list
// @Description  A page of the Event's Holder List: EVERY Ticket of every live Ticket Sale, oldest sale first — the Organization's answer to "who is coming". Available while EITHER the Ticket Assignment or the Ticket Question feature flag is open, and 404 only when both are dark. Each row carries the Ticket's `assignment_state` — `unassigned`, `assigned` or `accepted` — and, once a Holder has ACCEPTED, that Holder's own name and email address (ADR 0047). Nothing about a Holder is disclosed before acceptance: an address a buyer typed and its owner never accepted is reported as `assigned` and never named. A Ticket whose unaccepted address the retention purge took reads `assigned` with `never_accepted` beside it, derived at read time from the purge marker — somebody was named and never claimed the Ticket, which after the Event has started is a different fact from nobody having been named; no address travels with it, because the address is gone by definition. All assignment fields are ABSENT while the Ticket Assignment flag is off. Where the Ticket Question feature is open, each row also names the required questions it has not answered, in `outstanding` — an empty array is a Ticket that owes nothing, and stays on the list, because the roster is the point and the questions are a column on it. `outstanding=true` filters the list to the Tickets that still owe — Outstanding Answers is a filter of this list, not its definition. `question_id` narrows to the Tickets owing ONE NAMED Ticket Question, which on an Event asking several is a different chase from owing anything at all; it COMPOSES with `outstanding` rather than replacing it, so `outstanding=true&question_id=X` and `question_id=X` alone mean the same thing — owing X implies owing something. It decides nothing about what is outstanding: it is the same derivation narrowed by the question's id, so a retired question, an optional one and a reversed sale answer to it exactly as they answer to `outstanding`, and naming a question nobody owes returns an empty roster rather than a refusal. Like `outstanding`, it belongs to the Ticket Question feature and is IGNORED while that flag is closed. `assignment_state` narrows the roster to where each Ticket stands with its Holder, and takes FOUR values: `unassigned`, `assigned`, `accepted` and `never_accepted` — the last being a value of this filter and NOT a fourth assignment state, since it selects the Tickets whose unaccepted address the retention purge took (they report `assignment_state: assigned` with `never_accepted: true`). The four values PARTITION the roster: every Ticket matches exactly one, so a purged Ticket answers to `never_accepted` and not to `assigned`. This filter belongs to the Ticket Assignment feature and is IGNORED while that flag is closed — the whole roster comes back with a 200, never a 400, so a stale bookmark carrying it keeps working and no client has to know a flag. `q` searches the roster for a person: a case-insensitive substring over the buyer's name and email address, the Sale Confirmation reference, and — for an ACCEPTED Holder only — that Holder's name and email address, matching the Sales list's search so the two screens do not mean different things by the word. SEARCHABLE IF AND ONLY IF DISPLAYABLE (ADR 0065): an address a buyer typed into a Ticket Assignment and whose owner never accepted matches NOTHING, and neither does one the retention purge has taken. The acceptance condition is part of the search predicate itself and not a filter applied after it, because a search that matched an unaccepted address would answer one address at a time the very question ADR 0047's non-disclosure exists to refuse — type an address, get a row, and the empty Holder cell means yes, they are on this list. The cost is accepted and permanent: an Organizer who typed an address into an assignment cannot later search for it, and must find the row by its buyer or its reference. `q` belongs to NO feature flag — a buyer, an address and a reference are on every roster — and it is never written to any log. Three further filters narrow the roster STRUCTURALLY and compose with it and with each other (#523, ADR 0065): `ticket_type_id` is the VIP roster apart from general admission — a plain equality, because a Ticket belongs to exactly one Ticket Type, unlike a Ticket Sale, which may span several; `channel` is `online`, `in_person` or `import`, so the buyers who came through the door or through an import — and were therefore never asked anything — can be listed on their own; and `sold_from`/`sold_to` are calendar days (`YYYY-MM-DD`) READ IN THE EVENT'S TIMEZONE, inclusive of the whole end day, so "sold in January" means January where the Event is and not where the reader is standing, matching the Sales list. `sort` orders the roster FIVE ways and `dir` reverses each: `sold_at` (the DEFAULT, and it is `asc` — oldest sale first, unchanged, because the roster reads as the chronology of who joined the Event and this screen is already in use), `buyer` (last name then first, matching the Sales list's `customer`), `holder` (the accepted Holder's name), `ticket_type` (THE EVENT'S OWN CATALOG DISPLAY ORDER and deliberately not alphabetical, so Early Bird / General / VIP stays the order an Organization chose, the same `sort_order` the Sales Export's columns and the Trends' legend read) and `owes` (how many required Answers the Ticket still owes, so the worst offenders can be chased first). ROWS WITH NO HOLDER NAME SORT LAST IN BOTH DIRECTIONS under `sort=holder`, which deliberately breaks the convention that descending is the reverse of ascending (ADR 0065): on an Event where most Tickets are unassigned the conventional flip makes one of the two directions useless, opening on hundreds of empty cells. A blank here is a missing name and not only a NULL — an accepted Holder who never named themselves sorts with the blanks. Every sort carries a deterministic tiebreak beneath it, so paging a list never duplicates a Ticket and never loses one; the default order additionally keeps one sale's Tickets together and in ordinal order, as it always has. `owes` belongs to the Ticket Question feature and is IGNORED while that flag is closed — the roster comes back in the DEFAULT order with a 200, direction included, never a 400. An unknown `sort` or `dir` is likewise the default order rather than a refusal. `pagination.total` reflects the FILTERED view, so the page count never promises pages that do not exist. An unusable filter is IGNORED, never refused: a malformed date, an unknown channel or a malformed Ticket Type id leaves that dimension unfiltered, because a stale bookmark should be a wide roster the reader can see is wide, not an error page. There is deliberately NO status filter: a Sale Reversal means the Tickets cease to exist (ADR 0043), so reversed Tickets are not rows being hidden — they are not rows — and no payment-method or source filter either, both being facts about a sale with no roster meaning. `outstanding_count` is the Event's total number of debts across the whole roster, unaffected by the filter, while `pagination.total` counts the Tickets of the current view. Both `outstanding` and `outstanding_count` are absent while the Ticket Question flag is off. Tickets of `in_person` and `import` sales appear beside the `online` ones and start out owing everything, because those buyers were never asked — each row carries its `channel` so that reads as history rather than as loss. Started Events still report the whole roster and its debts, because "who came and who never told us" is what a reader after the fact came to find out. Org Admin and Event Owner only (#521, ADR 0065): one rule for holder data, the same gate the Sales Export carries — its per-Ticket sheet already emits this Event's accepted Holders' names and addresses to an Event Owner. Event Staff are refused; they work the door, and this is the platform's densest concentration of attendee personal data.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id           path      string  true   "Event ID"
// @Param        page         query     int     false  "Page number (default 1)"
// @Param        page_size    query     int     false  "Rows per page (default 50, max 100)"
// @Param        q               query     string  false  "Case-insensitive substring over the buyer's name and email, the Sale Confirmation reference, and — for an ACCEPTED Holder only — that Holder's name and email"
// @Param        outstanding     query     bool    false  "Only the Tickets that still owe a required Answer"
// @Param        question_id     query     string  false  "Only the Tickets owing this one Ticket Question an Answer — composes with `outstanding`, and ignored while Ticket Questions are dark"
// @Param        assignment_state query    string  false  "Only the Tickets in this assignment state (unassigned, assigned, accepted, never_accepted) — ignored while Ticket Assignment is dark"
// @Param        ticket_type_id  query     string  false  "Only the Tickets of this Ticket Type"
// @Param        channel         query     string  false  "Only the Tickets of sales on this Sales Channel (online, in_person, import)"
// @Param        sold_from       query     string  false  "Only the Tickets of sales made on or after this calendar day (YYYY-MM-DD), read in the Event's timezone"
// @Param        sold_to         query     string  false  "Only the Tickets of sales made on or before this calendar day (YYYY-MM-DD), read in the Event's timezone — the whole day is included"
// @Param        sort            query     string  false  "Order the roster by: sold_at (default), buyer, holder, ticket_type (the Event's catalog display order, not alphabetical) or owes — `owes` is ignored while Ticket Questions are dark"  Enums(sold_at, buyer, holder, ticket_type, owes)
// @Param        dir             query     string  false  "Sort direction (default asc — oldest sale first)"  Enums(asc, desc)
// @Success      200  {object}  openapi.EnvelopeHolderList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/holder-list [get]
func (h *Handler) ListHolderList(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}

	// The filters, the search and the sort, read by the SHARED parser this route
	// and the Holder Export both use (#529): the file exists to hand back what
	// this screen was showing, and two readings of one query string would be two
	// chances for the two to disagree. Every filter belonging to a feature flag is
	// parsed unconditionally here and DROPPED BY THE SERVICE while that feature is
	// dark — no flag is read in this layer, because the handler has none and the
	// rule about dark filters is stated once, in honourHolderFilters.
	//
	// Only the PAGE is this route's own: the Holder Export takes the whole answer.
	query := r.URL.Query()
	params := holderListFilterParams(query)
	params.Page = outstandingPageParam(query.Get("page"))
	params.PageSize = outstandingPageSizeParam(query.Get("page_size"))

	result, err := h.svc.ListHolderList(r.Context(), actorFromRequest(r), eventID, params)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// holderListFilterParams reads the Holder List's filters, its search and its
// sort off a query string, with the pagination LEFT OUT.
//
// ONE PARSER FOR THE SCREEN AND THE FILE (#529). The Holder Export exists to
// hand back what the list was showing, and two readings of the same query string
// would be two chances for the file and the screen to disagree about which view
// the reader was looking at. Every leniency argued at length above therefore
// applies identically to the download: a malformed date, an unknown channel or a
// hand-edited sort is IGNORED on both, so a stale bookmark produces a wide file
// rather than an error page — and the Info sheet then says, in words, which
// filters were actually applied, which is what keeps the wide file honest.
//
// PAGINATION IS THE ONE THING NOT CARRIED OVER, and the caller supplies it: a
// file is the whole answer, not a page of it.
func holderListFilterParams(query url.Values) service.ListHolderListParams {
	return service.ListHolderListParams{
		OwingOnly:       outstandingFilterParam(query.Get("outstanding")),
		QuestionID:      holderQuestionParam(query.Get("question_id")),
		Search:          holderSearchParam(query.Get("q")),
		AssignmentState: holderAssignmentStateParam(query.Get("assignment_state")),
		TicketTypeID:    holderTicketTypeParam(query.Get("ticket_type_id")),
		Channel:         holderChannelParam(query.Get("channel")),
		SoldFrom:        holderDateParam(query.Get("sold_from")),
		SoldTo:          holderDateParam(query.Get("sold_to")),
		Sort:            holderSortParam(query.Get("sort")),
		Dir:             holderDirParam(query.Get("dir")),
	}
}

// ExportHolderList returns the Event's Holder List as an .xlsx, narrowed and
// ordered by exactly the same parameters as the list itself.
//
// @Summary      Export an Event's Holder List as a spreadsheet
// @Description  Returns an .xlsx of the Event's Holder List — ONE ROW PER TICKET — reflecting exactly the filters and the sort supplied, so the file matches the screen it was taken from. Accepts the SAME query parameters as the Holder List (q, outstanding, question_id, assignment_state, ticket_type_id, channel, sold_from/sold_to, sort, dir) and parses them with the list's own helper, so the two cannot drift; the pagination parameters are ignored, since a file is the whole answer. An unusable filter is IGNORED rather than refused, exactly as on the list, and so is a filter belonging to a feature flag this deployment has closed. It is a NEW ARTIFACT and not the Sales Export: that file is one row per Ticket SALE with money on it and is completely unchanged, while this one is one row per TICKET and carries NO MONEY AT ALL — no amount, no net proceeds, no currency — so neither can be mistaken for the other or forwarded as a financial document. Columns, left to right: confirmation_ref (the Sale Confirmation reference, to join back onto the Sales Export), sold_at, channel, ticket_type, ticket_ordinal, customer_first_name, customer_last_name, customer_email, then — while Ticket Assignment is open — assignment_state, never_accepted, holder_first_name, holder_last_name, holder_email, then one column per Ticket Question and one TRUE/FALSE column per option where a question takes several. The ordinal is carried here and deliberately not on the Sales Export's per-Ticket sheet: a row here is a Ticket, and the ordinal is the only thing telling two Tickets of one sale line apart. AN UNACCEPTED HOLDER'S ADDRESS IS NOWHERE IN THE FILE (ADR 0047): the rows are built from the same decision the Holder List screen is drawn from, so a Ticket somebody was named for and never accepted exports the word `assigned` and three blank cells, and a Ticket whose unaccepted address the retention purge took exports `assigned` with never_accepted TRUE. Cells are really typed: sold_at is an Excel date cell formatted `yyyy-mm-dd hh:mm` drawn in the Event's timezone, the ordinal is a whole number, never_accepted and each multiple-choice option are real booleans, and a date answer is a real calendar-date cell. The workbook has exactly two sheets. `Info` comes first and is the active sheet on open: it names the Event, the generated-at moment, the timezone named outright as the Event's, the row count, and — in words rather than as query parameters — ONLY THE FILTERS ACTUALLY HONOURED, so a filter this deployment ignored is never described as having been applied and nobody reads a whole roster believing it is a filtered one. It states THAT a free-text search was applied and never the term, which matches customer addresses. The data sheet is named `Ticket Holders` and deliberately not `Sales`: the Sale Import parser selects its sheet by that name, so an export accidentally uploaded as an import fails rather than duplicating every sale. It carries nothing above its header row, so select-all, autofilter and pivot source ranges work without deleting a preamble — which is why the stamp is a sheet of its own. The filename is set by Content-Disposition as `holders-{event-slug}-{YYYY-MM-DD}.xlsx`. Generation is synchronous and the workbook is buffered in memory, so the file is CAPPED at 50,000 Tickets — its own ceiling with its own reason, and deliberately not the Sales Export's cap, which exists to mirror what a Sale Import would take back and does not transfer to a file nobody imports. A request whose filters match MORE than the cap builds nothing and is refused with the standard VALIDATION_FAILED envelope, carrying one field error on `filters` whose message names how many TICKETS matched and how many may be downloaded at once; nothing is ever truncated, and exactly the cap succeeds. One structured log line is written per generated file — the acting Member, Organization, Event, the honoured structural filters, the sort and the row count — because this is the platform's densest concentration of attendee personal data and "who pulled the guest list" cannot be answered retroactively. The search is recorded in it as a boolean only, for the reason it is absent from the file. Restricted to Org Admins and Event Owners, the same gate as the Holder List read; Event Staff are refused. 404 while BOTH the Ticket Assignment and the Ticket Question feature flags are dark, exactly as the list is.
// @Tags         staff
// @Produce      application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security     BearerAuth
// @Param        id               path   string  true   "Event ID"
// @Param        q                query  string  false  "Case-insensitive substring over the buyer's name and email, the Sale Confirmation reference, and — for an ACCEPTED Holder only — that Holder's name and email"
// @Param        outstanding      query  bool    false  "Only the Tickets that still owe a required Answer"
// @Param        question_id      query  string  false  "Only the Tickets owing this one Ticket Question an Answer"
// @Param        assignment_state query  string  false  "Only the Tickets in this assignment state"  Enums(unassigned, assigned, accepted, never_accepted)
// @Param        ticket_type_id   query  string  false  "Only the Tickets of this Ticket Type"
// @Param        channel          query  string  false  "Only the Tickets of sales on this Sales Channel"  Enums(online, in_person, import)
// @Param        sold_from        query  string  false  "Only the Tickets of sales made on or after this calendar day (YYYY-MM-DD), read in the Event's timezone"
// @Param        sold_to          query  string  false  "Only the Tickets of sales made on or before this calendar day (YYYY-MM-DD), read in the Event's timezone — the whole day is included"
// @Param        sort             query  string  false  "Order the roster by (default sold_at)"  Enums(sold_at, buyer, holder, ticket_type, owes)
// @Param        dir              query  string  false  "Sort direction (default asc — oldest sale first)"  Enums(asc, desc)
// @Success      200
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/holder-list/export [get]
func (h *Handler) ExportHolderList(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}

	export, fieldErrs, err := h.svc.ExportHolderList(
		r.Context(), actorFromRequest(r), eventID,
		holderListFilterParams(r.URL.Query()),
	)
	if len(fieldErrs) > 0 {
		// More matching Tickets than one file may carry. It is VALIDATION_FAILED
		// rather than a domain error because the answer is something the caller
		// changes about their request — the filters, which the staff app has on
		// screen beside the button — and the same envelope every other refusal on
		// this API uses means the client has one error path to render, not two.
		_ = platform.WriteValidationError(w, reqID, fieldErrs)
		return
	}
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+export.Filename+"\"")
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(export.Data)
}
