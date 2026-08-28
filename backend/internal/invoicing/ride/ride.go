// Package ride renders the RIDE — the Representación Impresa del Documento
// Electrónico — of an authorized Tax Invoice or Credit Note (#494, #495,
// #489, ADR 0062).
//
// The RIDE is the authorized document's reading, not its record: it is
// produced from the stored data each time it is asked for, never persisted,
// and refused for any document that is not authorized, since a RIDE without
// the número de autorización has no validity (SRI FAQ S6 Q20). Everything
// on the page is a stored fact — the Issuer snapshot, the Recipient, the
// lines with the arithmetic the document carries for them, the totals, the
// clave, the authorization number and date — and the formatters are the
// `sri` kit's, so a figure on the RIDE is the figure in the XML to the cent
// and nothing here recomputes one. What the Invoice row does not carry —
// the factura a Credit Note modifies, its motivo and valor de modificación
// — is read out of the stored signed XML itself (ADR 0062 §1), never from
// a second row.
//
// The page is drawn with go-pdf/fpdf and the clave's Code 128 barcode with
// boombuler/barcode, both pure Go: the Cloud Run image has no browser and
// gets none. The output is deterministic — fixed metadata, fixed dates, a
// fixed image encoding — so a buyer's RIDE and an operator's are the same
// bytes, and a document authorized before this shipped renders the same
// way the day somebody first asks.
//
// This package imports `invoicing` and `invoicing/sri`; `invoicing` must
// never import it back, which is why the refusal it raises is declared in
// `invoicing/downloads.go` beside the XML downloads' refusals.
//
// The RIDE is in Spanish throughout and its labels are fixed here, not in
// the Staff app's catalogs: it is an SRI document read by buyers and their
// accountants, in the Ficha Técnica's words, whoever downloads it.
package ride

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"

	"github.com/beevik/etree"
	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/go-pdf/fpdf"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Render renders the RIDE of an authorized document from stored data only,
// deterministically. It refuses with invoicing.ErrRIDENotFound() when the
// document is not authorized, has no Ecuador detail row, no authorization
// on file, no Issuer snapshot or no signed bytes — the cases in which the
// page would have to invent a legal fact.
//
// The document is `<clave>.pdf`, served as application/pdf, the filename
// following the XML downloads' so the three files of one factura sort
// together.
//
// A Credit Note whose signed XML cannot be read is a render error, not a
// refusal: the document is authorized and its RIDE is owed, so the caller
// (the delivery step, #496) gets a wrapped error to log rather than a 404
// that would pass for "nothing to hand over".
func Render(inv *invoicing.Invoice, ec *invoicing.EcuadorInvoiceDetails) (*invoicing.Document, error) {
	if inv == nil || inv.Status != invoicing.InvoiceStatusAuthorized || ec == nil || ec.Authorization == nil || !inv.Signed() || inv.Issuer == nil {
		return nil, invoicing.ErrRIDENotFound()
	}
	number, err := documentNumber(ec)
	if err != nil {
		return nil, err
	}

	p := newPage(ec.Authorization.Date)
	switch inv.Kind {
	case invoicing.DocumentKindCreditNote:
		err = p.creditNote(inv, ec, number)
	case invoicing.DocumentKindManual, invoicing.DocumentKindSale:
		err = p.factura(inv, ec, number)
	default:
		err = fmt.Errorf("ride: unknown document kind %q", inv.Kind)
	}
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := p.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("ride: write pdf: %w", err)
	}
	return &invoicing.Document{
		Filename:    ec.AccessKey + ".pdf",
		ContentType: invoicing.ContentTypePDF,
		Body:        buf.Bytes(),
	}, nil
}

// documentNumber is the estab-ptoEmi-secuencial the document prints, the
// same three parts the clave and the XML carry.
func documentNumber(ec *invoicing.EcuadorInvoiceDetails) (string, error) {
	seq, err := sri.FormatSequential(ec.Secuencial)
	if err != nil {
		return "", fmt.Errorf("ride: %w", err)
	}
	return ec.Estab + "-" + ec.PtoEmi + "-" + seq, nil
}

