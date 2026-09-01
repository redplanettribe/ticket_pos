package service

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/identity"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
)

// The two acceptance browsers (#565, parent #556, ADR 0067): the screens on
// which an operator can finally answer "who has not accepted the current
// Terms" without opening psql.
//
// TWO BROWSERS AND NOT ONE, DELIBERATELY. Customers and staff are two
// populations with different keys (a UUID and an email), different column
// counts (two documents and one), and a fourth state that only one of them can
// have. Forcing them through one configuration object would produce a screen
// whose every control had to be read twice — "does this filter apply to me?" —
// and a payload with a nullable half.
//
// This file owns what the SURFACE owns and nothing else: the cursor's encoding,
// the page size, the defaults, and the minting of a Staff Digest on the way
// out. Who is outstanding is consent's rule (#560) and who is staff is
// identity's; neither is decided here.

// LegalBrowserPageSize is how many rows one page carries.
//
// FIFTY, matching the house default (ADR 0006's page_size) so the number is not
// a second thing to remember, and NOT configurable by the caller. A page size a
// client could name is a page size a client could set to 50,000, which on a
// screen with no total and no export would be a roster download with extra
// steps.
const LegalBrowserPageSize = 50

// LegalAcceptanceBrowsers is what the operator surface needs from the two
// modules that own the populations.
//
// Deliberately two reads and NOTHING ELSE. There is no way from here to record
// an acceptance, publish an edition, reach a Consent Record or alter anybody's
// state: the browsers tell an operator who to chase, and every act remains
// per-person on the per-subject record (#566).
type LegalAcceptanceBrowsers interface {
	// BrowseCustomerAcceptances returns one page of Customers, ordered by
	// email ascending, filtered by one document's standing. It answers
	// LEGAL_DOCUMENT_NOT_FOUND and LEGAL_STANDING_NOT_AVAILABLE.
	BrowseCustomerAcceptances(ctx context.Context, query consentsvc.CustomerAcceptanceQuery) ([]consentsvc.CustomerAcceptanceItem, error)
}

// LegalStaffBrowser is the staff population's half, from identity.
type LegalStaffBrowser interface {
	// BrowseStaffAcceptances returns one page of staff, ordered by email
	// ascending, filtered by standing — Former included.
	BrowseStaffAcceptances(ctx context.Context, query identitysvc.StaffAcceptanceQuery) ([]identitysvc.StaffAcceptanceItem, error)
}

// WithLegalAcceptanceBrowsers gives this service the two population reads and
// the Staff Digester that names a staff person in a URL (#565).
//
// TIED ON AFTER CONSTRUCTION, the way WithDocuments is: the digester is derived
// from the deployment's link secret, which server.NewApp resolves, and a build
// without this line composes no browsers at all rather than browsers that
// answer with an unkeyed digest.
func (s *Service) WithLegalAcceptanceBrowsers(customers LegalAcceptanceBrowsers, staff LegalStaffBrowser, digester identity.StaffDigester) *Service {
	s.acceptanceCustomers = customers
	s.acceptanceStaff = staff
	s.staffDigester = digester
	return s
}

// CustomerAcceptancePage is one page of the customer browser on the wire.
//
// NOT the ADR 0006 envelope, and ADR 0067 records the departure. There is no
// `page`, no `page_size`, no `total_pages` and — the load-bearing absence — NO
// `total`.
//
// WHY NO TOTAL. It is the expensive half of the query: a keyset page is 51
// index entries, while COUNT(*) OVER() forces the whole filtered scan the seek
// exists to avoid, and it does so on exactly the screen where the filtered set
// is largest. It is also the least actionable number on the page — an operator
// reading "1,569 people owe an acceptance" does nothing differently from one
// reading "these fifty people owe an acceptance" — and a number that costs the
// most and changes the least is a number to leave out.
//
// WHY NO OFFSET. Immediately after a gating publication the outstanding set is
// the ENTIRE CUSTOMER BASE, so an operator working through it walks to deep
// offsets precisely when the screen matters most, and OFFSET n counts and
// discards n rows per page — quadratic over the walk.
type CustomerAcceptancePage struct {
	Rows []CustomerAcceptanceRow `json:"rows"`
	// NextCursor is the opaque token for the following page, or null when this
	// is the last. Its presence IS the "is there more?" signal; there is
	// nothing else to consult.
	NextCursor *string `json:"next_cursor"`
}

