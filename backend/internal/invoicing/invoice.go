package invoicing

import "time"

// The Tax Invoice (#454): what the platform issued to a Recipient through a
// country's Tax Authority, as the core keeps it. Everything here is shared by
// every country; the Ecuador detail row (clave de acceso, secuencial) lives
// beside it and is the adapter's shape.

// InvoiceStatus is where a Tax Invoice stands with its authority.
type InvoiceStatus string

const (
	// InvoiceStatusPending: the authority has not answered yet, said it was
	// still working, or could not be reached. The number is kept; the
	// operator chases it with Check status / Resend (#455).
	InvoiceStatusPending InvoiceStatus = "pending"
	// InvoiceStatusAuthorized: the legal artifact exists and is on file.
	InvoiceStatusAuthorized InvoiceStatus = "authorized"
	// InvoiceStatusNotAuthorized: examined and refused; the messages say why.
	InvoiceStatusNotAuthorized InvoiceStatus = "not_authorized"
	// InvoiceStatusRejected: not taken at all (SRI DEVUELTA); the messages
	// say why.
	InvoiceStatusRejected InvoiceStatus = "rejected"

	// The states a document the platform owes itself passes through (#473,
	// ADR 0060). A manual Tax Invoice is born pending and never sees them.

	// InvoiceStatusOwed: recorded in the transaction that recorded its
	// Ticket Sale, not yet worked. No number, no signature, no Issuer even.
	InvoiceStatusOwed InvoiceStatus = "owed"
	// InvoiceStatusNeedsAttention: parked for a Platform Operator — unsignable
	// from owed (no Issuer, no certificate, expired), or definitely refused
	// or unanswered too long from pending.
	InvoiceStatusNeedsAttention InvoiceStatus = "needs_attention"
	// InvoiceStatusWithdrawn: never sent and never will be — its Sale was
	// reversed first, or the factura it would have credited died.
	InvoiceStatusWithdrawn InvoiceStatus = "withdrawn"
	// InvoiceStatusAnnulled: a signed document — pending or needs_attention,
	// never authorized, which is credited instead — the operator annulled by
	// hand at the authority's portal and then recorded as such (#477).
	InvoiceStatusAnnulled InvoiceStatus = "annulled"
)

// DocumentKind says why a Tax Invoice exists: an operator typed it, a paid
// House checkout owed it, or such a Sale's reversal owed it (#473).
type DocumentKind string

const (
	DocumentKindManual     DocumentKind = "manual"
	DocumentKindSale       DocumentKind = "sale"
	DocumentKindCreditNote DocumentKind = "credit_note"
)

// SaleInvoiceIVARate is the rate every Sale Invoice is priced under: the
// platform sells tickets at the general rate, with the IVA inside the price
// the buyer paid (ADR 0060). The 0% RUAC rate for cultural shows is out of
// scope, visibly.
const SaleInvoiceIVARate = IVARate15

// IVARate is the platform's word for a line's IVA rate. The adapter
// translates to the authority's code (in Ecuador 4 / 0 / 7 / 6).
type IVARate string

const (
	IVARate15       IVARate = "15"
	IVARateZero     IVARate = "0"
	IVARateExento   IVARate = "exento"
	IVARateNoObjeto IVARate = "no_objeto"
)

// IVARates lists the accepted rates, for messages and forms.
var IVARates = []IVARate{IVARate15, IVARateZero, IVARateExento, IVARateNoObjeto}

// Valid reports whether r is one of the four rates.
func (r IVARate) Valid() bool {
	for _, v := range IVARates {
		if r == v {
			return true
		}
	}
	return false
}

// MaxOperatorAdditionalFields is how many name/value pairs an operator may
// add. The SRI's schema allows fifteen campoAdicional and the Recipient's
// email takes one of them automatically, so fourteen are the operator's.
const MaxOperatorAdditionalFields = 14

// Recipient is who a Tax Invoice is issued to, exactly as entered.
type Recipient struct {
	TaxIDType string
	TaxID     string
	LegalName string
	Address   string
	Email     string
}

// InvoiceLine is one line as stored: what was entered and the arithmetic the
// document carries for it.
type InvoiceLine struct {
	Position    int
	Description string
	// QuantityMillionths is the quantity × 1,000,000: six decimals.
	QuantityMillionths int64
	UnitPriceCents     int64
	DiscountCents      int64
	IVARate            IVARate
	// BaseCents is quantity × unit price − discount; IVACents the tax on it.
	BaseCents int64
	IVACents  int64
}

// AdditionalField is one operator-entered name/value pair.
type AdditionalField struct {
	Position int
	Name     string
	Value    string
}

// IssuerSnapshot is the Ecuador Issuer's details as they stood at issue time,
// with JSON names for the snapshot column. A resend (#455) rebuilds the
// document from this and never from the live Issuer row.
type IssuerSnapshot struct {
	RUC                      string  `json:"ruc"`
	RazonSocial              string  `json:"razon_social"`
	NombreComercial          string  `json:"nombre_comercial"`
	DireccionMatriz          string  `json:"direccion_matriz"`
	DireccionEstablecimiento string  `json:"direccion_establecimiento"`
	Establecimiento          string  `json:"establecimiento"`
	PuntoEmision             string  `json:"punto_emision"`
	ObligadoContabilidad     bool    `json:"obligado_contabilidad"`
	Regimen                  string  `json:"regimen"`
	AgenteRetencion          *string `json:"agente_retencion"`
}