// The page geometry, in millimetres on A4 portrait: a 10 mm margin, two
// equal columns for the header and the footer, and a lines table across
// the full width.
const (
	pageMargin  = 10.0
	contentW    = 210 - 2*pageMargin
	columnGap   = 4.0
	columnW     = (contentW - columnGap) / 2
	rightColumn = pageMargin + columnW + columnGap
	boxPad      = 2.0
	bandGap     = 3.0

	fontFamily = "Helvetica"
	bodySize   = 8.0
	labelSize  = 7.0
	titleSize  = 14.0
	barcodeH   = 12.0
)

// page wraps the fpdf document with the two things every drawing call
// needs: the cp1252 translator, because fpdf's core fonts are not Unicode
// and "ó", "ñ", "É" must reach the page as their code-page bytes; and the
// band being drawn, so two side-by-side boxes end at the same height.
type page struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	// bandTop and bandBottom track the band under construction (see band).
	bandTop, bandBottom float64
}

// newPage prepares an A4 document whose metadata is fixed so that two
// renders are byte-identical: fpdf stamps the current time into
// CreationDate and ModDate unless told otherwise, and the authorization
// date is the one instant the document itself vouches for. The producer
// and creator strings are fixed, the file ID fpdf writes is constant, and
// compression is on as it is in production — a test that inspects the
// text inflates the streams rather than switching it off. The catalog
// sort matters as much as the dates: fpdf numbers its font objects by
// walking a map, and without it the two Helvetica faces swap object
// numbers from one render to the next.
func newPage(authorizedAt time.Time) *page {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(true)
	pdf.SetCatalogSort(true)
	pdf.SetProducer("Ticket POS", true)
	pdf.SetCreator("Ticket POS", true)
	pdf.SetCreationDate(authorizedAt.UTC())
	pdf.SetModificationDate(authorizedAt.UTC())
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, pageMargin)
	pdf.SetCellMargin(1)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetTextColor(0, 0, 0)
	pdf.AddPage()
	return &page{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("")}
}

// ---- The two layouts ---------------------------------------------------

// layout is what tells a Credit Note's RIDE from a factura's; everything
// else on the page is the one document layout below. The two differ in
// three places the Ficha names: the title, the documento modificado band a
// Credit Note carries between the Recipient and the lines, and the forma
// de pago a factura carries under the información adicional.
type layout struct {
	title string
	// modified is the documento modificado band, drawn when set: only a
	// Credit Note has a document it modifies.
	modified *modifiedDocument
	// payment draws the forma de pago band. A Credit Note returns money, it
	// is not paid for, so it has none.
	payment bool
}

// factura draws the Ficha's factura RIDE: the emisor beside the
// authorization block, the Recipient, the lines, and a footer with the
// información adicional and forma de pago beside the totals.
func (p *page) factura(inv *invoicing.Invoice, ec *invoicing.EcuadorInvoiceDetails, number string) error {
	return p.document(inv, ec, number, layout{title: "FACTURA", payment: true})
}

// creditNote draws the Ficha's nota de crédito RIDE (#495, ADR 0062 §3):
// the same emisor, authorization, Recipient, lines and totals sections as
// the factura, headed "NOTA DE CRÉDITO", with the documento modificado
// band — the factura it credits, the motivo and the valor de modificación
// — between the Recipient and the lines, and no forma de pago.
func (p *page) creditNote(inv *invoicing.Invoice, ec *invoicing.EcuadorInvoiceDetails, number string) error {
	modified, err := parseModifiedDocument(inv.SignedXML)
	if err != nil {
		return err
	}
	return p.document(inv, ec, number, layout{title: "NOTA DE CRÉDITO", modified: &modified})
}