// CustomerAcceptanceRow is one person on the customer browser.
type CustomerAcceptanceRow struct {
	// CustomerID is how the per-subject record is reached (#566). An opaque
	// UUID, so a link out of this row puts no address in a URL.
	CustomerID string `json:"customer_id"`
	// Email is in the BODY. It is the column an operator recognises a person
	// by and it must be shown; what must never happen is its reaching a URL.
	Email string `json:"email"`
	Name  string `json:"name"`
	// PolicyStanding and TermsStanding: TWO STATUS COLUMNS ON ONE ROW, because
	// a person is one human being. Neither is ever `former`.
	PolicyStanding legal.Standing `json:"policy_standing"`
	TermsStanding  legal.Standing `json:"terms_standing"`
}

// StaffAcceptancePage is one page of the staff browser. Same shape, same
// absences, same reasons.
type StaffAcceptancePage struct {
	Rows       []StaffAcceptanceRow `json:"rows"`
	NextCursor *string              `json:"next_cursor"`
}

// StaffAcceptanceRow is one person on the staff browser.
type StaffAcceptanceRow struct {
	// Digest is the 32 hex characters that stand for this person in the link to
	// their per-subject record (#566). A staff person has no id — the person
	// key of the Staff platform is an email — and a data subject's address must
	// never appear in a URL, a query string or a referer.
	//
	// IT IS COMPUTED HERE, ON THE WAY OUT, AND STORED NOWHERE. Not in a column,
	// not in a log line, not in a file, not in an export. Persisting it would
	// turn the key rotation that `legal-staff-digest.v1` exists to make cheap
	// into a data migration over append-only consent evidence.
	Digest string `json:"digest"`
	// Email is in the BODY only, as on the customer row.
	Email string `json:"email"`
	// Standing is `current`, `outstanding`, `never_seen` or `former`.
	Standing legal.Standing `json:"standing"`
}

// LegalAcceptanceBrowseInput is one page request, as it arrives from a POSTED
// BODY.
//
// EVERY FIELD ARRIVES IN A BODY AND NONE IN A QUERY STRING, and the reason goes
// beyond the search term. #565's rule is that a data subject's email must never
// appear in a URL, a query string or a referer — and the CURSOR IS AN EMAIL.
// Keyset paging on `email ASC` seeks from the last address of the previous
// page, so `?cursor=...` would put a base64 of somebody's address into every
// access log, every proxy log, every browser history entry and the Referer
// header of whatever the operator clicked next. Base64 is an encoding and not a
// disguise.
//
// So the whole request is POSTed. A POST that reads is unusual, and it is the
// right unusual thing here: the alternative is a GET that leaks.
type LegalAcceptanceBrowseInput struct {
	// Document is `policy` or `terms` on the customer browser. On the staff
	// browser only `terms` exists — staff accept no Privacy Policy, because the
	// Policy gate is a Customer gate met on Storefront surfaces — and anything
	// else is 404.
	Document string
	// Standing is the state asked for. EMPTY MEANS OUTSTANDING: both browsers
	// default to it, because "who owes something" is the question the feature
	// exists to answer. An unrecognised value is refused rather than widened.
	Standing string
	// Cursor is the opaque token from the previous page's next_cursor. AN
	// UNPARSEABLE CURSOR IS TREATED AS ABSENT and serves the first page, the
	// public event feed's rule (catalog/service.decodeCursor): somebody with a
	// stale bookmark gets the top of the list rather than an error page.
	Cursor string
	// SearchEmail narrows to addresses containing this fragment, so the
	// one-step lookup the old surface gave is not lost.
	SearchEmail string
	// Actor is the operator doing the browsing, taken from the Staff Session
	// and NEVER from the body — the same rule every other attributed act on
	// this platform follows. It is here so that the page can be recorded in the
	// consent access log (#569): browsing a population is a touch of people's
	// data, and a read is the one act that leaves no domain row of its own.
	//
	// IT DOES NOT AFFECT WHAT IS SERVED. The page an operator sees does not
	// depend on who they are; this field only decides whose name is on the log
	// row.
	Actor string
}

