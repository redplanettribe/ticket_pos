package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// Envelope is the standard JSON response shape for all API endpoints.
type Envelope struct {
	Data      any       `json:"data"`
	Error     *APIError `json:"error"`
	RequestID string    `json:"request_id"`
}

// APIError is the standard error object inside the envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// FieldError describes a single validation failure.
//
// Message is the English sentence; Code is the stable machine name of the rule
// that failed (validation_codes.go). Clients key their own copy on Code and fall
// back to Message, which is why Code is omitempty rather than required: a
// validation that has not been given a code yet still renders, in English.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ValidationErrorDetails is the details payload for VALIDATION_FAILED.
type ValidationErrorDetails struct {
	Fields []FieldError `json:"fields"`
}

// NoStore marks a response `Cache-Control: no-store`, so that no cache between
// the API and the reader - a CDN, a proxy, the browser's own - may keep a copy.
// Every download of somebody's personal or tax data sets it before its first
// byte: the Holder and Sales Exports' attendee, buyer and Holder data and
// Ticket Answers (ADR 0075), a Tax Document's buyer name, Tax ID and
// purchase, and the Tax Document Archive's whole period of them.
func NoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

// WriteJSON encodes v as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// WriteSuccess writes a success envelope with the given data payload.
func WriteSuccess(w http.ResponseWriter, requestID string, status int, data any) error {
	w.Header().Set("X-Request-ID", requestID)
	return WriteJSON(w, status, Envelope{
		Data:      data,
		Error:     nil,
		RequestID: requestID,
	})
}

// WriteHandlerError writes a handler-layer error envelope.
func WriteHandlerError(w http.ResponseWriter, requestID string, status int, code, message string, details any) error {
	w.Header().Set("X-Request-ID", requestID)
	return WriteJSON(w, status, Envelope{
		Data: nil,
		Error: &APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: requestID,
	})
}

