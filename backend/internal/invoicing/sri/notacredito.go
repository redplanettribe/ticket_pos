package sri

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/beevik/etree"
)

// The nota de crédito (#476, parent #471, ADR 0060): the document that
// undoes a factura at the SRI — codDoc 04, its own secuencial, the same
// buyer, and a reference to the factura it modifies. Rendered against
// NotaCredito_V1.1.0.xsd (research §2.1, §2.9), which shares the factura's
// infoTributaria, detalles and infoAdicional and differs in the middle
// block: infoNotaCredito names the modified document (codDocModificado,
// numDocModificado, fechaEmisionDocSustento), states valorModificacion —
// the amount credited — and a motivo, has no pagos block, and its
// totalImpuesto carries no tarifa.
//
// THE ARITHMETIC IS THE FACTURA'S, to the cent. A Credit Note credits the
// whole Sale Invoice, so it is built from the same lines under the same
// IVA-inclusive back-out (ComputeTotals), and valorModificacion is the
// importeTotal the factura stated. Nothing here invents a second way of
// computing what was paid.

// NotaCreditoVersion is the XML schema version this package emits.
const NotaCreditoVersion = "1.1.0"

// ErrInvalidNotaCredito is wrapped by every validation failure of
// BuildNotaCredito, beside ErrInvalidFactura for the fields the two share.
var ErrInvalidNotaCredito = errors.New("sri: invalid nota de crédito")

// MaxMotivoLength is the XSD's bound on motivo.
const MaxMotivoLength = 300

// ModifiedDocument is the factura a nota de crédito undoes, as the SRI
// asks it to be named: the document type, the printed number
// "estab-ptoEmi-secuencial", and its emission date.
type ModifiedDocument struct {
	// DocumentType is the codDoc of the modified document; a Credit Note
	// credits a factura, DocumentTypeFactura.
	DocumentType string
	// Number is "001-002-000000123": the factura's estab, ptoEmi and
	// nine-digit secuencial (FormatNumber on the invoicing side).
	Number string
	// IssuedOn is the factura's emission instant; its date is rendered in
	// Guayaquil, as the factura rendered it.
	IssuedOn time.Time
}

var modifiedNumberPattern = regexp.MustCompile(`^[0-9]{3}-[0-9]{3}-[0-9]{9}$`)

// NotaCredito is everything needed to render a nota de crédito XML v1.1.0.
// The Issuer, Recipient, numbering and lines are the factura's own shapes;
// what is added is the document it modifies and why.
type NotaCredito struct {
	Environment Environment
	Issuer      Issuer
	// AccessKey is the clave de acceso allocated for this document, under
	// DocumentTypeNotaCredito; its components must agree with the fields
	// below (SRI error 58).
	AccessKey string
	// Sequential is the 9-digit secuencial, from the nota de crédito's own
	// sequence.
	Sequential string
	// IssuedOn is the emission instant; the date is rendered in Guayaquil.
	IssuedOn  time.Time
	Recipient Recipient
	// Lines are the credited lines: the factura's, priced as the factura
	// priced them.
	Lines []Line
	// Modifies is the factura this note undoes.
	Modifies ModifiedDocument
	// Motivo is the reason, as the SRI asks for one: the reversal route in
	// words.
	Motivo string
	// AdditionalFields are further campoAdicional entries beside the
	// Recipient's email.
	AdditionalFields []AdditionalField
}

// BuiltNotaCredito is the output of BuildNotaCredito.
type BuiltNotaCredito struct {
	// XML is the unsigned document: an XML declaration followed by the
	// canonical (C14N) serialisation of the prefix-free <notaCredito> root.
	XML []byte
	// Totals is the arithmetic the XML carries; TotalCents is the
	// valorModificacion.
	Totals Totals
}

