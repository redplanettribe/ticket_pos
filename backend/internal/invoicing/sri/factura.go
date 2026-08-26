package sri

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beevik/etree"
)

// FacturaVersion is the XML schema version this package emits (Ficha Anexo 3).
// Versions 2.x exist only for rubros de terceros (Anexo 7).
const FacturaVersion = "1.1.0"

// ErrInvalidFactura is wrapped by every validation failure of BuildFactura.
var ErrInvalidFactura = errors.New("sri: invalid factura")

// Regime is the Issuer's régimen, which decides the contribuyenteRimpe tag.
type Regime string

const (
	// RegimeGeneral emits no contribuyenteRimpe tag.
	RegimeGeneral Regime = "general"
	// RegimeRIMPE emits "CONTRIBUYENTE RÉGIMEN RIMPE" (Ficha Anexo 22).
	RegimeRIMPE Regime = "rimpe"
	// RegimeRIMPENegocioPopular emits "CONTRIBUYENTE NEGOCIO POPULAR - RÉGIMEN RIMPE"
	// (Ficha Anexo 22). The vendored factura_V1.1.0.xsd predates this value and
	// rejects it; the SRI's receiving validator accepts it.
	RegimeRIMPENegocioPopular Regime = "rimpe_popular"
)

const (
	rimpeTag               = "CONTRIBUYENTE RÉGIMEN RIMPE"
	rimpeNegocioPopularTag = "CONTRIBUYENTE NEGOCIO POPULAR - RÉGIMEN RIMPE"
)

// Issuer is the Ecuador emisor as it stood when the factura was issued —
// the snapshot ticket #454 stores on the Tax Invoice.
type Issuer struct {
	RUC                  string
	RazonSocial          string
	NombreComercial      string // optional
	DirMatriz            string
	DirEstablecimiento   string // optional
	Establishment        string // 3 digits, "estab"
	EmissionPoint        string // 3 digits, "ptoEmi"
	ObligadoContabilidad bool
	Regime               Regime
	AgenteRetencion      string // optional resolution number, digits, ≤ 8
}

// RecipientIDType is the SRI tipoIdentificacionComprador (Ficha Tabla 6).
// Consumidor final (07) and identificación del exterior (08) are deliberately
// absent: a consumidor-final factura can never be credited or annulled.
type RecipientIDType string

const (
	RecipientIDRUC      RecipientIDType = "04"
	RecipientIDCedula   RecipientIDType = "05"
	RecipientIDPassport RecipientIDType = "06"
)

// RecipientIDTypeFromTaxIDType maps the platform's Tax ID Type strings
// ("ruc", "cedula", "passport") onto SRI codes.
func RecipientIDTypeFromTaxIDType(taxIDType string) (RecipientIDType, error) {
	switch taxIDType {
	case "ruc":
		return RecipientIDRUC, nil
	case "cedula":
		return RecipientIDCedula, nil
	case "passport":
		return RecipientIDPassport, nil
	}
	return "", fmt.Errorf("%w: unknown tax id type %q", ErrInvalidFactura, taxIDType)
}

// Recipient is who the factura is issued to, as entered by the operator.
type Recipient struct {
	IDType    RecipientIDType
	ID        string
	LegalName string
	Address   string // optional
	// Email, when set, is written as the additional field "email" — the SRI
	// convention the RIDE and receiving software rely on. It counts towards
	// the XSD's limit of 15 additional fields.
	Email string
}

// IVACode is the SRI codigoPorcentaje for IVA (Ficha Tabla 17).
type IVACode string

const (
	IVACodeZero     IVACode = "0" // 0%
	IVACode15       IVACode = "4" // 15% (since 2024-04-01)
	IVACodeNoObjeto IVACode = "6" // no objeto de impuesto
	IVACodeExento   IVACode = "7" // exento de IVA
)

