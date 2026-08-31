package evidence

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
)

// `evidence.pdf` — THE RECORD'S READING (#568), in ADR 0062's shape.
//
// It is the RIDE's design applied to a different document: rendered in Go with
// go-pdf/fpdf because the Cloud Run image has no browser and gets none, built
// from stored data only, produced on demand and never persisted, with fixed
// metadata so that two renders are byte-identical. Private block methods on a
// `page`, one exported entry point, and a test that inflates the streams rather
// than switching compression off.
//
// IT IS RENDERED FROM `record.json`'s OWN VALUE and from nothing else. The JSON
// is the record; this is a reading of it; and a reading assembled from a second
// pass over the inputs would be a second story about one person's evidence. So
// renderEvidencePDF takes the recordFile, not the Input.
//
// IT SAYS OUT LOUD WHAT THE FIELDS MEAN. The three paragraphs the pack must
// state — that `email_proven` is what makes a consent valid, and the two
// limitations — are printed here in the same constants record.json carries, on
// the first page, before the data. A regulator reading a printout must not have
// to open the JSON to find the caveats.
//
// THE TEXTS ARE ANNEXED IN FULL, one annex per edition, every language, as the
// raw stored markdown. Not prettified and not interpreted: these are the bytes
// the fingerprint covers, so a rendering that "improved" them would print
// something that hashes to a different number than the one on the same page.

// The page geometry, in millimetres on A4 portrait — the RIDE's margins.
const (
	pdfMargin  = 15.0
	pdfContent = 210 - 2*pdfMargin

	pdfFont      = "Helvetica"
	pdfBodySize  = 9.0
	pdfSmallSize = 7.5
	pdfMonoFont  = "Courier"
	pdfH1Size    = 16.0
	pdfH2Size    = 11.5
	pdfLine      = 4.2
)

// The acts table's columns. Fixed widths, so a wide user agent wraps rather
// than pushing the edition column off the page.
var actColumns = []float64{26, 22, 52, 30, 50}

// page wraps the fpdf document with the cp1252 translator every drawing call
// needs: fpdf's core fonts are not Unicode, so "ó", "ñ" and an em-dash have to
// reach the page as their code-page bytes.
type page struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
}