// document draws the one RIDE page, the layout naming what the kind adds:
// the emisor beside the authorization block, the Recipient, the documento
// modificado band where there is one, the lines, and a footer with the
// información adicional (and the forma de pago where there is one) beside
// the totals.
func (p *page) document(inv *invoicing.Invoice, ec *invoicing.EcuadorInvoiceDetails, number string, l layout) error {
	p.pdf.SetTitle(l.title+" "+number, true)

	p.beginBand()
	p.column(pageMargin, columnW, func() { p.emisorBlock(inv.Issuer) })
	if err := p.columnErr(rightColumn, columnW, func() error {
		return p.authorizationBlock(l.title, number, inv, ec)
	}); err != nil {
		return err
	}
	p.endBand(box{pageMargin, columnW}, box{rightColumn, columnW})

	p.beginBand()
	p.column(pageMargin, contentW, func() { p.recipientBlock(inv) })
	p.endBand(box{pageMargin, contentW})

	if l.modified != nil {
		p.beginBand()
		p.column(pageMargin, contentW, func() { p.modifiedDocumentBlock(*l.modified) })
		p.endBand(box{pageMargin, contentW})
	}

	if err := p.linesTable(inv.Lines); err != nil {
		return err
	}
	p.pdf.Ln(bandGap)

	// The footer: información adicional (and forma de pago) on the left,
	// the totals on the right. The two are drawn from the same top and the
	// taller one sets where the page continues.
	top := p.pdf.GetY()
	p.beginBand()
	p.column(pageMargin, columnW, func() { p.additionalInfoBlock(inv) })
	p.endBand(box{pageMargin, columnW})
	if l.payment {
		p.beginBand()
		p.column(pageMargin, columnW, func() { p.paymentBlock(inv) })
		p.endBand(box{pageMargin, columnW})
	}
	leftBottom := p.pdf.GetY()

	p.pdf.SetXY(rightColumn, top)
	if err := p.totalsBlock(rightColumn, columnW, inv); err != nil {
		return err
	}
	if p.pdf.GetY() < leftBottom {
		p.pdf.SetY(leftBottom)
	}
	return p.pdf.Error()
}

// modifiedDocument is what a Credit Note's infoNotaCredito says about the
// factura it modifies and why, in the strings the XML carries: the date is
// already dd/mm/yyyy and the valor already two-decimal, so the RIDE prints
// them as they stand rather than parsing and re-formatting a figure the
// document has stated.
type modifiedDocument struct {
	docType  string // codDocModificado, "01" for a factura
	number   string // numDocModificado, estab-ptoEmi-secuencial
	issuedOn string // fechaEmisionDocSustento, dd/mm/yyyy
	motivo   string
	valor    string // valorModificacion
}

// parseModifiedDocument reads the modified document out of the Credit
// Note's stored signed XML — the one place the platform keeps it, since the
// Invoice row names the credited document by ID only and the RIDE must be
// deterministic from stored data without a second load (ADR 0062 §1). The
// elements are the ones sri.BuildNotaCredito writes under infoNotaCredito;
// the enveloped signature is a sibling of that block and is not looked at,
// so an unsigned body parses the same. A missing element is a render
// error: the XML was validated against the XSD when it was built, so one
// absent here is a corrupted record, not a document to render without it.
func parseModifiedDocument(signed []byte) (modifiedDocument, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		return modifiedDocument{}, fmt.Errorf("ride: parse credit note xml: %w", err)
	}
	info := doc.FindElement("/notaCredito/infoNotaCredito")
	if info == nil {
		return modifiedDocument{}, fmt.Errorf("ride: credit note xml has no infoNotaCredito")
	}
	var m modifiedDocument
	for _, f := range []struct {
		tag string
		dst *string
	}{
		{"codDocModificado", &m.docType},
		{"numDocModificado", &m.number},
		{"fechaEmisionDocSustento", &m.issuedOn},
		{"motivo", &m.motivo},
		{"valorModificacion", &m.valor},
	} {
		el := info.SelectElement(f.tag)
		if el == nil || el.Text() == "" {
			return modifiedDocument{}, fmt.Errorf("ride: credit note xml has no %s", f.tag)
		}
		*f.dst = el.Text()
	}
	return m, nil
}

// modifiedDocumentBlock is the Ficha's "comprobante que se modifica"
// section: the modified document's type and number, its fecha de emisión,
// the razón de modificación and the valor de modificación, each as a
// "Label: value" line a reader — or a test — finds verbatim.
func (p *page) modifiedDocumentBlock(m modifiedDocument) {
	p.labelled("Comprobante que se modifica", modifiedDocumentTypeLabel(m.docType)+" No. "+m.number)
	p.labelled("Fecha emisión (doc. sustento)", m.issuedOn)
	p.labelled("Razón de modificación", m.motivo)
	p.labelled("Valor de modificación", m.valor)
}