// taxCodeIVA is the SRI "codigo" for the IVA tax itself (Ficha Tabla 16).
const taxCodeIVA = "2"

// RatePercent is the tarifa the code stands for, in whole percent.
func (c IVACode) RatePercent() (int, error) {
	switch c {
	case IVACodeZero, IVACodeNoObjeto, IVACodeExento:
		return 0, nil
	case IVACode15:
		return 15, nil
	}
	return 0, fmt.Errorf("%w: unknown IVA code %q", ErrInvalidFactura, string(c))
}

// Quantity is a line quantity in millionths: the XML allows up to six
// decimals for cantidad on version 1.1.0.
type Quantity int64

// QuantityFromInt converts a whole quantity.
func QuantityFromInt(n int64) Quantity { return Quantity(n * 1_000_000) }

var quantityPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]{1,6}))?$`)

// ParseQuantity parses a decimal string with up to six fraction digits.
func ParseQuantity(s string) (Quantity, error) {
	m := quantityPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("%w: quantity %q must be a decimal with at most 6 fraction digits", ErrInvalidFactura, s)
	}
	whole, ok := new(big.Int).SetString(m[1], 10)
	if !ok {
		return 0, fmt.Errorf("%w: quantity %q", ErrInvalidFactura, s)
	}
	frac := m[2] + strings.Repeat("0", 6-len(m[2]))
	fracInt, _ := new(big.Int).SetString(frac, 10)
	total := new(big.Int).Mul(whole, big.NewInt(1_000_000))
	total.Add(total, fracInt)
	if !total.IsInt64() {
		return 0, fmt.Errorf("%w: quantity %q is too large", ErrInvalidFactura, s)
	}
	return Quantity(total.Int64()), nil
}

// String renders the quantity with its significant decimals, at least two
// ("2.00", "0.5" → "0.50", "0.333333").
func (q Quantity) String() string {
	neg := q < 0
	if neg {
		q = -q
	}
	whole := int64(q) / 1_000_000
	frac := fmt.Sprintf("%06d", int64(q)%1_000_000)
	for len(frac) > 2 && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	s := fmt.Sprintf("%d.%s", whole, frac)
	if neg {
		s = "-" + s
	}
	return s
}

// Line is one detalle.
type Line struct {
	Code           string // optional codigoPrincipal, ≤ 25 chars
	Description    string
	Quantity       Quantity
	UnitPriceCents int64
	DiscountCents  int64 // optional, ≤ quantity × unit price
	IVA            IVACode
	// IVAInclusive says UnitPriceCents is the price AS PAID, with the IVA
	// inside it — a Sale Invoice's line (#474, ADR 0060). The base is then
	// backed out of quantity × unit price (BackOutIVA) so that base + IVA
	// equals the cents paid, and the document's precioUnitario is the
	// backed-out unit price to six decimals rather than the paid one. A
	// discount is refused on such a line: the price paid is the whole story.
	IVAInclusive bool
}

// AdditionalField is one campoAdicional of infoAdicional.
type AdditionalField struct {
	Name  string
	Value string
}

// MaxAdditionalFields is the XSD's maxOccurs for campoAdicional.
const MaxAdditionalFields = 15

// MaxAdditionalFieldLength bounds both the name and the value (Ficha §9.11).
const MaxAdditionalFieldLength = 300

// PaymentMethod is a forma de pago code (Ficha Tabla 24 / Anexo 21).
type PaymentMethod string

// PaymentMethodDefault is "otros con utilización del sistema financiero", the
// platform's default because it is paid through payment providers and banks.
const PaymentMethodDefault PaymentMethod = "20"

// PaymentMethods lists the current forma de pago codes and their SRI labels,
// for the form and the RIDE.
var PaymentMethods = []struct {
	Code  PaymentMethod
	Label string
}{
	{"01", "SIN UTILIZACION DEL SISTEMA FINANCIERO"},
	{"15", "COMPENSACIÓN DE DEUDAS"},
	{"16", "TARJETA DE DÉBITO"},
	{"17", "DINERO ELECTRÓNICO"},
	{"18", "TARJETA PREPAGO"},
	{"19", "TARJETA DE CRÉDITO"},
	{"20", "OTROS CON UTILIZACION DEL SISTEMA FINANCIERO"},
	{"21", "ENDOSO DE TÍTULOS"},
}

// PaymentMethodCard is "tarjeta de crédito": what a Sale Invoice states
// for money a card processor collected (#473). PayPhone takes cards and
// reports the brand, never whether it was credit or debit; the credit code
// is the one the Ficha lists first for card payments and the one a
// processor-collected charge is conventionally filed under.
const PaymentMethodCard PaymentMethod = "19"

// PaymentMethodForProvider maps the Payment Provider that collected an
// Online Sale's money onto the forma de pago the document states. Every
// provider the platform has is a card processor; anything unknown falls to
// the platform's default, "otros con utilización del sistema financiero",
// which is true of any provider at all.
func PaymentMethodForProvider(provider string) PaymentMethod {
	switch provider {
	case "payphone":
		return PaymentMethodCard
	}
	return PaymentMethodDefault
}

// PaymentMethodLabel returns the SRI label of a code, or "" when unknown.
func PaymentMethodLabel(code PaymentMethod) string {
	for _, m := range PaymentMethods {
		if m.Code == code {
			return m.Label
		}
	}
	return ""
}

var paymentMethodPattern = regexp.MustCompile(`^(0[1-9]|1[0-9]|2[0-1])$`)

// Factura is everything needed to render a factura XML v1.1.0.
type Factura struct {
	Environment Environment
	Issuer      Issuer
	// AccessKey is the clave de acceso already allocated for this document;
	// its components must agree with the fields below (SRI error 58).
	AccessKey string
	// Sequential is the 9-digit secuencial.
	Sequential string
	// IssuedOn is the emission instant; the date is rendered in Guayaquil.
	IssuedOn  time.Time
	Recipient Recipient
	Lines     []Line
	// PaymentMethod defaults to PaymentMethodDefault when empty.
	PaymentMethod PaymentMethod
	// AdditionalFields are the operator's own campoAdicional entries.
	AdditionalFields []AdditionalField
}

// LineTotals is the arithmetic of one line, in cents.
//
// On an IVA-inclusive line GrossCents is what was paid, BaseCents the base
// backed out of it and IVACents the remainder, so the three still sum the
// same way: base + IVA = gross − discount.
type LineTotals struct {
	IVA           IVACode
	RatePercent   int
	GrossCents    int64 // quantity × unit price, rounded half up
	DiscountCents int64
	BaseCents     int64 // precioTotalSinImpuesto = gross − discount
	IVACents      int64 // base × rate, rounded half up
}

// TaxTotal is one totalImpuesto: the per-rate subtotal.
type TaxTotal struct {
	IVA         IVACode
	RatePercent int
	BaseCents   int64
	IVACents    int64
}

// Totals is the factura's arithmetic, all in integer cents.
type Totals struct {
	Lines []LineTotals
	// ByRate is ordered by IVA code, one entry per rate present.
	ByRate []TaxTotal
	// SubtotalCents is totalSinImpuestos: the sum of line bases.
	SubtotalCents int64
	// DiscountCents is totalDescuento.
	DiscountCents int64
	// IVACents is the sum of line IVA values.
	IVACents int64
	// TotalCents is importeTotal = SubtotalCents + IVACents.
	TotalCents int64
}

// ComputeTotals does the factura arithmetic for a set of lines, the same way
// BuildFactura does, so a form preview and the RIDE agree with the XML.
func ComputeTotals(lines []Line) (Totals, error) {
	if len(lines) == 0 {
		return Totals{}, fmt.Errorf("%w: at least one line is required", ErrInvalidFactura)
	}
	var t Totals
	byRate := map[IVACode]*TaxTotal{}
	for i, l := range lines {
		rate, err := l.IVA.RatePercent()
		if err != nil {
			return Totals{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		if l.Quantity <= 0 {
			return Totals{}, fmt.Errorf("%w: line %d: quantity must be positive", ErrInvalidFactura, i+1)
		}
		if l.UnitPriceCents < 0 {
			return Totals{}, fmt.Errorf("%w: line %d: unit price must not be negative", ErrInvalidFactura, i+1)
		}
		if l.DiscountCents < 0 {
			return Totals{}, fmt.Errorf("%w: line %d: discount must not be negative", ErrInvalidFactura, i+1)
		}
		gross, err := grossCents(l.Quantity, l.UnitPriceCents)
		if err != nil {
			return Totals{}, fmt.Errorf("%w: line %d: %v", ErrInvalidFactura, i+1, err)
		}
		if l.DiscountCents > gross {
			return Totals{}, fmt.Errorf("%w: line %d: discount exceeds the line amount", ErrInvalidFactura, i+1)
		}
		var base, iva int64
		if l.IVAInclusive {
			if l.DiscountCents != 0 {
				return Totals{}, fmt.Errorf("%w: line %d: an IVA-inclusive line takes no discount", ErrInvalidFactura, i+1)
			}
			if base, iva, err = BackOutIVA(gross, l.IVA); err != nil {
				return Totals{}, fmt.Errorf("line %d: %w", i+1, err)
			}
		} else {
			base = gross - l.DiscountCents
			iva = divRoundHalfUp(base*int64(rate), 100)
		}
		t.Lines = append(t.Lines, LineTotals{
			IVA: l.IVA, RatePercent: rate, GrossCents: gross, DiscountCents: l.DiscountCents, BaseCents: base, IVACents: iva,
		})
		t.SubtotalCents += base
		t.DiscountCents += l.DiscountCents
		t.IVACents += iva
		tt, ok := byRate[l.IVA]
		if !ok {
			tt = &TaxTotal{IVA: l.IVA, RatePercent: rate}
			byRate[l.IVA] = tt
		}
		tt.BaseCents += base
		tt.IVACents += iva
	}
	t.TotalCents = t.SubtotalCents + t.IVACents
	for _, tt := range byRate {
		t.ByRate = append(t.ByRate, *tt)
	}
	sort.Slice(t.ByRate, func(i, j int) bool { return t.ByRate[i].IVA < t.ByRate[j].IVA })
	return t, nil
}

func grossCents(q Quantity, unitCents int64) (int64, error) {
	prod := new(big.Int).Mul(big.NewInt(int64(q)), big.NewInt(unitCents))
	prod.Add(prod, big.NewInt(500_000))
	prod.Quo(prod, big.NewInt(1_000_000))
	if !prod.IsInt64() {
		return 0, errors.New("amount overflows")
	}
	return prod.Int64(), nil
}

func divRoundHalfUp(n, d int64) int64 {
	if n < 0 {
		return -((-n + d/2) / d)
	}
	return (n + d/2) / d
}

// FormatUnitPrice renders a line's base divided by its quantity as a unit
// price with up to six decimals, the most the v1.1.0 schema allows on
// precioUnitario: what an IVA-inclusive line prints, so that cantidad ×
// precioUnitario reconciles with precioTotalSinImpuesto to within a
// micro-dollar per unit (SRI error 52). Trailing zeros are trimmed down to
// two decimals ("8.695652", "8.70").
func FormatUnitPrice(baseCents int64, q Quantity) string {
	// micro-dollars = baseCents × 10⁴ ÷ (q ÷ 10⁶) = baseCents × 10¹⁰ ÷ q,
	// rounded half up.
	num := new(big.Int).Mul(big.NewInt(baseCents), big.NewInt(10_000_000_000))
	den := big.NewInt(int64(q))
	num.Add(num, new(big.Int).Quo(den, big.NewInt(2)))
	micro := new(big.Int).Quo(num, den)
	whole, frac := new(big.Int).QuoRem(micro, big.NewInt(1_000_000), new(big.Int))
	digits := fmt.Sprintf("%06d", frac.Int64())
	for len(digits) > 2 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
	}
	return whole.String() + "." + digits
}

// FormatCents renders cents with exactly two decimals ("1234" → "12.34").
func FormatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// BuiltFactura is the output of BuildFactura.
type BuiltFactura struct {
	// XML is the unsigned document: an XML declaration followed by the
	// canonical (C14N) serialisation of the prefix-free <factura> root.
	XML []byte
	// Totals is the arithmetic the XML carries.
	Totals Totals
}

// BuildFactura validates the input and renders the factura XML v1.1.0
// (Ficha Anexo 3): root <factura id="comprobante" version="1.1.0">, UTF-8,
// no namespace prefixes, two-decimal amounts, per-rate totalConImpuestos,
// one pago for the full total, up to 15 campoAdicional.
func BuildFactura(f Factura) (*BuiltFactura, error) {
	if err := validateDocument(&f, DocumentTypeFactura); err != nil {
		return nil, err
	}
	totals, err := ComputeTotals(f.Lines)
	if err != nil {
		return nil, err
	}

	root := etree.NewElement("factura")
	root.CreateAttr("id", "comprobante")
	root.CreateAttr("version", FacturaVersion)

	it := root.CreateElement("infoTributaria")
	text(it, "ambiente", string(f.Environment))
	text(it, "tipoEmision", TipoEmisionNormal)
	text(it, "razonSocial", f.Issuer.RazonSocial)
	if f.Issuer.NombreComercial != "" {
		text(it, "nombreComercial", f.Issuer.NombreComercial)
	}
	text(it, "ruc", f.Issuer.RUC)
	text(it, "claveAcceso", f.AccessKey)
	text(it, "codDoc", DocumentTypeFactura)
	text(it, "estab", f.Issuer.Establishment)
	text(it, "ptoEmi", f.Issuer.EmissionPoint)
	text(it, "secuencial", f.Sequential)
	text(it, "dirMatriz", f.Issuer.DirMatriz)
	if f.Issuer.AgenteRetencion != "" {
		text(it, "agenteRetencion", f.Issuer.AgenteRetencion)
	}
	switch f.Issuer.Regime {
	case RegimeRIMPE:
		text(it, "contribuyenteRimpe", rimpeTag)
	case RegimeRIMPENegocioPopular:
		text(it, "contribuyenteRimpe", rimpeNegocioPopularTag)
	}

	inf := root.CreateElement("infoFactura")
	text(inf, "fechaEmision", f.IssuedOn.In(Guayaquil).Format("02/01/2006"))
	if f.Issuer.DirEstablecimiento != "" {
		text(inf, "dirEstablecimiento", f.Issuer.DirEstablecimiento)
	}
	if f.Issuer.ObligadoContabilidad {
		text(inf, "obligadoContabilidad", "SI")
	} else {
		text(inf, "obligadoContabilidad", "NO")
	}
	text(inf, "tipoIdentificacionComprador", string(f.Recipient.IDType))
	text(inf, "razonSocialComprador", f.Recipient.LegalName)
	text(inf, "identificacionComprador", f.Recipient.ID)
	if f.Recipient.Address != "" {
		text(inf, "direccionComprador", f.Recipient.Address)
	}
	text(inf, "totalSinImpuestos", FormatCents(totals.SubtotalCents))
	text(inf, "totalDescuento", FormatCents(totals.DiscountCents))
	tci := inf.CreateElement("totalConImpuestos")
	for _, tt := range totals.ByRate {
		ti := tci.CreateElement("totalImpuesto")
		text(ti, "codigo", taxCodeIVA)
		text(ti, "codigoPorcentaje", string(tt.IVA))
		text(ti, "baseImponible", FormatCents(tt.BaseCents))
		text(ti, "tarifa", formatRate(tt.RatePercent))
		text(ti, "valor", FormatCents(tt.IVACents))
	}
	text(inf, "propina", "0.00")
	text(inf, "importeTotal", FormatCents(totals.TotalCents))
	text(inf, "moneda", "DOLAR")
	pago := inf.CreateElement("pagos").CreateElement("pago")
	text(pago, "formaPago", string(f.PaymentMethod))
	text(pago, "total", FormatCents(totals.TotalCents))

	dets := root.CreateElement("detalles")
	for i, l := range f.Lines {
		lt := totals.Lines[i]
		d := dets.CreateElement("detalle")
		if l.Code != "" {
			text(d, "codigoPrincipal", l.Code)
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

	fields := f.AdditionalFields
	if f.Recipient.Email != "" {
		fields = append([]AdditionalField{{Name: "email", Value: f.Recipient.Email}}, fields...)
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
	return &BuiltFactura{XML: xml, Totals: totals}, nil
}

func text(parent *etree.Element, tag, value string) {
	parent.CreateElement(tag).SetText(value)
}

func formatRate(percent int) string {
	return fmt.Sprintf("%d.00", percent)
}

// xmlDeclaration precedes every document this package emits.
const xmlDeclaration = `<?xml version="1.0" encoding="UTF-8"?>`

// serializeDocument writes the declaration and the canonical form of root.
// Canonical output is what the signature digests are computed over, so the
// bytes on the wire and the bytes that were hashed are the same bytes.
func serializeDocument(root *etree.Element) ([]byte, error) {
	body, err := canonicalize(root)
	if err != nil {
		return nil, err
	}
	return append([]byte(xmlDeclaration), body...), nil
}

var noNewline = regexp.MustCompile(`^[^\n]*$`)

// validateDocument checks the fields a factura and a nota de crédito share
// — the Issuer, the Recipient, the number, the clave, the lines and the
// additional fields — against the two schemas' common rules. docType is
// the codDoc the clave de acceso must have been computed under.
func validateDocument(f *Factura, docType string) error {
	if !f.Environment.Valid() {
		return fmt.Errorf("%w: environment %q", ErrInvalidFactura, f.Environment)
	}
	is := &f.Issuer
	if !regexp.MustCompile(`^[0-9]{10}001$`).MatchString(is.RUC) {
		return fmt.Errorf("%w: issuer RUC %q must be 13 digits ending in 001", ErrInvalidFactura, is.RUC)
	}
	if err := checkText("issuer razón social", is.RazonSocial, 1, 300); err != nil {
		return err
	}
	if err := checkText("issuer nombre comercial", is.NombreComercial, 0, 300); err != nil {
		return err
	}
	if err := checkText("issuer dirección matriz", is.DirMatriz, 1, 300); err != nil {
		return err
	}
	if err := checkText("issuer dirección establecimiento", is.DirEstablecimiento, 0, 300); err != nil {
		return err
	}
	if !digits3.MatchString(is.Establishment) {
		return fmt.Errorf("%w: establishment %q must be three digits", ErrInvalidFactura, is.Establishment)
	}
	if !digits3.MatchString(is.EmissionPoint) {
		return fmt.Errorf("%w: emission point %q must be three digits", ErrInvalidFactura, is.EmissionPoint)
	}
	if is.AgenteRetencion != "" && !regexp.MustCompile(`^[0-9]{1,8}$`).MatchString(is.AgenteRetencion) {
		return fmt.Errorf("%w: agente de retención %q must be up to eight digits", ErrInvalidFactura, is.AgenteRetencion)
	}
	switch is.Regime {
	case RegimeGeneral, RegimeRIMPE, RegimeRIMPENegocioPopular:
	default:
		return fmt.Errorf("%w: unknown régimen %q", ErrInvalidFactura, is.Regime)
	}

	r := &f.Recipient
	switch r.IDType {
	case RecipientIDRUC, RecipientIDCedula, RecipientIDPassport:
	default:
		return fmt.Errorf("%w: recipient identification type %q", ErrInvalidFactura, r.IDType)
	}
	if err := checkText("recipient identification", r.ID, 1, 20); err != nil {
		return err
	}
	if err := checkText("recipient legal name", r.LegalName, 1, 300); err != nil {
		return err
	}
	if err := checkText("recipient address", r.Address, 0, 300); err != nil {
		return err
	}
	if err := checkText("recipient email", r.Email, 0, MaxAdditionalFieldLength); err != nil {
		return err
	}

	if !digits9.MatchString(f.Sequential) {
		return fmt.Errorf("%w: sequential %q must be nine digits", ErrInvalidFactura, f.Sequential)
	}
	if f.IssuedOn.IsZero() {
		return fmt.Errorf("%w: emission date is zero", ErrInvalidFactura)
	}
	parsed, err := ParseAccessKey(f.AccessKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFactura, err)
	}
	expected := AccessKeyInput{
		IssuedOn: f.IssuedOn, DocumentType: docType, RUC: is.RUC, Environment: f.Environment,
		Establishment: is.Establishment, EmissionPoint: is.EmissionPoint, Sequential: f.Sequential,
		NumericCode: parsed.NumericCode,
	}
	if want, _ := NewAccessKey(expected); want != f.AccessKey {
		return fmt.Errorf("%w: clave de acceso components differ from the document (SRI error 58)", ErrInvalidFactura)
	}

	if f.PaymentMethod == "" {
		f.PaymentMethod = PaymentMethodDefault
	}
	if !paymentMethodPattern.MatchString(string(f.PaymentMethod)) {
		return fmt.Errorf("%w: forma de pago %q", ErrInvalidFactura, f.PaymentMethod)
	}

	if len(f.Lines) == 0 {
		return fmt.Errorf("%w: at least one line is required", ErrInvalidFactura)
	}
	for i, l := range f.Lines {
		if err := checkText(fmt.Sprintf("line %d code", i+1), l.Code, 0, 25); err != nil {
			return err
		}
		if err := checkText(fmt.Sprintf("line %d description", i+1), l.Description, 1, 300); err != nil {
			return err
		}
	}

	n := len(f.AdditionalFields)
	if r.Email != "" {
		n++
	}
	if n > MaxAdditionalFields {
		return fmt.Errorf("%w: %d additional fields (email included), the maximum is %d", ErrInvalidFactura, n, MaxAdditionalFields)
	}
	for i, af := range f.AdditionalFields {
		if err := checkLength(fmt.Sprintf("additional field %d name", i+1), af.Name, 1, MaxAdditionalFieldLength); err != nil {
			return err
		}
		if err := checkLength(fmt.Sprintf("additional field %d value", i+1), af.Value, 1, MaxAdditionalFieldLength); err != nil {
			return err
		}
	}
	return nil
}

// checkText enforces the XSD's [^\n]* pattern and length bounds (in runes).
func checkText(what, s string, min, max int) error {
	if err := checkLength(what, s, min, max); err != nil {
		return err
	}
	if !noNewline.MatchString(s) {
		return fmt.Errorf("%w: %s must not contain a newline", ErrInvalidFactura, what)
	}
	return nil
}

func checkLength(what, s string, min, max int) error {
	n := utf8.RuneCountInString(s)
	if n < min {
		return fmt.Errorf("%w: %s is required", ErrInvalidFactura, what)
	}
	if n > max {
		return fmt.Errorf("%w: %s is longer than %d characters", ErrInvalidFactura, what, max)
	}
	return nil
}