// renderEvidencePDF draws the whole document.
func renderEvidencePDF(record recordFile) ([]byte, error) {
	p := newEvidencePage()
	p.cover(record)
	p.subject(record)
	p.customerActs(record)
	p.staffAcceptances(record)
	p.editionAnnexes(record)

	var buf bytes.Buffer
	if err := p.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("evidence: write evidence.pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// pdfEpoch is the creation and modification date stamped into the file.
//
// FIXED, for the archive's reason and for the RIDE's: fpdf writes the current
// time into CreationDate and ModDate unless told otherwise, which would make
// two packs over one record differ by the second they were built. The RIDE uses
// the authorization date because that is the one instant the document itself
// vouches for; a pack vouches for no instant at all — its whole claim is that
// it is a function of the acts — so the value here is a constant that cannot be
// mistaken for a fact about anybody.
var pdfEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// newEvidencePage prepares the document with the RIDE's determinism settings:
// fixed dates, fixed producer and creator, and a sorted catalog, without which
// fpdf numbers its font objects by walking a map and two renders swap object
// numbers.
func newEvidencePage() *page {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(true)
	pdf.SetCatalogSort(true)
	pdf.SetProducer("Ticket POS", true)
	pdf.SetCreator("Ticket POS", true)
	pdf.SetCreationDate(pdfEpoch)
	pdf.SetModificationDate(pdfEpoch)
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	pdf.SetAutoPageBreak(true, pdfMargin)
	pdf.SetCellMargin(1)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetTextColor(0, 0, 0)
	pdf.AddPage()
	return &page{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("")}
}

// ---- Blocks ------------------------------------------------------------

// cover is the first page: what the document is, what makes a consent valid,
// and the two limitations — before any data, because they are what stops the
// data being misread.
func (p *page) cover(record recordFile) {
	p.heading("Consent Evidence Pack", pdfH1Size)
	p.paragraph(record.Pack.About)
	p.gap()

	p.heading("What makes a consent valid", pdfH2Size)
	p.paragraph(record.Pack.WhatMakesAConsentValid)
	p.gap()

	p.heading("What this document does not say", pdfH2Size)
	for _, limitation := range record.Pack.Limitations {
		p.paragraph(limitation)
	}
	p.gap()

	p.heading("How the fingerprints are checked", pdfH2Size)
	p.paragraph("Every legal edition named below carries a content hash (" +
		record.Pack.PreimageRule.Algorithm + "). The rule that produces it is restated here so this " +
		"pack can be verified against nothing but itself and the files it carries.")
	for i, step := range record.Pack.PreimageRule.Steps {
		p.paragraph(fmt.Sprintf("%d. %s", i+1, step))
	}
	p.paragraph(record.Pack.PreimageRule.Example)
	p.gap()

	p.heading("Why there is no date inside this file", pdfH2Size)
	p.paragraph(record.Pack.Determinism)
	p.paragraph("Pack format: " + record.Pack.Format)
}

// subject is who the pack is about and what is true of them now.
func (p *page) subject(record recordFile) {
	p.pdf.AddPage()
	p.heading("Subject", pdfH1Size)
	p.fact("Email address", record.Subject.Email)
	p.fact("Records held", strings.Join(record.Subject.Populations, ", "))
	p.gap()

	if record.Customer == nil {
		p.paragraph("No Customer holds this address. There is no Customer record, and therefore no " +
			"consent history, for it.")
		return
	}

	p.heading("Customer record", pdfH2Size)
	p.fact("Customer id", record.Customer.ID)
	p.fact("Name", strings.TrimSpace(record.Customer.FirstName+" "+record.Customer.LastName))
	p.fact("Email on the Customer record", record.Customer.Email)
	p.fact("Privacy Policy accepted", p.acceptanceLine(record.Customer.Policy))
	p.fact("Terms and Conditions accepted", p.acceptanceLine(record.Customer.Terms))
	p.fact("Marketing consent", word(record.Customer.MarketingConsent, "never asked"))
	p.fact("Networking consent", word(record.Customer.NetworkingConsent, "never asked"))
	p.gap()
	p.small("Each acceptance names the exact edition accepted. Whether that edition still satisfies the " +
		"gate today is a question about today and is deliberately not stated in this pack.")
}

// acceptanceLine renders one gate's last acceptance as a sentence, spelling the
// absence in words rather than as a blank.
func (p *page) acceptanceLine(acceptance Acceptance) string {
	if acceptance.AcceptedAt == nil && acceptance.Edition == nil {
		return "never accepted"
	}
	when := "not recorded"
	if acceptance.AcceptedAt != nil {
		when = acceptance.AcceptedAt.UTC().Format(time.RFC3339)
	}
	edition := "not recorded"
	if acceptance.Edition != nil {
		edition = editionName(*acceptance.Edition)
	}
	return when + " (edition " + edition + ")"
}

// customerActs is the consent history as a table — one row per act, newest
// first, exactly the rows record.json carries.
//
// THE WITHDRAWALS ARE IN HERE AND NOWHERE ELSE. There is no separate withdrawal
// section, because a withdrawal is one of these rows: its `prior` values say
// what it took away, and `denied` looks identical whether somebody gave
// something up or refused twice.
func (p *page) customerActs(record recordFile) {
	p.pdf.AddPage()
	p.heading("Consent acts", pdfH1Size)
	if len(record.CustomerActs) == 0 {
		p.paragraph("This address has performed no consent acts. That is the complete answer, not a " +
			"truncated one.")
		return
	}
	p.small(fmt.Sprintf("%d act(s), newest first. A null answer is a box that was NOT SHOWN on that "+
		"surface and is not a refusal; a null technical field is something the surface did not collect.",
		len(record.CustomerActs)))
	p.gap()

	p.tableHeader([]string{"Captured (UTC)", "Channel", "Answers", "Editions", "Evidence"})
	for _, act := range record.CustomerActs {
		p.tableRow([]string{
			act.CapturedAt.UTC().Format("2006-01-02\n15:04:05"),
			act.Channel,
			actAnswers(act),
			actEditions(act),
			actEvidence(act),
		})
	}
}

// staffAcceptances is the staff half.
//
// PRINTED EVEN WHEN EMPTY, with the limitation restated beside it, because the
// one misreading this section invites is exactly the one #568 forbids: "no
// staff acceptances" means no rows for this address and never "not staff".
func (p *page) staffAcceptances(record recordFile) {
	p.pdf.AddPage()
	p.heading("Staff platform acceptances", pdfH1Size)
	if record.Staff == nil || len(record.Staff.Acceptances) == 0 {
		p.paragraph("There are no Terms acceptances recorded against this address on the Staff platform.")
		p.paragraph(packLimitationStaff)
		return
	}
	p.small(fmt.Sprintf("%d acceptance(s), newest first. This history is complete: a person holds at "+
		"most one acceptance per edition per capacity.", len(record.Staff.Acceptances)))
	p.gap()

	p.tableHeader([]string{"Accepted (UTC)", "Capacity", "Edition", "Language shown", "Evidence"})
	for _, acceptance := range record.Staff.Acceptances {
		p.tableRow([]string{
			acceptance.AcceptedAt.UTC().Format("2006-01-02\n15:04:05"),
			acceptance.Capacity,
			editionName(acceptance.TermsEdition),
			word(acceptance.PresentedLocale, "not recorded"),
			strings.Join(compact([]string{
				labelled("IP", acceptance.IP),
				labelled("Session", acceptance.SessionID),
				labelled("Origin", acceptance.OriginURL),
				labelled("Agent", acceptance.UserAgent),
			}), "\n"),
		})
	}
	p.gap()
	p.small(packLimitationStaff)
}

// editionAnnexes prints every edition the pack names: its identity, its
// fingerprint, the preimage that produces it, and then the full text of every
// artifact in every language.
//
// EVERY LANGUAGE, ALWAYS. The fingerprint spans them at once, so an annex
// carrying only the language somebody was shown would print a hash nobody could
// reproduce from the pages beside it.
func (p *page) editionAnnexes(record recordFile) {
	for _, edition := range record.Editions {
		p.pdf.AddPage()
		p.heading("Annex: "+documentTitle(edition.Document)+", edition "+edition.Label, pdfH1Size)
		p.fact("Edition id", edition.ID)
		p.fact("Published as", edition.PublishedAs)
		p.fact("Effective from", edition.EffectiveDate)
		p.fact("Languages published", strings.Join(edition.Locales, ", "))
		p.fact("Content hash", edition.ContentHash)
		p.gap()

		p.heading("Fingerprint preimage", pdfH2Size)
		p.small("In order. Each field is framed as its byte length, a newline, then its bytes.")
		p.tableHeaderWidths([]string{"Field", "Bytes", "File in this pack"}, []float64{50, 20, 110})
		for _, field := range edition.Preimage {
			name := "locale " + field.Locale
			if field.Field == "artifact" {
				name = fmt.Sprintf("%s / %d-%s", field.Locale, *field.Ordinal, field.Slug)
			}
			p.tableRowWidths([]string{name, fmt.Sprintf("%d", field.Bytes), field.Path},
				[]float64{50, 20, 110})
		}
		p.gap()

		for _, field := range edition.Preimage {
			if field.Field != "artifact" {
				continue
			}
			p.pdf.AddPage()
			p.heading(fmt.Sprintf("%s [%s] %d-%s", documentTitle(edition.Document), field.Locale,
				*field.Ordinal, field.Slug), pdfH2Size)
			p.small("The exact stored text, as it is hashed and as it is served. " + field.Path)
			p.gap()
			p.body(p.textOf(record, field.Path))
		}
	}
}

// textOf is the stored text of one artifact, looked up by the SAME PATH the
// archive files it under and record.json points at. One lookup key for all
// three, so the annex cannot print text from a file the preimage list does not
// name.
func (p *page) textOf(record recordFile, path string) string {
	return record.annexBodies[path]
}

// ---- Drawing primitives -------------------------------------------------

func (p *page) heading(text string, size float64) {
	p.pdf.SetFont(pdfFont, "B", size)
	p.pdf.MultiCell(pdfContent, size*0.5, p.tr(text), "", "L", false)
	p.pdf.Ln(1)
}

func (p *page) paragraph(text string) {
	p.pdf.SetFont(pdfFont, "", pdfBodySize)
	p.pdf.MultiCell(pdfContent, pdfLine, p.tr(text), "", "L", false)
	p.pdf.Ln(1)
}

func (p *page) small(text string) {
	p.pdf.SetFont(pdfFont, "I", pdfSmallSize)
	p.pdf.MultiCell(pdfContent, pdfLine*0.85, p.tr(text), "", "L", false)
}

// body prints stored text in a monospaced face, so that markdown's own leading
// spaces, list markers and code fences survive the page as written.
func (p *page) body(text string) {
	p.pdf.SetFont(pdfMonoFont, "", pdfSmallSize)
	p.pdf.MultiCell(pdfContent, pdfLine*0.8, p.tr(text), "", "L", false)
}

// fact is a label and a value on one line, with the value never blank: an empty
// cell beside "Marketing consent" reads as a refusal.
func (p *page) fact(label, value string) {
	if strings.TrimSpace(value) == "" {
		value = "not recorded"
	}
	p.pdf.SetFont(pdfFont, "B", pdfBodySize)
	p.pdf.CellFormat(58, pdfLine+1, p.tr(label), "", 0, "L", false, 0, "")
	p.pdf.SetFont(pdfFont, "", pdfBodySize)
	p.pdf.MultiCell(pdfContent-58, pdfLine+1, p.tr(value), "", "L", false)
}

func (p *page) gap() { p.pdf.Ln(3) }

func (p *page) tableHeader(headings []string) { p.tableHeaderWidths(headings, actColumns) }

func (p *page) tableHeaderWidths(headings []string, widths []float64) {
	p.pdf.SetFont(pdfFont, "B", pdfSmallSize)
	for i, heading := range headings {
		p.pdf.CellFormat(widths[i], pdfLine+1, p.tr(heading), "B", 0, "L", false, 0, "")
	}
	p.pdf.Ln(-1)
}

func (p *page) tableRow(cells []string) { p.tableRowWidths(cells, actColumns) }

// tableRowWidths draws one row whose cells wrap independently, all ending at
// the same height — measured first, so a long user agent does not overlap the
// row below it.
func (p *page) tableRowWidths(cells []string, widths []float64) {
	p.pdf.SetFont(pdfFont, "", pdfSmallSize)
	lineHeight := pdfLine * 0.85

	height := lineHeight
	for i, cell := range cells {
		lines := p.pdf.SplitLines([]byte(p.tr(cell)), widths[i]-2)
		if h := float64(len(lines)) * lineHeight; h > height {
			height = h
		}
	}
	// A page break is taken BEFORE the row rather than inside it, so a row is
	// never split across two pages with half its cells on each.
	if p.pdf.GetY()+height > 297-pdfMargin {
		p.pdf.AddPage()
	}

	top := p.pdf.GetY()
	x := pdfMargin
	for i, cell := range cells {
		p.pdf.SetXY(x, top)
		p.pdf.MultiCell(widths[i], lineHeight, p.tr(cell), "", "L", false)
		x += widths[i]
	}
	p.pdf.SetXY(pdfMargin, top+height)
	p.pdf.Ln(1)
}

// ---- Value rendering ----------------------------------------------------

// actAnswers is one act's answers, each spelled in words. A null answer is
// "not shown", never a blank and never "no".
func actAnswers(act consentsvc.ConsentActItem) string {
	lines := []string{
		"Policy: " + answer(act.PolicyAcceptance),
		"Terms: " + answer(act.TermsAcceptance),
		"Marketing: " + answer(act.MarketingConsent) + prior(act.PriorMarketingConsent),
		"Networking: " + answer(act.NetworkingConsent) + prior(act.PriorNetworkingConsent),
	}
	return strings.Join(lines, "\n")
}

// actEditions names the editions the act was captured against, and the language
// actually rendered — including the difference between "this surface showed no
// document" and "it did and we do not know which language".
func actEditions(act consentsvc.ConsentActItem) string {
	lines := []string{"Policy: " + editionName(act.PolicyEdition)}
	if act.TermsEdition != nil {
		lines = append(lines, "Terms: "+editionName(*act.TermsEdition))
	} else {
		lines = append(lines, "Terms: not shown")
	}
	switch {
	case act.PresentedLocale == nil:
		lines = append(lines, "Language: no document shown")
	case *act.PresentedLocale == nil:
		lines = append(lines, "Language: not recorded")
	default:
		lines = append(lines, "Language: "+**act.PresentedLocale)
	}
	return strings.Join(lines, "\n")
}

// actEvidence is what makes the act stand up, with the one field that does so
// stated first and named as such.
func actEvidence(act consentsvc.ConsentActItem) string {
	proven := "email NOT proven"
	if act.EmailProven {
		proven = "email proven"
	}
	lines := []string{proven, "Address: " + act.Email}
	lines = append(lines, compact([]string{
		labelled("IP", act.IP),
		labelled("Session", act.SessionID),
		labelled("Origin", act.OriginURL),
		labelled("Agent", act.UserAgent),
		labelled("Recorded by", act.RecordedBy),
		labelled("Request", act.RequestReference),
	})...)
	if act.ConfirmedAt != nil {
		lines = append(lines, "Confirmed: "+act.ConfirmedAt.UTC().Format(time.RFC3339))
	}
	if act.ConfirmationSentAt != nil {
		lines = append(lines, "Told: "+act.ConfirmationSentAt.UTC().Format(time.RFC3339))
	}
	return strings.Join(lines, "\n")
}

// answer spells a nullable answer. NULL IS "not shown" AND NEVER A BLANK: this
// is the one substitution that would turn a box nobody was offered into a
// refusal they never made.
func answer(value *bool) string {
	if value == nil {
		return "not shown"
	}
	if *value {
		return "yes"
	}
	return "no"
}

// prior renders what a consent was immediately before the act, which is the
// only way to read whether the act took something away.
func prior(value *string) string {
	if value == nil {
		return ""
	}
	return " (was " + *value + ")"
}

func labelled(label string, value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return ""
	}
	return label + ": " + *value
}

func compact(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

func word(value *string, absent string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return absent
	}
	return *value
}

// editionName renders an edition reference. An id whose label could not be read
// keeps its id and loses only its label — dropping the reference would erase the
// one thing tying the act to bytes.
func editionName(ref consentsvc.LegalEditionRef) string {
	if ref.ID == "" {
		return "none"
	}
	if ref.Label == "" {
		return ref.ID + " (label not recorded)"
	}
	return ref.Label + " (" + ref.ID + ")"
}

func documentTitle(document string) string {
	if document == DocumentTerms {
		return "Terms and Conditions"
	}
	return "Privacy Policy"
}
