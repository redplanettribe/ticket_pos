// Package invoicing owns Tax invoicing: the platform's Issuer with each
// country's Tax Authority, and — from #454 — the Tax Invoices it issues to a
// Recipient (#450, ADR 0059, CONTEXT.md "Tax invoicing").
//
// It is a THIN CORE WITH A COUNTRY ADAPTER. This package holds what every
// country shares: an Issuer is a registration with one authority, points at
// one of that authority's environments, and — later — holds a signing
// certificate; a Tax Invoice has a number, a status, a Recipient snapshot and
// money. Everything one authority cares about that another would not — for
// Ecuador, the RUC and the SRI's establecimiento / punto de emisión numbering,
// the clave de acceso, the factura XML, the XAdES signature and the SOAP
// transport — belongs to that country's adapter behind the TaxAuthority seam
// below, in a sub-package named for the authority (`sri`). Nothing
// Ecuador-specific leaks into the core beyond a country column and a
// per-country detail row.
//
// The country code is visible in the route path (/operator/invoicing/issuers/ec)
// so that the seam is a fact of the API and not merely of the code.
package invoicing

import (
	"context"
	"time"
)

// Country is an ISO 3166-1 alpha-2 code, lower-case, naming which Tax
// Authority an Issuer is registered with. There is at most one Issuer per
// Country.
type Country string

// CountryEcuador is the only country with an adapter today; its authority is
// the SRI.
const CountryEcuador Country = "ec"

// Environment is which of the authority's environments an Issuer points at.
// The words are the platform's rather than any authority's codes — the SRI
// says ambiente 1 and 2 — so that the core never carries a value that means
// nothing in another country; the adapter translates.
type Environment string

const (
	// EnvironmentTest is the authority's certification environment: SRI
	// pruebas. Documents issued under it are real to the SRI and never real
	// to anyone else, and every surface that shows one badges it as test.
	EnvironmentTest Environment = "test"
	// EnvironmentProduction is the authority's live environment: SRI
	// producción.
	EnvironmentProduction Environment = "production"
)

// Valid reports whether e is one of the two environments.
func (e Environment) Valid() bool {
	return e == EnvironmentTest || e == EnvironmentProduction
}