// WriteDomainError maps a domain error to HTTP and writes the envelope.
//
// Served through RequestPipeline, it first tells the request log about the
// error (statusRecorder.noteError): a mapped error's code, whether it declares
// a Retry-After, an unmapped error's ErrorClass. The one error it writes
// nothing for is the client's own cancellation, once the client has gone and
// while no status has been decided yet, which the request log records as a
// 499; everything else is written as usual.
func WriteDomainError(w http.ResponseWriter, requestID string, err error) error {
	if !requestRecorder(w).noteError(err) {
		return nil
	}
	var domainErr apperror.DomainError
	if errors.As(err, &domainErr) {
		status := domainHTTPStatus(domainErr.Code())
		if after := domainRetryAfter(domainErr.Code()); after != "" {
			w.Header().Set("Retry-After", after)
		}
		return WriteHandlerError(w, requestID, status, domainErr.Code(), domainErr.Message(), domainErr.Details())
	}
	return WriteHandlerError(w, requestID, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
}

// domainRetryAfter is the Retry-After, in seconds, a domain refusal carries, or
// "" for one that carries none.
//
// ONLY A REFUSAL ABOUT CAPACITY GETS ONE, where the same request a moment later
// is expected to succeed and saying when is useful to the caller. It lives
// beside the status mapping so a handler never special-cases a code to add it.
//
// It also decides how the request is logged: a 5xx whose domain error declares
// a Retry-After here is an expected refusal and logs at WARN, every other 5xx
// at ERROR, whatever headers a handler set by hand (loggingMiddleware).
func domainRetryAfter(code string) string {
	switch code {
	// Every Holder Export slot on the instance is streaming (ADR 0075). Most
	// exports finish in seconds, so a few seconds is when a retry is worth it.
	case "HOLDER_EXPORT_BUSY":
		return "5"
	}
	return ""
}

func domainHTTPStatus(code string) int {
	switch code {
	// OTP_GLOBAL_CEILING_REACHED shares the status with the per-key limits but
	// never the code: same "come back later" for the caller, a different signal
	// entirely for whoever is reading the logs.
	case "OTP_RATE_LIMITED", "OTP_ATTEMPTS_EXCEEDED", "OTP_GLOBAL_CEILING_REACHED":
		return http.StatusTooManyRequests
	// A buyer sending Assignment mails faster than the per-buyer window allows
	// (#332, parent #322). It shares the status with the passcode limits and
	// never the code, on the same rule the ceiling above is held to: the caller
	// hears "come back later" either way, and whoever reads the logs can still
	// tell which control fired. It is temporary by construction — the window
	// rolls — which is exactly what distinguishes it from the per-Ticket cap,
	// a 409 further down.
	case "ASSIGNMENT_RATE_LIMITED":
		return http.StatusTooManyRequests
	case "OTP_INVALID", "OTP_EXPIRED":
		return http.StatusUnauthorized
	case "SESSION_NOT_FOUND", "SESSION_EXPIRED":
		return http.StatusUnauthorized
	// A failed Google Sign-In is 401 for the same reason a bad passcode is: the
	// caller proved nothing. It is one code covering every cause on purpose —
	// see googleauth.ErrSignInFailed.
	case "GOOGLE_SIGN_IN_FAILED":
		return http.StatusUnauthorized
	// A Customer Session failing is reported with its own codes: it is an
	// unrelated record on an unrelated surface to a Staff Session (ADR 0010).
	case "CUSTOMER_SESSION_NOT_FOUND", "CUSTOMER_SESSION_EXPIRED":
		return http.StatusUnauthorized
	// A genuine Customer Session that is simply too narrow for what it was
	// presented for — a Confirmation Link session asking to edit the profile. It
	// is 403 and not 401 because re-presenting the same credential can never
	// help; a wider one is needed.
	case "CUSTOMER_SESSION_SCOPE_INSUFFICIENT":
		return http.StatusForbidden
	// A Confirmation Link is a credential, so a bad or spent one is 401 for the
	// same reason a bad passcode is: the caller failed to prove anything.
	case "CONFIRMATION_LINK_INVALID", "CONFIRMATION_LINK_EXPIRED":
		return http.StatusUnauthorized
	// A pending-consent token that is unknown, spent or expired (#251). 401 with
	// its Confirmation Link neighbour and for the same reason: it is a credential
	// standing between proof of email ownership and a Customer Session, and the
	// recovery is to sign in again.
	case "PENDING_CONSENT_INVALID":
		return http.StatusUnauthorized
	// The staff door's pending-terms token, same nature and same status as its
	// customer neighbour above, on its own code so whoever reads the logs can
	// tell the doors apart (#538).
	case "PENDING_TERMS_INVALID":
		return http.StatusUnauthorized
	// A consent submission with the required box unticked (#251, parent #249).
	// 400: nothing about the caller is unauthorized and re-sending the request
	// with the box ticked is exactly what fixes it. The refusal lives in the API
	// and not only in the form — see consent.ErrPolicyAcceptanceRequired.
	case "POLICY_ACCEPTANCE_REQUIRED":
		return http.StatusBadRequest
	// A terms submission with the required box unticked (#538): 400 beside its
	// policy neighbour and for its reason — nothing about the caller is
	// unauthorized, and re-sending with the box ticked is exactly what fixes it.
	case "TERMS_ACCEPTANCE_REQUIRED":
		return http.StatusBadRequest
	// The Adulthood Declaration box unticked (#586, ADR 0069). 400 beside the
	// two acceptance refusals above and for their reason — nothing about the
	// caller is unauthorized, and restating the request with the box ticked is
	// exactly what fixes it. Emphatically NOT 403: this platform has no idea how
	// old anybody is and a status meaning "you may not" would claim otherwise.
	// See consent.ErrAdulthoodDeclarationRequired.
	case "ADULTHOOD_DECLARATION_REQUIRED":
		return http.StatusBadRequest
	// A capture on a withdraw-only channel that tried to grant something (#271).
	// 400 beside its neighbour above and for the mirror reason: nothing about the
	// caller is unauthorized — a Platform Operator is entitled to be here and
	// entitled to withdraw — the request simply asks for the one thing this
	// channel may never do. See consent.ErrConsentGrantNotPermitted.
	case "CONSENT_GRANT_NOT_PERMITTED":
		return http.StatusBadRequest
	// An email address that names no Customer, answered only to a Platform
	// Operator (#271). 404 because the address named a person and there is no
	// such person; see customers.ErrCustomerNotFound for why this one surface is
	// allowed to say so when nothing else on the platform is.
	case "CUSTOMER_NOT_FOUND":
		return http.StatusNotFound
	// An unsubscribe token that does not verify is 400 and pointedly not the 401
	// its Confirmation Link neighbour gets (#224, ADR 0030). A Confirmation Link
	// mints a session, so a bad one is a failure to authenticate; unsubscribing
	// authenticates nobody by design — the link must work in a mail client months
	// after anybody last signed in — so a bad token is a malformed argument and
	// there is no credential to re-present.
	case "UNSUBSCRIBE_LINK_INVALID":
		return http.StatusBadRequest
	// No signing key configured is a deployment fault, not the caller's — as is
	// object storage missing when an Avatar upload is asked for.
	case "CONFIRMATION_LINK_UNAVAILABLE", "UNSUBSCRIBE_LINK_UNAVAILABLE", "AVATAR_UPLOAD_UNAVAILABLE", "RE_ADDRESSING_LINK_UNAVAILABLE":
		return http.StatusInternalServerError
	case "FORBIDDEN":
		return http.StatusForbidden
	// The Issuer's signing certificate (#453, ADR 0059). No key on the server
	// is 503: the deployment has not provisioned the secret, and nothing the
	// caller sends will change that. A stored certificate that will not open
	// is a 500 for the same reason — the row or the key, never the request.
	// The three ways an upload is bad are the default 400 below; a missing
	// Issuer is the ordinary 404; a missing certificate is a 409 because the
	// Issuer exists and is simply not ready to sign.
	case "CERTIFICATE_KEY_NOT_CONFIGURED":
		return http.StatusServiceUnavailable
	case "CERTIFICATE_UNREADABLE":
		return http.StatusInternalServerError
	case "ISSUER_NOT_FOUND":
		return http.StatusNotFound
	case "CERTIFICATE_NOT_UPLOADED":
		return http.StatusConflict
	// A Tax Invoice refused before a number is consumed (#454): the Issuer is
	// there but not fit for a factura (409), or the invoice as entered will
	// not build (400).
	case "ISSUER_INCOMPLETE":
		return http.StatusConflict
	case "INVOICE_INVALID":
		return http.StatusBadRequest
	case "INVOICE_NOT_FOUND", "AUTHORIZATION_XML_NOT_FOUND", "SIGNED_XML_NOT_FOUND", "RIDE_NOT_FOUND":
		return http.StatusNotFound
	// Check status / Resend on an authorized invoice, and a save that would
	// change a frozen Issuer detail (#455): the resource exists and its state
	// forbids the request. The same for either action on a document still
	// owed and unsigned (#473), and for Mark annulled on a document in any
	// state but pending or needs_attention, or either action on one already
	// annulled (#477) or withdrawn (#476).
	case "INVOICE_ALREADY_AUTHORIZED", "INVOICE_NOT_ISSUED", "ISSUER_FIELD_FROZEN", "INVOICE_NOT_ANNULLABLE", "INVOICE_ANNULLED", "INVOICE_WITHDRAWN":
		return http.StatusConflict
	// Resend on a document the Tax Authority refuses by number (#577, ADR
	// 0068): the resource exists and the request was well formed, and what
	// stands in the way is the authority's standing objection to the very
	// secuencial a resend would carry. No retry with the same body changes
	// it — that is the whole finding.
	case "INVOICE_REFUSED_BY_NUMBER":
		return http.StatusConflict
	// The Abandon's refusals (#578, ADR 0068), and Mark annulled's new one.
	// Each is a fact about the document as it stands: it is already
	// abandoned, it is in a state no refusal put it in, the authority did
	// not refuse its number, or the ledger carries no fresh Check for the
	// act to rest on. INVOICE_CHECK_NOT_FRESH is the one with a next step —
	// press Check status, then Abandon — and it is still a conflict, not a
	// 400: the request was well formed and the document's ledger is what
	// forbids it.
	case "INVOICE_ABANDONED", "INVOICE_NOT_ABANDONABLE", "INVOICE_NOT_REFUSED_BY_NUMBER",
		"INVOICE_CHECK_NOT_FRESH", "INVOICE_ABANDON_INSTEAD":
		return http.StatusConflict
	// The Sale Invoice Reissue's refusals (#483, ADR 0061): the document
	// exists and the request was well formed, and what stands in the way is
	// a fact about the document or its Sale — its kind, its state, a reversed
	// Sale, a reissue still in flight, a successor already standing, a
	// Credit Note already authorized against it. None becomes the answer by
	// being retried with the same body.
	case "INVOICE_MANUAL_NOT_REISSUABLE", "CREDIT_NOTE_NOT_REISSUABLE", "INVOICE_NOT_AUTHORIZED",
		"INVOICE_SALE_REVERSED", "REISSUE_IN_FLIGHT", "INVOICE_SUPERSEDED", "INVOICE_ALREADY_CREDITED":
		return http.StatusConflict
	// Issue again's refusals (#580, ADR 0068), on the same terms: the kind
	// of document (a manual one is typed again by hand, a Credit Note is
	// never re-owed), a state that is not one of the two terminal deaths
	// this act reaches, or a Sale that already has a live replacement. A
	// reversed Sale is INVOICE_SALE_REVERSED above, shared with the reissue
	// because it is the same fact about the same Sale.
	case "INVOICE_MANUAL_NOT_ISSUABLE_AGAIN", "CREDIT_NOTE_NOT_ISSUABLE_AGAIN",
		"INVOICE_NOT_TERMINALLY_DEAD", "INVOICE_ALREADY_REPLACED":
		return http.StatusConflict
	case "NOT_FOUND", "ORGANIZATION_NOT_FOUND", "MEMBER_NOT_FOUND", "EVENT_NOT_FOUND":
		return http.StatusNotFound
	// A House Organization designation refused for the Organization's
	// currency (#472, ADR 0060): the Organization exists and the request was
	// well formed, and what stands in the way is a fact about it that no
	// retry with the same body changes.
	case "HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED":
		return http.StatusConflict
	// TICKET_TYPE_CLOSED sits with CAPACITY_EXCEEDED and PURCHASE_LIMIT_EXCEEDED
	// and shares their 409 (ADR 0070): the request was well formed and the buyer
	// was entitled to make it, and what stands in the way is a fact about the
	// Ticket Type that no retry with the same body changes. A separate code and
	// not a third label on the sold-out one, because the Storefront words a shut
	// window differently from an exhausted one (ADR 0023).
	case "ORGANIZATION_SLUG_TAKEN", "EVENT_SLUG_TAKEN", "MEMBER_ALREADY_EXISTS", "LAST_ORG_ADMIN", "CANNOT_REMOVE_SELF", "CAPACITY_EXCEEDED", "PURCHASE_LIMIT_EXCEEDED", "TICKET_TYPE_CLOSED", "IMPORT_BATCH_FAILED", "IMPORT_NOT_LATEST_BATCH", "IMPORT_ALREADY_REVERSED", "EVENT_NOT_DRAFT", "EVENT_DELETE_FORBIDDEN", "EVENT_PUBLISH_REQUIREMENTS_NOT_MET", "EVENT_ALREADY_PUBLISHED", "EVENT_ALREADY_CANCELLED", "EVENT_NOT_PUBLISHED", "TICKET_TYPE_DELETE_FORBIDDEN", "CURRENCY_LOCKED":
		return http.StatusConflict
	case "ASSIGNMENT_NOT_FOUND", "TICKET_TYPE_NOT_FOUND", "IMPORT_BATCH_NOT_FOUND", "PAYMENT_NOT_FOUND", "TICKET_SALE_NOT_FOUND", "PROMOTION_NOT_FOUND", "AFFILIATE_LINK_NOT_FOUND", "PAYOUT_REQUEST_NOT_FOUND", "TAG_NOT_FOUND", "TICKET_QUESTION_NOT_FOUND", "TICKET_QUESTION_OPTION_NOT_FOUND", "QUESTION_REVIEW_NOT_FOUND":
		return http.StatusNotFound
	// Ticket Question authoring asked for while the feature flag is off (#309,
	// ADR 0045). 404 and pointedly not 403: while the flag is off there is
	// nothing here to be forbidden from, and the staff API must answer exactly as
	// a build without the feature would. See catalog.ErrTicketQuestionsUnavailable.
	case "TICKET_QUESTIONS_UNAVAILABLE":
		return http.StatusNotFound
	// Ticket Assignment asked for while ITS OWN flag is off (#324, parent #322).
	// A separate case from TICKET_QUESTIONS_UNAVAILABLE above and not a second
	// label on it, because TICKET_ASSIGNMENT_ENABLED is a separate flag: killing
	// assignment must not take Ticket Questions dark, and one shared code here
	// would be the first place that separation quietly stopped being true. Same
	// 404 and for the same reason — while the flag is off there is nothing here.
	case "TICKET_ASSIGNMENT_UNAVAILABLE":
		return http.StatusNotFound
	// Sale Invoicing asked for while SALE_INVOICING_ENABLED is off (#471, ADR
	// 0060): the House designation and the Drainer's endpoint. 404 on the two
	// flags' terms above — while it is closed there is nothing here — so a
	// build with the flag closed answers as one without the feature.
	case "SALE_INVOICING_UNAVAILABLE":
		return http.StatusNotFound
	// The Ticket Assignment window refusals (#324). 409 beside the Answer
	// window's two: the request was well formed and the buyer was entitled to
	// make it, and what stands in the way is a fact about the sale — it was
	// recorded at the door and has no buyer surface, it was reversed, or the
	// doors have opened. None becomes the answer by being retried with the same
	// body.
	case "ASSIGNMENT_CHANNEL_UNSUPPORTED", "ASSIGNMENT_SALE_REVERSED", "ASSIGNMENT_EVENT_STARTED":
		return http.StatusConflict
	// One Ticket's lifetime allowance of Assignment mails is spent (#332). 409
	// beside the window refusals above and pointedly NOT 429: this never becomes
	// the answer by waiting, and a "too many requests" would send the buyer back
	// in an hour to hear the same thing forever. What stands in the way is a
	// permanent fact about the Ticket, which is what 409 says here.
	case "ASSIGNMENT_MAIL_CAP_REACHED":
		return http.StatusConflict
	// A Holder address that is not an address (#324). 400 and not 409, on the
	// same line INVALID_ANSWER sits on: the body itself is wrong and restating
	// it correctly is exactly what fixes it.
	case "INVALID_HOLDER_EMAIL":
		return http.StatusBadRequest
	// The Ticket Question authoring refusals (#309). All 409 for the reason their
	// Promotion neighbours are: the request was well formed and the Org Admin was
	// entitled to make it, and what stands in the way is a fact about the question
	// — it has been answered, it has been retired, its kind does not take Options,
	// it would be left with none, or it already has twenty. None becomes the
	// answer by being retried with the same body.
	// TICKET_QUESTION_APPROVED_IMMUTABLE, its Option twin and
	// TICKET_QUESTION_UNDER_REVIEW (#408, ADR 0056) are the same shape: a fact
	// about where the question stands with the Operator.
	case "TICKET_QUESTION_KIND_FROZEN",
		"TICKET_QUESTION_RETIRED",
		"TICKET_QUESTION_APPROVED_IMMUTABLE",
		"TICKET_QUESTION_OPTION_APPROVED_IMMUTABLE",
		"TICKET_QUESTION_UNDER_REVIEW",
		"TICKET_QUESTION_NOT_APPROVED",
		"TICKET_QUESTION_KIND_TAKES_NO_OPTIONS",
		"TICKET_QUESTION_OPTIONS_REQUIRED",
		"TOO_MANY_TICKET_QUESTION_OPTIONS":
		return http.StatusConflict
	// A Ticket, or the Answer on one, that this Event does not have (#310). 404
	// beside TICKET_QUESTION_NOT_FOUND above and for the same reason: the
	// address named a thing and there is no such thing here, whether because it
	// never existed or because it belongs to another Organization — which are
	// deliberately the same answer.
	case "TICKET_NOT_FOUND", "ANSWER_NOT_FOUND":
		return http.StatusNotFound
	// The two edit-window refusals (#310). 409: the request was well formed and
	// the caller was entitled to make it, and what stands in the way is a fact
	// about the Ticket — its Sale was reversed, or the doors have opened.
	// Neither becomes the answer by being retried with the same body, and both
	// leave everything already answered readable.
	// The Question Review's refusals (#406, ADR 0056). The acknowledgement is a
	// missing part of the body, so 400 beside INVALID_HOLDER_EMAIL; the other
	// three are facts about the Event or the Review that no retry with the same
	// body changes, so 409 beside their Ticket Question neighbours.
	case "QUESTION_REVIEW_ACKNOWLEDGEMENT_REQUIRED":
		return http.StatusBadRequest
	// The Operator's answer with a hole in it (#407): an item without a
	// verdict, a refusal without a reason, an item the Review does not carry.
	// The body is wrong, so 400 beside the acknowledgement.
	case "QUESTION_REVIEW_VERDICT_REQUIRED", "QUESTION_REVIEW_REASON_REQUIRED", "QUESTION_REVIEW_UNKNOWN_ITEM":
		return http.StatusBadRequest
	case "QUESTION_REVIEW_OUTSTANDING", "QUESTION_REVIEW_EVENT_STARTED",
		"QUESTION_REVIEW_NOTHING_TO_REVIEW", "QUESTION_REVIEW_NOT_OUTSTANDING":
		return http.StatusConflict
	case "TICKET_SALE_REVERSED", "EVENT_STARTED_ANSWERS_CLOSED":
		return http.StatusConflict
	// The Sale Re-addressing's refusals (#420, ADR 0058). All 409 beside the
	// Operator Reversal's SALE_NOT_REVERSIBLE and for the same reason: the
	// request was well formed and the Operator was entitled to make it, and
	// what stands in the way is a fact about the Sale — its channel, its
	// status, its Event's start, or that the "correction" is the address it
	// already carries — or, on a withdrawal, that nothing is pending to be
	// withdrawn (#423). None becomes the answer by being retried with the same
	// body.
	case "SALE_NOT_RE_ADDRESSABLE", "RE_ADDRESSING_EVENT_STARTED", "RE_ADDRESSING_SAME_ADDRESS", "RE_ADDRESSING_NOTHING_PENDING":
		return http.StatusConflict
	// The Re-addressing Link's own refusals (#421, ADR 0058), 401 as the
	// Assignment Link's are: the token IS the credential, and a token that does
	// not open — forged, or genuine but for a record that has since ended — is
	// a credential that does not authenticate. The two are told apart by code
	// because the reader of the second is the buyer, who is entitled to know
	// that the purchase can no longer be accepted.
	case "RE_ADDRESSING_LINK_INVALID", "RE_ADDRESSING_LINK_NO_LONGER_VALID":
		return http.StatusUnauthorized
	// An Answer that does not fit its Ticket Question (#310). 400 and not 409,
	// which is the line between these and the two above: the body itself is
	// wrong — text sent to a number question, an Option the question does not
	// offer — and restating it correctly is exactly what fixes it. The details
	// carry the kind and the problem token so the form can point at the field.
	case "INVALID_ANSWER", "ANSWER_OPTION_NOT_OFFERED":
		return http.StatusBadRequest
	// An Assignment Link that does not open (#325, ADR 0046). 401 beside its
	// Confirmation Link neighbour and for the same reason: the token IS the
	// credential, and one that was tampered with, truncated by a mail client,
	// or signed by another deployment is a caller who proved nothing.
	//
	// ASSIGNMENT_LINK_INVALID also answers a Ticket whose Sale was reversed or
	// that has been REASSIGNED away from this address, and that breadth
	// is the disclosure rule holding in the error state. "Your friend gave your
	// ticket to somebody else" is a fact about the buyer's decisions, and this
	// page never names the buyer or describes what they did.
	case "ASSIGNMENT_LINK_INVALID", "ASSIGNMENT_LINK_EXPIRED":
		return http.StatusUnauthorized
	// No link secret configured, as above: the deployment's fault, not the
	// Holder's — and this reader has no buyer to ask for a new link, because they
	// are not told who the buyer is.
	case "ASSIGNMENT_LINK_UNAVAILABLE":
		return http.StatusInternalServerError
	// The Privacy Policy asked for in a language it is not published in (#250).
	// 404 rather than a 400 about a bad parameter: the address named a document,
	// and that document does not exist. Never a fallback to English — see
	// consent.ErrPolicyLocaleNotPublished.
	case "POLICY_LOCALE_NOT_PUBLISHED":
		return http.StatusNotFound
	// The Terms asked for in a language they are not published in (ADR 0066).
	// 404 beside its policy neighbour and for its reason: the address named a
	// document, and that document does not exist in that language. Never a
	// fallback — an English reader must not be handed the Spanish contract under
	// their own language's address.
	case "TERMS_LOCALE_NOT_PUBLISHED":
		return http.StatusNotFound
	// The Legal Center asked to draft a document that is not one of the two
	// (#561). 404 beside the two above and for the same reason: the address
	// named a document and there is no such document. The Legal Center's other
	// refusals — an unsupported language, an empty language set, a missing or
	// duplicated slug — are all "the body is wrong", so they take the default
	// 400 and are not listed here.
	case "LEGAL_DOCUMENT_NOT_FOUND":
		return http.StatusNotFound
	// The per-subject consent record addressed to somebody nobody is (#566):
	// a customer id that names no Customer, or a Staff Digest that matches
	// nobody on the Staff platform. 404 beside LEGAL_DOCUMENT_NOT_FOUND and for
	// its reason — the address named a person and there is no such person — and
	// 404 rather than an empty record, because an operator following a stale
	// link must be told the person is not there instead of being shown a blank
	// record they might then act on.
	case "LEGAL_SUBJECT_NOT_FOUND", "STAFF_SUBJECT_NOT_FOUND":
		return http.StatusNotFound
	// A preview or a diff recorded against a document with no saved draft
	// (#562). 409: the request was well formed and the operator was entitled to
	// make it, and what stands in the way is a fact about the draft that no
	// restatement of the body can fix. Its sibling LEGAL_DRAFT_CELL_NOT_FOUND —
	// a cell the draft has no text in — is "the body names something that is not
	// there", so it takes the default 400 and is not listed here.
	case "LEGAL_DRAFT_NOT_STORED":
		return http.StatusConflict
	// The two review gates a publication must clear (#563): every artifact seen
	// rendered, and the diff against the current edition seen. 409 for
	// LEGAL_DRAFT_NOT_STORED's reason — the request was well formed and the
	// operator was entitled to make it, and what stands in the way is a fact
	// about the draft rather than about the body. The rest of the publish
	// refusals (an incomplete draft, a structural or locale-set change offered as
	// a correction, an empty diff offered as a correction, a missing reason, an
	// effective date that is not one or is too soon, a dropped protected
	// language) are all "the request asserts something the draft does not
	// support", so they take the default 400 and are not listed.
	case "LEGAL_PUBLISH_NOT_PREVIEWED", "LEGAL_PUBLISH_DIFF_NOT_SEEN":
		return http.StatusConflict
	// A cancellation addressed to an edition that is not there (#564). 404 for
	// LEGAL_DOCUMENT_NOT_FOUND's reason — the address named an edition and there
	// is no such edition of this document — and the two documents are separate
	// tables, so one document's path never confirms the other's rows.
	case "LEGAL_EDITION_NOT_FOUND":
		return http.StatusNotFound
	// A cancellation arriving after the edition's day (#564). 409 and not 400:
	// the request was well formed and the operator was entitled to make it when
	// the screen offered it, and what stands in the way is a fact about the
	// CALENDAR that no restatement of the body can fix. The control is gone by
	// then, so this is what a page left open overnight meets.
	case "LEGAL_EDITION_ALREADY_EFFECTIVE":
		return http.StatusConflict
	// The staff acceptance browser on a deployment with no CONFIRMATION_LINK_
	// SECRET (#565). 503 and not 500: nothing is broken and no request was
	// malformed — the deployment is missing a value, and the screen REFUSES TO
	// SERVE rather than name people under a zero key anybody could reproduce.
	// Unreachable in production, where server.NewApp will not start without it.
	// Its two neighbours LEGAL_STANDING_UNKNOWN and LEGAL_STANDING_NOT_AVAILABLE
	// are "the body asked for a state that is not one of the four" and "not one
	// this population has", so both take the default 400 and are not listed.
	case "STAFF_DIGEST_UNAVAILABLE":
		return http.StatusServiceUnavailable
	// No Policy Version in effect. A deployment fault — migration 060 seeds one
	// — so it is the platform's 500 and not the caller's 404.
	case "NO_CURRENT_POLICY_VERSION":
		return http.StatusInternalServerError
	// The two Payout Request refusals (ADR 0026). Both 409: the request was well
	// formed and the Org Admin was entitled to make it, and what stands in the way
	// is a fact about the money or about the request's own state. Asking for more
	// than has cleared is not a typo the caller can fix by restating the body —
	// they must ask for less, or wait for the balance to clear — and a request
	// that has already been paid, declined or cancelled will never be pending
	// again, so neither is retryable into success.
	case "PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE", "PAYOUT_REQUEST_NOT_PENDING":
		return http.StatusConflict
	// The operator's half of the same fact (#177): a fulfilment or a decline that
	// reached a request somebody else had already ended. 409 for the same reason
	// the two above are — the request will never be pending again, so retrying is
	// not a path to success — and a separate code from PAYOUT_REQUEST_NOT_PENDING
	// because the sentence it carries is a different instruction to a different
	// person: the compare-and-swap stopped a second RECORD, not a second
	// TRANSFER, and an operator who also wired the money must record the Payout
	// directly (ADR 0026).
	case "PAYOUT_REQUEST_ALREADY_RESOLVED":
		return http.StatusConflict
	// A decline — or a second submitted transfer — against a request whose
	// transfer has already been submitted (#184, ADR 0026 amendment). 409 like its two
	// neighbours, and its own code because it is the one refusal here that is
	// NOT about a request that ended: nothing has been resolved, the money is on
	// its way, and the answer is to wait for the bank rather than to record a
	// Payout for money that might come back.
	case "PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED":
		return http.StatusConflict
	// Its mirror (#185): marking `failed` a request no transfer was submitted
	// for. 409 again — the request was well formed and the operator entitled to
	// make it, and what stands in the way is the request's own state. It is its
	// own code rather than PAYOUT_REQUEST_ALREADY_RESOLVED because the request
	// most often has NOT ended: it is `pending`, nobody has touched it, and the
	// answer is to decline it or to submit a transfer first.
	case "PAYOUT_REQUEST_TRANSFER_NOT_SUBMITTED":
		return http.StatusConflict
	// The two sides of the External Registration exclusivity invariant (ADR 0028),
	// which spans events and ticket_types and so cannot be a CHECK constraint.
	// Both 409: the request was well formed and the caller entitled to make it,
	// and what stands in the way is the Event's own mode, or the Ticket Types
	// already on it. Neither becomes the answer by being retried — one needs the
	// mode switched back, the other needs the Ticket Types deleted. Two codes
	// rather than one because they are two different instructions to the reader.
	case "EVENT_IS_EXTERNAL_REGISTRATION", "EVENT_HAS_TICKET_TYPES":
		return http.StatusConflict
	// The two published-state freezes on the same pair (#208). Both 409 for the
	// same reason as their neighbours above: the request was well formed and the
	// caller entitled to make it, and what stands in the way is the Event's own
	// status. Neither is a body the caller can fix into success — one needs a new
	// Event, the other needs a replacement link rather than a removal — which is
	// why they are 409 and not 400.
	case "EVENT_REGISTRATION_MODE_LOCKED", "EVENT_REGISTRATION_URL_REQUIRED":
		return http.StatusConflict
	// The Promotion refusals (ADR 0021). All 409: the request was well formed and
	// the caller was entitled to make it, but the catalog is not in a state that
	// admits it — the one slot is taken, or the price would break the invariant
	// that a Promotional Price sits strictly below its List Price. Each needs a
	// different edit elsewhere before it can succeed, which is why they are three
	// codes and not one.
	case "PROMOTION_ALREADY_EXISTS", "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE", "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE":
		return http.StatusConflict
	// An Affiliate Link that has been clicked or has attributed a sale cannot be
	// deleted: 409, because the request was well formed and permitted, and the
	// link's history is what stands in the way. Deactivation is the way out, and
	// retrying the delete never becomes the answer.
	case "AFFILIATE_LINK_HAS_HISTORY":
		return http.StatusConflict
	// The Customer-initiated Sale Reversal refusals (ADR 0018). All three are
	// 409: the request was well formed and the caller was entitled to make it,
	// but the Ticket Sale is not in a state that admits an undo — already
	// reversed, never reversible, or past its Reversal Window. Retrying changes
	// nothing, which is what separates them from a 400 the caller could fix.
	case "SALE_ALREADY_REVERSED", "SALE_NOT_REVERSIBLE", "SALE_NOT_IMPORTED", "REVERSAL_WINDOW_CLOSED":
		return http.StatusConflict
	// A Customer pressing Undo on a sale whose reversal became an Unresolved
	// Reversal (ADR 0024). A fourth 409 for the same reason as the three above —
	// the request was fine and the sale is not in a state that admits an undo —
	// and pointedly not the 503-with-a-retry the operator's contention gets:
	// retrying is the one thing that must not happen here, because nobody knows
	// whether the money already went back.
	case "REVERSAL_UNRESOLVED":
		return http.StatusConflict
	// An Operator Reversal that contended with a reversal already in flight on the
	// same Ticket Sale (ADR 0024). 503 with a retry rather than 409: unlike the
	// three above, this says nothing about the sale's state — it is the platform
	// asking for a moment while it finds out what the Payment Provider did, and
	// retrying is exactly the right response.
	case "SALE_REVERSAL_IN_PROGRESS":
		return http.StatusServiceUnavailable
	// Every Holder Export slot on this API instance is streaming (ADR 0075). A
	// 503 with a retry for SALE_REVERSAL_IN_PROGRESS's reason: it says nothing
	// about the request, and trying again in a moment is exactly right.
	case "HOLDER_EXPORT_BUSY":
		return http.StatusServiceUnavailable
	// The three refusals about an Operator Reversal's money memo: a refund larger
	// than the Ticket Sale ever collected (#125), money stated about a sale that
	// collected none, and money left out of a sale that collected some (#126).
	// All are 400 and not 409 because the operator can fix every one of them by
	// restating the memo — the sale is perfectly reversible, the assertion about
	// it is not. Stated rather than left to the default so the choice is visible
	// beside the reversal refusals above.
	case "REFUNDED_AMOUNT_EXCEEDS_COLLECTED", "NOTHING_TO_REFUND", "REFUNDED_AMOUNT_REQUIRED":
		return http.StatusBadRequest
	// The provider was asked and refused. Nothing was changed, and the cause is
	// on the far side of a boundary the buyer cannot act on — a 502 rather than a
	// 409, because this is not a fact about their purchase.
	case "SALE_REVERSAL_FAILED":
		return http.StatusBadGateway
	// The provider took the money but the Ticket Sale could not be recorded: a
	// platform-side failure the caller cannot fix, logged loudly server-side for
	// the operator to resolve by hand.
	case "PAYMENT_SALE_COMMIT_FAILED":
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

// WriteInvalidJSON writes a malformed JSON error response.
func WriteInvalidJSON(w http.ResponseWriter, requestID string) error {
	return WriteHandlerError(w, requestID, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON", nil)
}

// WriteValidationError writes a validation error response.
func WriteValidationError(w http.ResponseWriter, requestID string, fields []FieldError) error {
	return WriteHandlerError(w, requestID, http.StatusBadRequest, "VALIDATION_FAILED", "Request validation failed", ValidationErrorDetails{
		Fields: fields,
	})
}

// WriteUnauthorized writes an unauthorized error response.
func WriteUnauthorized(w http.ResponseWriter, requestID string, message string) error {
	if message == "" {
		message = "Authentication required"
	}
	return WriteHandlerError(w, requestID, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

// RequestID returns the request ID stored on the context.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithRequestID stores a request ID on the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}