// modifiedDocumentTypeLabel names the modified document's codDoc. The
// platform only ever credits a factura; any other code is printed as the
// code rather than guessed at.
func modifiedDocumentTypeLabel(code string) string {
	if code == sri.DocumentTypeFactura {
		return "FACTURA"
	}
	return code
}

// ---- Shared sections ---------------------------------------------------
//
// Each block draws inside the column the caller opened (see column) and
// leaves the cursor below what it drew. document draws them in the Ficha's
// order; the layout says which of the optional ones appear.

// emisorBlock is the Issuer as it stood at signing: razón social, nombre
// comercial, both addresses, whether it keeps books, the RIMPE legend its
// régimen earns and the agente de retención resolution where it is one.
// The legends are the Ficha's, in the exact words the print view used.
func (p *page) emisorBlock(iss *invoicing.IssuerSnapshot) {
	p.text(iss.RazonSocial, "B", 10)
	if iss.NombreComercial != "" {
		p.text(iss.NombreComercial, "B", bodySize)
	}
	p.pdf.Ln(1)
	p.labelled("Dirección Matriz", iss.DireccionMatriz)
	p.labelled("Dirección Sucursal", iss.DireccionEstablecimiento)
	p.pdf.Ln(1)
	p.text("OBLIGADO A LLEVAR CONTABILIDAD: "+yesNo(iss.ObligadoContabilidad), "B", bodySize)
	switch iss.Regimen {
	case invoicing.RegimenRIMPEContribuyente:
		p.text("Contribuyente Régimen RIMPE", "B", bodySize)
	case invoicing.RegimenRIMPENegocioPopular:
		p.text("Contribuyente Negocio Popular - Régimen RIMPE", "B", bodySize)
	}
	if iss.AgenteRetencion != nil && *iss.AgenteRetencion != "" {
		p.text("Agente de Retención Resolución No. "+*iss.AgenteRetencion, "", bodySize)
	}
}

// authorizationBlock is the right-hand header: the RUC, the document's
// title and number, the authorization number and date, the ambiente and
// tipo de emisión, and the clave printed and as a Code 128 barcode.
func (p *page) authorizationBlock(title, number string, inv *invoicing.Invoice, ec *invoicing.EcuadorInvoiceDetails) error {
	p.text("R.U.C.: "+inv.Issuer.RUC, "B", 10)
	p.pdf.Ln(1)
	p.text(title, "B", titleSize)
	p.text("No. "+number, "B", 10)
	p.pdf.Ln(1)
	p.label("NÚMERO DE AUTORIZACIÓN")
	p.text(ec.Authorization.Number, "", bodySize)
	p.labelled("FECHA Y HORA DE AUTORIZACIÓN", ec.Authorization.Date.In(sri.Guayaquil).Format("02/01/2006 15:04:05"))
	p.labelled("AMBIENTE", ambienteLabel(inv.Environment))
	p.labelled("EMISIÓN", "NORMAL")
	p.pdf.Ln(1)
	p.label("CLAVE DE ACCESO")
	if err := p.barcode(ec.AccessKey); err != nil {
		return err
	}
	p.pdf.SetFont(fontFamily, "", labelSize)
	p.pdf.MultiCell(0, lineHeight(labelSize), p.tr(ec.AccessKey), "", "C", false)
	return nil
}

// recipientBlock is who the document is issued to, with the emission date
// beside the identification, as the Ficha lays it out.
func (p *page) recipientBlock(inv *invoicing.Invoice) {
	r := inv.Recipient
	p.labelled("Razón Social / Nombres y Apellidos", r.LegalName)
	p.labelled(taxIDLabel(r.TaxIDType), r.TaxID)
	p.labelled("Fecha Emisión", inv.IssuedOn.Format("02/01/2006"))
	if r.Address != "" {
		p.labelled("Dirección", r.Address)
	}
}