// SnapshotOf freezes the Issuer's details.
func SnapshotOf(d EcuadorIssuerDetails) IssuerSnapshot {
	return IssuerSnapshot{
		RUC:                      d.RUC,
		RazonSocial:              d.RazonSocial,
		NombreComercial:          d.NombreComercial,
		DireccionMatriz:          d.DireccionMatriz,
		DireccionEstablecimiento: d.DireccionEstablecimiento,
		Establecimiento:          d.Establecimiento,
		PuntoEmision:             d.PuntoEmision,
		ObligadoContabilidad:     d.ObligadoContabilidad,
		Regimen:                  d.Regimen,
		AgenteRetencion:          d.AgenteRetencion,
	}
}

// Invoice is the core Tax Invoice row.
//
// A DOCUMENT MAY BE UNSIGNED. Before #473 every row was signed at birth;
// an owed Sale Invoice or Credit Note has no Issuer, environment, emission
// date, signer, snapshot or bytes until the Drainer signs it, and Signed
// says which. The zero values stand in for NULL on the unsigned side, and
// the views render them as null rather than as blanks.
type Invoice struct {
	ID string
	// Kind is why the document exists (manual, sale, credit_note).
	Kind DocumentKind
	// IssuerID is "" until signed.
	IssuerID string
	Country  Country
	// Environment is "" until signed: it is copied from the Issuer at signing
	// time, so an Issuer moved to production signs still-owed documents in
	// production.
	Environment Environment
	Status      InvoiceStatus
	// IssuedOn is the emission date in the Issuer's country; IssuedAt the
	// instant Issue was pressed; IssuedBy the operator's email (or the
	// Drainer's name). All zero until signed.
	IssuedOn  time.Time
	IssuedAt  time.Time
	IssuedBy  string
	Recipient Recipient
	// Issuer is the Issuer's details as they stood at signing; nil until then.
	Issuer   *IssuerSnapshot
	Currency string
	// Totals in cents, as the document carries them.
	SubtotalCents int64
	DiscountCents int64
	IVACents      int64
	TotalCents    int64
	PaymentMethod string
	// SignedXML is the document exactly as sent; AuthorizationXML the
	// authority's own document once authorized, nil before.
	SignedXML        []byte
	AuthorizationXML []byte
	// Messages are the authority's messages from its last answer, verbatim.
	Messages         []AuthorityMessage
	Lines            []InvoiceLine
	AdditionalFields []AdditionalField

	// The Sale side (#473, ADR 0060), all empty on a manual document.

	// TicketSaleID is the Ticket Sale a sale document or credit note is about.
	TicketSaleID string
	// SaleConfirmationRef is that Sale's Sale Confirmation reference, read
	// beside the row for the operator surfaces; never stored here.
	SaleConfirmationRef string
	// CreditsInvoiceID is the Sale Invoice a Credit Note credits, and
	// CreditNoteReason why (#481, ADR 0061): a Sale Reversal's route, or
	// CreditNoteReasonReissue. A reason, not a reversal — a reissue Credit
	// Note has no reversal behind it.
	CreditsInvoiceID string
	CreditNoteReason string
	// CreditedByInvoiceID is, on a Sale Invoice, the Credit Note that
	// credits it (#476) — the newest LIVE one, should there ever be more
	// than one: a withdrawn or annulled Credit Note credits nothing (#484)
	// — read beside the row so the two documents link both ways; "" when
	// none does.
	CreditedByInvoiceID string
	// IVARate is the one rate a platform-priced document was priced under;
	// "" on a manual document, whose lines each carry their own.
	IVARate IVARate
	// DeliveredAt is when the authorized document was mailed to the buyer;
	// NextAttemptAt when the Drainer should next work it. Nil when not.
	DeliveredAt   *time.Time
	NextAttemptAt *time.Time
	// AttentionSince is when the document entered needs_attention — what the
	// Operator Dashboard's queue orders by (#477); nil in every other state.
	AttentionSince *time.Time
	// AnnulledBy and AnnulledAt are the operator who recorded the manual
	// portal annulment and when (#477); "" and nil unless annulled.
	AnnulledBy string
	AnnulledAt *time.Time
	// RecipientWarning is the authority's word, on an authorized Sale
	// Invoice, that the Recipient's Tax ID does not exist or is incorrect
	// (#482, ADR 0061): set from the authorization's messages, cleared only
	// when the document is superseded — the moment the corrected factura
	// that supersedes it is authorized (#484). Never true on any other kind.
	RecipientWarning bool

	// The Sale Invoice Reissue (#483, ADR 0061). SupersedesInvoiceID is, on
	// a Sale Invoice a reissue produced, the factura it corrects; stored
	// here and nowhere else. SupersededByInvoiceID is the reverse link read
	// beside the row — the live successor (one not withdrawn) of a
	// reissued factura, "" when it is current. ReissuedBy, ReissuedAt and
	// ReissueNote are the reissue's trail: stored on the corrected factura,
	// and read beside the superseded factura and the reissue Credit Note so
	// every document concerned shows who, when and why.
	SupersedesInvoiceID   string
	SupersededByInvoiceID string
	ReissuedBy            string
	ReissuedAt            *time.Time
	ReissueNote           string

	// The Sale Invoice Backfill's trail (#508, ADR 0064): on a Sale Invoice
	// a Platform Operator owed to an Uninvoiced House Sale, who and when.
	// "" and nil on a document born at checkout, and on every other kind.
	// Only its birth differs: from the moment it is owed the document is
	// drained, delivered, credited and reissued like any Sale Invoice.
	BackfilledBy string
	BackfilledAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxReissueNoteLength bounds the operator's note on a Sale Invoice Reissue
// (#483): a sentence for a colleague, bounded as an Operator Reversal's note
// is and by the schema (migration 102), stated here so the caller is told
// which field is wrong rather than shown a constraint violation.
const MaxReissueNoteLength = 500

// Signed reports whether the document has been built and signed — the seven
// issue-time facts are present and an Ecuador detail row exists — as opposed
// to merely owed.
func (i *Invoice) Signed() bool {
	return len(i.SignedXML) > 0
}

// EcuadorInvoiceDetails is the SRI's numbering of one Tax Invoice.
type EcuadorInvoiceDetails struct {
	CodDoc        string
	Estab         string
	PtoEmi        string
	Secuencial    int64
	AccessKey     string
	Authorization *Authorization
}

// Authorization is what the SRI granted an authorized document.
type Authorization struct {
	Number string
	Date   time.Time
}

// AttemptOperation names which of the two TaxAuthority calls an attempt was.
type AttemptOperation string

const (
	AttemptSubmit AttemptOperation = "submit"
	AttemptQuery  AttemptOperation = "query"
)

// AttemptOutcomeError is the attempts-ledger outcome when no answer could be
// read from the authority: a transport failure, a fault, a timeout.
const AttemptOutcomeError = "error"

// Attempt is one request made to the authority for an invoice.
type Attempt struct {
	ID        int64
	InvoiceID string
	Operation AttemptOperation
	// Outcome is an OutcomeState, or AttemptOutcomeError.
	Outcome   string
	Messages  []AuthorityMessage
	Error     string
	StartedAt time.Time
	Duration  time.Duration
}

// AcknowledgedByAuthority reports whether the authority ever took delivery
// of the document: some Submit came back received — RECIBIDA, or 43/70,
// "the clave is already registered / in processing" on a resend.
//
// ONLY A SUBMIT ACKNOWLEDGES (#513, maintainer ruling 2026-08-29). A query
// answer never does, whatever it says: OutcomeReceived from autorización is
// the authority describing a document it is processing, and OutcomeUnknown
// is the authority saying it has no record of the clave at all. Reading
// either as acknowledgement is what left production invoice
// 001-001-000000001 polled forever after its Submit died in transport —
// the SRI had never received it.
//
// A document this reports false for is sent again, byte for byte, under the
// same clave and secuencial. That can never duplicate: if the authority did
// hold it and only its answer was lost, the resubmit is answered 43/70,
// which is itself an acknowledging Submit.
func AcknowledgedByAuthority(attempts []Attempt) bool {
	for _, a := range attempts {
		if a.Operation == AttemptSubmit && a.Outcome == string(OutcomeReceived) {
			return true
		}
	}
	return false
}

// CreditNoteReasonReissue is the one Credit Note reason that is not a Sale
// Reversal's route (#481, ADR 0061): the Sale Invoice it credits is being
// superseded by a Sale Invoice Reissue, and the Sale stands. The five
// routes are sales.SaleReversal's Route strings, stored as they come.
const CreditNoteReasonReissue = "reissue"

// CreditNoteMotivo is the reason a Credit Note states to the authority
// for the reason it was owed (#476, ADR 0060; #481, ADR 0061): a reversal
// route, or a reissue, in the document's own language, since the nota de
// crédito is read by the SRI and the buyer's accountant, never by the
// Storefront. A reissue's motivo is the fixed correction text and never a
// reversal's: the sale was not reversed. An unknown reason — one added to
// the schema's CHECK later — is named as such rather than refused: the
// document is owed by then, and a reason it cannot state must not be the
// reason it is never issued.
func CreditNoteMotivo(reason string) string {
	switch reason {
	case "customer":
		return "Anulación de la venta por el comprador"
	case "platform":
		return "Anulación de la venta por el operador de la plataforma"
	case "import_undo":
		return "Anulación de la venta al deshacer su importación"
	case "staff_reversal":
		return "Anulación de la venta por el personal de la organización"
	case "correction":
		return "Anulación de la venta por corrección"
	case CreditNoteReasonReissue:
		return "Corrección de los datos del receptor"
	}
	return "Anulación de la venta"
}