// BrowseCustomerAcceptances answers one page of the customer browser.
func (s *Service) BrowseCustomerAcceptances(ctx context.Context, input LegalAcceptanceBrowseInput) (*CustomerAcceptancePage, error) {
	standing, err := parseBrowseStanding(input.Standing)
	if err != nil {
		return nil, err
	}

	items, err := s.acceptanceCustomers.BrowseCustomerAcceptances(ctx, consentsvc.CustomerAcceptanceQuery{
		Document:    strings.TrimSpace(input.Document),
		Standing:    standing,
		CursorEmail: decodeAcceptanceCursor(input.Cursor),
		SearchEmail: normalizeAcceptanceSearch(input.SearchEmail),
		// ONE MORE THAN THE PAGE SIZE, which is the whole of how "is there
		// another page?" is answered without a COUNT: read 51, show 50, and
		// the 51st's existence is the signal. The 51st row is never rendered
		// and never leaves this function.
		Limit: LegalBrowserPageSize + 1,
	})
	if err != nil {
		return nil, err
	}

	page := &CustomerAcceptancePage{Rows: make([]CustomerAcceptanceRow, 0, LegalBrowserPageSize)}
	for i, item := range items {
		if i == LegalBrowserPageSize {
			page.NextCursor = pointerTo(encodeAcceptanceCursor(items[i-1].Email))
			break
		}
		page.Rows = append(page.Rows, CustomerAcceptanceRow{
			CustomerID:     item.ID,
			Email:          item.Email,
			Name:           strings.TrimSpace(item.FirstName + " " + item.LastName),
			PolicyStanding: item.PolicyStanding,
			TermsStanding:  item.TermsStanding,
		})
	}

	// THE QUESTION IS LOGGED AND THE ANSWER IS NOT (#569). What goes into the
	// access log is which population was browsed, which document, which filter,
	// WHETHER a search narrowed it, and how many rows came back — never the
	// rows. A log that recorded the roster would be an unbounded second copy of
	// the list it audits, and the search fragment is an email address, so it
	// stops here: only the boolean travels.
	//
	// AFTER THE PAGE IS BUILT, so the count is the count actually served; and a
	// failure to log fails the read, because a read of somebody's data that was
	// not recorded is the one outcome this feature exists to prevent.
	if err := s.recordListRead(ctx, input.Actor, accessPopulationCustomer, strings.TrimSpace(input.Document),
		string(standing), normalizeAcceptanceSearch(input.SearchEmail) != "", len(page.Rows)); err != nil {
		return nil, err
	}
	return page, nil
}

// The two populations, in migration 116's vocabulary. Constants rather than
// literals at the two call sites, so the customer browser and the staff browser
// cannot come to spell the same word differently in an append-only log.
const (
	accessPopulationCustomer = "customer"
	accessPopulationStaff    = "staff"
)