// The lines table's columns, in millimetres: they add up to contentW.
var lineColumns = []struct {
	title string
	w     float64
	align string
}{
	{"Descripción", 76, "L"},
	{"Cantidad", 18, "R"},
	{"Precio Unitario", 24, "R"},
	{"Descuento", 22, "R"},
	{"Tarifa IVA", 20, "R"},
	{"Precio Total", 30, "R"},
}

// linesTable draws one row per stored line. Cantidad is the stored
// millionths as the XML writes them, precio unitario and descuento the
// stored cents, tarifa the rate's percent, precio total the stored base —
// quantity × unit price − discount as the document carries it, never
// multiplied out again here.
func (p *page) linesTable(lines []invoicing.InvoiceLine) error {
	pdf := p.pdf
	h := lineHeight(bodySize)

	pdf.SetFont(fontFamily, "B", labelSize)
	pdf.SetFillColor(230, 230, 230)
	pdf.SetX(pageMargin)
	for _, c := range lineColumns {
		pdf.CellFormat(c.w, h, p.tr(c.title), "1", 0, c.align, true, 0, "")
	}
	pdf.Ln(h)

	pdf.SetFont(fontFamily, "", bodySize)
	for _, l := range lines {
		rate, err := rateLabel(l.IVARate)
		if err != nil {
			return fmt.Errorf("ride: line %d: %w", l.Position, err)
		}
		cells := []string{
			l.Description,
			sri.Quantity(l.QuantityMillionths).String(),
			sri.FormatCents(l.UnitPriceCents),
			sri.FormatCents(l.DiscountCents),
			rate,
			sri.FormatCents(l.BaseCents),
		}
		// A long description wraps; every other cell is one line, so the
		// row is as tall as the description turns out to be once drawn.
		// A row must never straddle a page — fpdf's own break inside the
		// description would leave the other cells behind — so the height
		// is estimated from the string's width first, one line to spare,
		// and the page is turned before the row when it would not fit.
		// (fpdf's SplitText is not used: it indexes a Latin-1 width table
		// by rune and panics on the cp1252 bytes the translator emits.)
		descW := lineColumns[0].w - 2
		estimatedLines := int(pdf.GetStringWidth(p.tr(l.Description))/descW) + 2
		_, pageH := pdf.GetPageSize()
		if pdf.GetY()+h*float64(estimatedLines) > pageH-pageMargin {
			pdf.AddPage()
		}
		top := pdf.GetY()
		pdf.SetXY(pageMargin, top)
		pdf.MultiCell(lineColumns[0].w, h, p.tr(cells[0]), "", lineColumns[0].align, false)
		rowH := max(pdf.GetY()-top, h)
		pdf.Rect(pageMargin, top, lineColumns[0].w, rowH, "D")
		x := pageMargin + lineColumns[0].w
		for i, c := range lineColumns[1:] {
			pdf.SetXY(x, top)
			pdf.CellFormat(c.w, rowH, p.tr(cells[i+1]), "1", 0, c.align, false, 0, "")
			x += c.w
		}
		pdf.SetXY(pageMargin, top+rowH)
	}
	return pdf.Error()
}

