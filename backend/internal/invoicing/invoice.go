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
	// InvoiceStatusAnnulled: an authorized document the operator annulled by
	// hand at the authority's portal.
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
	// ReversalReason the reversal route that made it owed.
	CreditsInvoiceID string
	ReversalReason   string
	// IVARate is the one rate a platform-priced document was priced under;
	// "" on a manual document, whose lines each carry their own.
	IVARate IVARate
	// DeliveredAt is when the authorized document was mailed to the buyer;
	// NextAttemptAt when the Drainer should next work it. Nil when not.
	DeliveredAt   *time.Time
	NextAttemptAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

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