// BrowseStaffAcceptances answers one page of the staff browser.
//
// IT REFUSES TO SERVE RATHER THAN FALL BACK TO AN EMPTY KEY. If the deployment
// set no CONFIRMATION_LINK_SECRET the digester holds no key, and every row on
// this page would carry a digest computed under a constant that anybody with a
// copy of this source could reproduce — turning the one identifier standing
// between an access log and a staff roster into a reversible one. An empty
// digest would be worse still: fifty rows all naming the same person.
//
// It cannot happen in production, where server.NewApp refuses to start without
// the secret (server.confirmationLinkSecret). THAT CHECK IS IN NewApp AND NOT
// IN platform.LoadConfig, and must stay there: cmd/migrate shares LoadConfig
// and runs with APP_ENV=production and no signing key of any kind, so a check
// moved there fails the deploy before a single migration runs — which it has,
// every time somebody has tried it.
func (s *Service) BrowseStaffAcceptances(ctx context.Context, input LegalAcceptanceBrowseInput) (*StaffAcceptancePage, error) {
	// The document is named on this route too, so both screens are addressed
	// the same way and the path stays total. Only `terms` exists: there is one
	// staff gate and it is the Términos y Condiciones (§3, ADR 0066). A later
	// staff-facing document is another case here, never a loosened check.
	if strings.TrimSpace(input.Document) != consentsvc.LegalDocumentTerms {
		return nil, consent.ErrLegalDocumentNotFound()
	}
	if !s.staffDigester.Configured() {
		return nil, identity.ErrStaffDigestUnavailable()
	}

	standing, err := parseBrowseStanding(input.Standing)
	if err != nil {
		return nil, err
	}

	items, err := s.acceptanceStaff.BrowseStaffAcceptances(ctx, identitysvc.StaffAcceptanceQuery{
		Standing:    standing,
		CursorEmail: decodeAcceptanceCursor(input.Cursor),
		SearchEmail: normalizeAcceptanceSearch(input.SearchEmail),
		Limit:       LegalBrowserPageSize + 1,
	})
	if err != nil {
		return nil, err
	}

	page := &StaffAcceptancePage{Rows: make([]StaffAcceptanceRow, 0, LegalBrowserPageSize)}
	for i, item := range items {
		if i == LegalBrowserPageSize {
			page.NextCursor = pointerTo(encodeAcceptanceCursor(items[i-1].Email))
			break
		}
		digest, ok := s.staffDigester.Digest(item.Email)
		if !ok {
			// Unreachable: the digester was checked above. Refusing the whole
			// page rather than emitting a row with an empty key, because a row
			// whose link names nobody is a row that links to somebody else.
			return nil, identity.ErrStaffDigestUnavailable()
		}
		page.Rows = append(page.Rows, StaffAcceptanceRow{
			Digest:   digest,
			Email:    item.Email,
			Standing: item.Standing,
		})
	}

	// The same row the customer browser writes, over the other population. The
	// DIGESTS ON THIS PAGE ARE NOT LOGGED and neither are the addresses behind
	// them: a `list_read` records the question, and the digest is a URL key
	// written to no row anywhere (#565).
	if err := s.recordListRead(ctx, input.Actor, accessPopulationStaff, consentsvc.LegalDocumentTerms,
		string(standing), normalizeAcceptanceSearch(input.SearchEmail) != "", len(page.Rows)); err != nil {
		return nil, err
	}
	return page, nil
}

// parseBrowseStanding reads the standing filter, defaulting to Outstanding.
//
// EMPTY IS OUTSTANDING AND UNKNOWN IS A REFUSAL, and the two must not be the
// same branch. Defaulting an unrecognised value to Outstanding would hide a
// client bug behind a plausible screen; widening it to "everybody" would answer
// "who owes an acceptance?" with the entire customer base, which on a screen
// with no total looks exactly like a re-gate.
func parseBrowseStanding(raw string) (legal.Standing, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return legal.StandingOutstanding, nil
	}
	standing, ok := legal.ParseStanding(trimmed)
	if !ok {
		return "", consent.ErrLegalStandingUnknown(trimmed)
	}
	return standing, nil
}

// normalizeAcceptanceSearch folds the search fragment the way every stored
// address was folded on the way in (platform.NormalizeEmail is lower+trim), so
// an operator who types an address the way a human writes it still finds the
// person. It is a FRAGMENT and not an address, so it is not run through
// NormalizeEmail itself — that function's contract is about whole addresses.
func normalizeAcceptanceSearch(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// encodeAcceptanceCursor renders the keyset position — the last email of the
// page just served — as an opaque token.
//
// Base64 RawURL, following the public event feed (catalog/service.encodeCursor)
// so there is one cursor encoding in this codebase and not two. IT IS AN
// ENCODING AND NOT A SECRET: anybody can decode it, which is exactly why the
// cursor travels in a POSTED BODY and never in a query string (see
// LegalAcceptanceBrowseInput).
//
// The email ALONE, with no tiebreak column, because the sort key is unique:
// `customers.email` is UNIQUE (migration 016) and the staff population is
// deduplicated by email. A strictly-greater seek on a unique key skips nothing
// and repeats nothing.
func encodeAcceptanceCursor(email string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(email))
}

// decodeAcceptanceCursor reads a cursor back. AN UNPARSEABLE CURSOR IS TREATED
// AS ABSENT — the first page — rather than as an error, which is the public
// event feed's rule and the right one: a bad cursor means a stale bookmark or a
// truncated copy-paste, and the useful answer is the top of the list.
func decodeAcceptanceCursor(cursor string) string {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return ""
	}
	return string(raw)
}

// pointerTo is the one-liner every optional wire field needs. A local helper
// rather than a package-wide one, so it stays where the two callers are.
func pointerTo(value string) *string { return &value }