// totalsBlock is the Ficha's fixed totals skeleton, in its order: the
// subtotal under each rate, the subtotal sin impuestos, the descuento,
// the IVA and the valor total. The per-rate subtotals are the stored line
// bases grouped by rate — a sum of stored figures, not a recomputation —
// and the four document totals are the stored ones. A rate with no line
// prints 0.00, as the SRI's own RIDE does, so the same rows appear on
// every document and a reader knows where to look.
func (p *page) totalsBlock(x, w float64, inv *invoicing.Invoice) error {
	byRate := map[invoicing.IVARate]int64{}
	for _, l := range inv.Lines {
		byRate[l.IVARate] += l.BaseCents
	}
	general, err := sri.IVACode15.RatePercent()
	if err != nil {
		return fmt.Errorf("ride: %w", err)
	}
	rows := []struct {
		label string
		cents int64
		bold  bool
	}{
		{fmt.Sprintf("SUBTOTAL %d%%", general), byRate[invoicing.IVARate15], false},
		{"SUBTOTAL 0%", byRate[invoicing.IVARateZero], false},
		{"SUBTOTAL NO OBJETO DE IVA", byRate[invoicing.IVARateNoObjeto], false},
		{"SUBTOTAL EXENTO DE IVA", byRate[invoicing.IVARateExento], false},
		{"SUBTOTAL SIN IMPUESTOS", inv.SubtotalCents, false},
		{"DESCUENTO", inv.DiscountCents, false},
		{fmt.Sprintf("IVA %d%%", general), inv.IVACents, false},
		{"VALOR TOTAL", inv.TotalCents, true},
	}
	pdf := p.pdf
	h := lineHeight(bodySize)
	labelW := w - 30
	for _, r := range rows {
		style := ""
		if r.bold {
			style = "B"
		}
		pdf.SetFont(fontFamily, style, bodySize)
		pdf.SetX(x)
		pdf.CellFormat(labelW, h, p.tr(r.label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, h, p.tr(sri.FormatCents(r.cents)), "1", 1, "R", false, 0, "")
	}
	return pdf.Error()
}

// paymentBlock is the factura's forma de pago: the SRI label of the stored
// code, and the valor — the whole total, since the platform records one
// pago for the full amount. The Credit Note layout has no such block.
func (p *page) paymentBlock(inv *invoicing.Invoice) {
	p.label("FORMA DE PAGO")
	code := sri.PaymentMethod(inv.PaymentMethod)
	label := sri.PaymentMethodLabel(code)
	if label == "" {
		label = string(code)
	}
	pdf := p.pdf
	h := lineHeight(bodySize)
	pdf.SetFont(fontFamily, "", bodySize)
	x := pdf.GetX()
	w := columnW - 2*boxPad
	pdf.CellFormat(w-24, h, p.tr(label), "", 0, "L", false, 0, "")
	pdf.CellFormat(24, h, p.tr(sri.FormatCents(inv.TotalCents)), "", 1, "R", false, 0, "")
	pdf.SetX(x)
}

// additionalInfoBlock is the información adicional: the Recipient's email
// first, as the XML writes it under the name "email", then the operator's
// own fields in their stored order.
func (p *page) additionalInfoBlock(inv *invoicing.Invoice) {
	p.label("INFORMACIÓN ADICIONAL")
	if inv.Recipient.Email != "" {
		p.labelled("Email", inv.Recipient.Email)
	}
	for _, f := range inv.AdditionalFields {
		p.labelled(f.Name, f.Value)
	}
}

// ---- Vocabulary --------------------------------------------------------

func yesNo(b bool) string {
	if b {
		return "SI"
	}
	return "NO"
}

// ambienteLabel names the SRI environment the document was authorized in.
func ambienteLabel(env invoicing.Environment) string {
	if sri.AmbienteFor(env) == sri.EnvironmentProduction {
		return "PRODUCCIÓN"
	}
	return "PRUEBAS"
}

// taxIDLabel is the Recipient's identification named by its kind, as the
// print view named it. The kinds are the platform's three Tax ID Types; a
// stored type outside them cannot happen (the column is CHECKed) and would
// print under the generic caption rather than under the raw value.
func taxIDLabel(taxIDType string) string {
	switch taxIDType {
	case platform.TaxIDTypeRUC:
		return "RUC"
	case platform.TaxIDTypeCedula:
		return "Cédula"
	case platform.TaxIDTypePassport:
		return "Pasaporte"
	}
	return "Identificación"
}

// rateLabel is the tarifa column: the percent for a rated line, the
// Ficha's words for the two unrated kinds.
func rateLabel(rate invoicing.IVARate) (string, error) {
	switch rate {
	case invoicing.IVARateExento:
		return "Exento", nil
	case invoicing.IVARateNoObjeto:
		return "No objeto", nil
	}
	code, err := sri.IVACodeFor(rate)
	if err != nil {
		return "", err
	}
	percent, err := code.RatePercent()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d%%", percent), nil
}

// ---- Drawing primitives ------------------------------------------------

func lineHeight(size float64) float64 { return size * 0.5 }

// text writes one paragraph across the current column, wrapping at its
// right margin.
func (p *page) text(s, style string, size float64) {
	p.pdf.SetFont(fontFamily, style, size)
	p.pdf.MultiCell(0, lineHeight(size), p.tr(s), "", "L", false)
}

// label writes a small caption above a value.
func (p *page) label(s string) {
	p.pdf.SetFont(fontFamily, "B", labelSize)
	p.pdf.MultiCell(0, lineHeight(labelSize), p.tr(s), "", "L", false)
}

// labelled writes "Label: value" as one paragraph, so a test can find the
// pair verbatim and a reader never has to match a caption to a line.
func (p *page) labelled(label, value string) {
	p.text(label+": "+value, "", bodySize)
}

// beginBand opens a band of side-by-side boxes at the current Y.
func (p *page) beginBand() {
	p.bandTop = p.pdf.GetY()
	p.bandBottom = p.bandTop
}

// column draws inside a box at x with width w, from the band's top: the
// page margins are narrowed to the box for the duration so every wrapped
// paragraph stays inside it, and the band's bottom moves down to whatever
// the tallest column reached.
func (p *page) column(x, w float64, draw func()) {
	_ = p.columnErr(x, w, func() error { draw(); return nil })
}

func (p *page) columnErr(x, w float64, draw func() error) error {
	pdf := p.pdf
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	pdf.SetLeftMargin(x + boxPad)
	pdf.SetRightMargin(pageW - (x + w) + boxPad)
	pdf.SetXY(x+boxPad, p.bandTop+boxPad)
	err := draw()
	if bottom := pdf.GetY() + boxPad; bottom > p.bandBottom {
		p.bandBottom = bottom
	}
	pdf.SetLeftMargin(left)
	pdf.SetRightMargin(right)
	return err
}

// box is one column's horizontal extent, for endBand to frame.
type box struct{ x, w float64 }

// endBand frames each column drawn since beginBand — every rectangle as
// tall as the band — and moves the cursor below the band.
func (p *page) endBand(boxes ...box) {
	h := p.bandBottom - p.bandTop
	for _, b := range boxes {
		p.pdf.Rect(b.x, p.bandTop, b.w, h, "D")
	}
	p.pdf.SetXY(pageMargin, p.bandBottom+bandGap)
}

// ---- The barcode -------------------------------------------------------

// barcode draws the clave as Code 128 across the current column, then
// leaves the cursor below it.
func (p *page) barcode(key string) error {
	bc, err := claveBarcode(key)
	if err != nil {
		return err
	}
	encoded, err := barcodePNG(bc)
	if err != nil {
		return err
	}
	pdf := p.pdf
	name := "clave-" + key
	pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(encoded))
	x := pdf.GetX()
	y := pdf.GetY()
	w := columnW - 2*boxPad
	pdf.ImageOptions(name, x, y, w, barcodeH, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	pdf.SetXY(x, y+barcodeH+0.5)
	return pdf.Error()
}

// claveBarcode encodes the clave as Code 128 — the symbology the Ficha
// prescribes for the RIDE — scaled to whole-module width so the bars stay
// crisp. There is no decoder in the library; the returned Barcode's
// Content is the proof of what it encodes.
func claveBarcode(key string) (barcode.Barcode, error) {
	bc, err := code128.Encode(key)
	if err != nil {
		return nil, fmt.Errorf("ride: encode clave as Code 128: %w", err)
	}
	// Three pixels a module, one row of modules stretched to a readable
	// height; the PDF scales the image to the column, the pixel count only
	// bounds the resolution.
	scaled, err := barcode.Scale(bc, bc.Bounds().Dx()*3, 60)
	if err != nil {
		return nil, fmt.Errorf("ride: scale barcode: %w", err)
	}
	return scaled, nil
}

// barcodePNG encodes the barcode as an 8-bit grayscale PNG. The pixels are
// copied into an image.Gray first so the encoder sees the same buffer
// every time; Go's image/png is deterministic for the same input, which is
// what keeps two renders byte-identical.
func barcodePNG(bc barcode.Barcode) ([]byte, error) {
	b := bc.Bounds()
	img := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			img.SetGray(x, y, color.GrayModel.Convert(bc.At(b.Min.X+x, b.Min.Y+y)).(color.Gray))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("ride: encode barcode png: %w", err)
	}
	return buf.Bytes(), nil
}