// BuildNotaCredito validates the input and renders the nota de crédito XML
// v1.1.0: root <notaCredito id="comprobante" version="1.1.0">, UTF-8, no
// namespace prefixes, two-decimal amounts, per-rate totalConImpuestos
// without tarifa, valorModificacion equal to the credited total, no pagos,
// up to 15 campoAdicional.
func BuildNotaCredito(n NotaCredito) (*BuiltNotaCredito, error) {
	if err := validateNotaCredito(&n); err != nil {
		return nil, err
	}
	totals, err := ComputeTotals(n.Lines)
	if err != nil {
		return nil, err
	}

	root := etree.NewElement("notaCredito")
	root.CreateAttr("id", "comprobante")
	root.CreateAttr("version", NotaCreditoVersion)

	it := root.CreateElement("infoTributaria")
	text(it, "ambiente", string(n.Environment))
	text(it, "tipoEmision", TipoEmisionNormal)
	text(it, "razonSocial", n.Issuer.RazonSocial)
	if n.Issuer.NombreComercial != "" {
		text(it, "nombreComercial", n.Issuer.NombreComercial)
	}
	text(it, "ruc", n.Issuer.RUC)
	text(it, "claveAcceso", n.AccessKey)
	text(it, "codDoc", DocumentTypeNotaCredito)
	text(it, "estab", n.Issuer.Establishment)
	text(it, "ptoEmi", n.Issuer.EmissionPoint)
	text(it, "secuencial", n.Sequential)
	text(it, "dirMatriz", n.Issuer.DirMatriz)
	if n.Issuer.AgenteRetencion != "" {
		text(it, "agenteRetencion", n.Issuer.AgenteRetencion)
	}
	switch n.Issuer.Regime {
	case RegimeRIMPE:
		text(it, "contribuyenteRimpe", rimpeTag)
	case RegimeRIMPENegocioPopular:
		text(it, "contribuyenteRimpe", rimpeNegocioPopularTag)
	}

	inf := root.CreateElement("infoNotaCredito")
	text(inf, "fechaEmision", n.IssuedOn.In(Guayaquil).Format("02/01/2006"))
	if n.Issuer.DirEstablecimiento != "" {
		text(inf, "dirEstablecimiento", n.Issuer.DirEstablecimiento)
	}
	text(inf, "tipoIdentificacionComprador", string(n.Recipient.IDType))
	text(inf, "razonSocialComprador", n.Recipient.LegalName)
	text(inf, "identificacionComprador", n.Recipient.ID)
	if n.Issuer.ObligadoContabilidad {
		text(inf, "obligadoContabilidad", "SI")
	} else {
		text(inf, "obligadoContabilidad", "NO")
	}
	text(inf, "codDocModificado", n.Modifies.DocumentType)
	text(inf, "numDocModificado", n.Modifies.Number)
	text(inf, "fechaEmisionDocSustento", n.Modifies.IssuedOn.In(Guayaquil).Format("02/01/2006"))
	text(inf, "totalSinImpuestos", FormatCents(totals.SubtotalCents))
	text(inf, "valorModificacion", FormatCents(totals.TotalCents))
	text(inf, "moneda", "DOLAR")
	tci := inf.CreateElement("totalConImpuestos")
	for _, tt := range totals.ByRate {
		ti := tci.CreateElement("totalImpuesto")
		text(ti, "codigo", taxCodeIVA)
		text(ti, "codigoPorcentaje", string(tt.IVA))
		text(ti, "baseImponible", FormatCents(tt.BaseCents))
		text(ti, "valor", FormatCents(tt.IVACents))
	}
	text(inf, "motivo", n.Motivo)

	dets := root.CreateElement("detalles")
	for i, l := range n.Lines {
		lt := totals.Lines[i]
		d := dets.CreateElement("detalle")
		if l.Code != "" {
			text(d, "codigoInterno", l.Code)
		}
		text(d, "descripcion", l.Description)
		text(d, "cantidad", l.Quantity.String())
		if l.IVAInclusive {
			text(d, "precioUnitario", FormatUnitPrice(lt.BaseCents, l.Quantity))
		} else {
			text(d, "precioUnitario", FormatCents(l.UnitPriceCents))
		}
		text(d, "descuento", FormatCents(l.DiscountCents))
		text(d, "precioTotalSinImpuesto", FormatCents(lt.BaseCents))
		imp := d.CreateElement("impuestos").CreateElement("impuesto")
		text(imp, "codigo", taxCodeIVA)
		text(imp, "codigoPorcentaje", string(l.IVA))
		text(imp, "tarifa", formatRate(lt.RatePercent))
		text(imp, "baseImponible", FormatCents(lt.BaseCents))
		text(imp, "valor", FormatCents(lt.IVACents))
	}

	fields := n.AdditionalFields
	if n.Recipient.Email != "" {
		fields = append([]AdditionalField{{Name: "email", Value: n.Recipient.Email}}, fields...)
	}
	if len(fields) > 0 {
		ia := root.CreateElement("infoAdicional")
		for _, af := range fields {
			ca := ia.CreateElement("campoAdicional")
			ca.CreateAttr("nombre", af.Name)
			ca.SetText(af.Value)
		}
	}

	xml, err := serializeDocument(root)
	if err != nil {
		return nil, err
	}
	return &BuiltNotaCredito{XML: xml, Totals: totals}, nil
}

// validateNotaCredito checks the shared fields under the nota de crédito's
// codDoc, then what only this document has: the modified document and the
// motivo. The forma de pago the shared check defaults is irrelevant here —
// the schema has no pagos — and is never rendered.
func validateNotaCredito(n *NotaCredito) error {
	shared := Factura{
		Environment:      n.Environment,
		Issuer:           n.Issuer,
		AccessKey:        n.AccessKey,
		Sequential:       n.Sequential,
		IssuedOn:         n.IssuedOn,
		Recipient:        n.Recipient,
		Lines:            n.Lines,
		AdditionalFields: n.AdditionalFields,
	}
	if err := validateDocument(&shared, DocumentTypeNotaCredito); err != nil {
		return err
	}
	m := &n.Modifies
	if !digits2.MatchString(m.DocumentType) {
		return fmt.Errorf("%w: modified document type %q must be two digits", ErrInvalidNotaCredito, m.DocumentType)
	}
	if !modifiedNumberPattern.MatchString(m.Number) {
		return fmt.Errorf("%w: modified document number %q must be estab-ptoEmi-secuencial", ErrInvalidNotaCredito, m.Number)
	}
	if m.IssuedOn.IsZero() {
		return fmt.Errorf("%w: modified document emission date is zero", ErrInvalidNotaCredito)
	}
	if err := checkText("motivo", n.Motivo, 1, MaxMotivoLength); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidNotaCredito, err)
	}
	return nil
}