// Issuer is the cross-country core of the platform's registration with one
// Tax Authority. Its country-specific details live on the adapter's detail
// row (EcuadorIssuerDetails for CountryEcuador); its signing certificate is
// in custody on the same row (#453), of which only the metadata is read here.
type Issuer struct {
	ID          string
	Country     Country
	Environment Environment
	// Certificate is the metadata of the signing certificate in custody, nil
	// when none has been uploaded. The sealed bytes and password are never on
	// this struct: they are opened into memory only at signing.
	Certificate *CertificateMetadata
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CertificateMetadata is what is known about the certificate in custody
// without opening it: what the Issuer page shows.
type CertificateMetadata struct {
	// Subject is the RFC 2253 distinguished name.
	Subject string
	// RUC is the 13-digit RUC found inside the certificate, or "" when none
	// could be discovered. Compared with the Issuer's own RUC for a warning,
	// never a refusal.
	RUC       string
	NotBefore time.Time
	NotAfter  time.Time
	// FingerprintSHA256 is the lowercase hex SHA-256 of the DER certificate.
	FingerprintSHA256 string
	UploadedAt        time.Time
}

// TaxAuthority is the seam a country adapter implements: the two things the
// core ever asks an authority to do. A prepared document is submitted, and the
// outcome of an earlier submission is asked for by the reference the authority
// knows it under (in Ecuador, the clave de acceso).
//
// The core drives the status machine — pending, authorized, not authorized,
// rejected — off these two calls and the attempts ledger it keeps around them;
// it never learns SOAP, XML or an authority's error codes. The adapter never
// touches a table. What "prepare a document" means is the adapter's business
// too (building, numbering and signing the factura), which is why the
// document arrives here already prepared.
//
// THE SHAPES BELOW ARE DELIBERATELY SMALL. #454 wires the first adapter and
// is expected to widen Outcome (authorization number and date, the authority's
// own XML) as the issue flow needs; nothing here should be read as final.
type TaxAuthority interface {
	// Submit hands a prepared document to the authority and reports how it was
	// received. A transport failure is an error; a refusal by the authority is
	// an Outcome, because the authority's messages are data the operator reads
	// and never an HTTP error (#450).
	Submit(ctx context.Context, doc PreparedDocument) (Outcome, error)
	// QueryOutcome asks the authority what became of the document it knows by
	// reference.
	QueryOutcome(ctx context.Context, authorityReference string) (Outcome, error)
}

// PreparedDocument is a document as the adapter built and signed it, ready to
// submit: the bytes the authority receives and the reference it will know
// them by.
type PreparedDocument struct {
	AuthorityReference string
	Body               []byte
}

// OutcomeState is the authority's verdict as the core understands it.
type OutcomeState string

const (
	// OutcomeReceived: the authority took the document and has not decided
	// yet (SRI RECIBIDA, EN PROCESAMIENTO). The core keeps the invoice pending.
	OutcomeReceived OutcomeState = "received"
	// OutcomeAuthorized: the document is a legal artifact.
	OutcomeAuthorized OutcomeState = "authorized"
	// OutcomeNotAuthorized: the authority examined it and said no (SRI NO
	// AUTORIZADO).
	OutcomeNotAuthorized OutcomeState = "not_authorized"
	// OutcomeRejected: the authority would not take it at all (SRI DEVUELTA).
	OutcomeRejected OutcomeState = "rejected"
	// OutcomeUnknown: the authority has no record of this reference. Asked
	// about the document, it answered about nothing (SRI autorización with
	// numeroComprobantes 0 and an empty autorizaciones list).
	//
	// THIS IS NOT OutcomeReceived (#514, parent #513). "Received" is the
	// authority saying it holds the document and has not finished with it;
	// "unknown" is it saying it never had one under this reference — which
	// is where a submit that died in transport leaves a document, and the
	// state the platform's first production factura sat in for a day while
	// every surface read it as held. Like received it decides nothing about
	// the invoice's status: the document stays pending, or needs_attention
	// past 24 h, exactly as any undecided answer leaves it. Unlike received
	// it is never an acknowledgement, so nothing may read it as one.
	OutcomeUnknown OutcomeState = "unknown"
)

// Outcome is what an authority said, verbatim enough to show the operator.
type Outcome struct {
	State    OutcomeState
	Messages []AuthorityMessage
	// AlreadyHeld is set with OutcomeReceived when the authority reported
	// that it already holds a document under this reference and did NOT take
	// the one just submitted (SRI 43 "clave registrada", 70 "en
	// procesamiento"). The core treats it as "it is there — ask for the
	// outcome" and keeps the artifact on file as it was, since what the
	// authority holds is the earlier send (#455).
	AlreadyHeld bool
	// AuthorizationNumber and AuthorizationDate are set only when State is
	// OutcomeAuthorized: the authority's number for the legal artifact (in
	// Ecuador the clave de acceso itself) and when it granted it.
	AuthorizationNumber string
	AuthorizationDate   time.Time
	// AuthorityXML is the authority's own authorization document when State
	// is OutcomeAuthorized — the legal proof the invoice keeps — and nil
	// otherwise.
	AuthorityXML []byte
}

// AuthorityMessage is one message from the authority, kept in the authority's
// own vocabulary: the SRI's identificador, mensaje, informacionAdicional and
// tipo map onto these four fields and are shown to the operator unchanged.
//
// The JSON names are how the messages are stored (last_messages, the attempts
// ledger) and served, so the operator surface reads one shape everywhere.
type AuthorityMessage struct {
	Identifier     string `json:"identifier"`
	Message        string `json:"message"`
	AdditionalInfo string `json:"additional_info"`
	Type           string `json:"type"`
}
