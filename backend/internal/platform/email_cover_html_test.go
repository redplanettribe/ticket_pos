package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The Cover Image kinds' HTML parts (ADR 0082, #879): the Sale Confirmation,
// the Ticket Assignment and the Sale Re-addressing. The goldens and the
// structural assertions cover their shape; these cover what a reader is told
// and shown.

func coverFixtures() []mailFixture {
	return append(coverMailFixtures(LocaleEN), coverMailFixtures(LocaleES)...)
}

func coverKind(f mailFixture) string {
	name := strings.TrimSuffix(strings.TrimSuffix(f.name, "-en"), "-es")
	for _, variant := range []string{"-noimages", "-full"} {
		name = strings.TrimSuffix(name, variant)
	}
	return name
}

// All three kinds, in both languages, with renditions and without, and the
// Sale Confirmation with its consent block and without.
func TestCoverMailFixturesCoverEveryKind(t *testing.T) {
	noImages, consent := map[string]int{}, 0
	for _, f := range coverFixtures() {
		if strings.Contains(f.name, "-noimages-") {
			noImages[coverKind(f)]++
		}
		if strings.Contains(f.text, saleConfirmationConsentBodyCopy.in(f.locale)) {
			consent++
		}
	}
	if len(noImages) != 3 {
		t.Fatalf("no-rendition fixtures cover %d kinds, want 3: %v", len(noImages), noImages)
	}
	for kind, n := range noImages {
		if n != 2 {
			t.Errorf("%s has %d no-rendition fixtures, want one per language", kind, n)
		}
	}
	if consent != 2 {
		t.Errorf("%d Sale Confirmation fixtures carry the consent block, want one per language", consent)
	}
}

// Every line of the text part reads in the HTML part. A "Label: value" line
// reads as its value (the Event's name is the <h1>, the receipt's facts are
// in the details box), a lead ending in a colon reads without it (the button
// or the consent heading follows it), and a link is an href.
func TestCoverMailHTMLCarriesTheTextPart(t *testing.T) {
	for _, f := range coverFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, body)
			read := textOf(doc)
			hrefs := map[string]bool{}
			for _, a := range findAll(doc, func(n *html.Node) bool { return n.Data == "a" }) {
				hrefs[attr(a, "href")] = true
			}
			for _, line := range strings.Split(f.text, "\n") {
				line = strings.TrimSpace(line)
				switch {
				case line == "", strings.Contains(read, line), hrefs[line]:
					continue
				case strings.HasSuffix(line, ":") && strings.Contains(read, strings.TrimSuffix(line, ":")):
					continue
				}
				if _, value, ok := strings.Cut(line, ": "); ok && strings.Contains(read, value) {
					continue
				}
				t.Errorf("the HTML part does not say %q", line)
			}
		})
	}
}

// The block opens with the wide rendition, then the Event's name as the one
// <h1>, its date and venue, and the Organization with its Logo. With no
// renditions it starts at the eyebrow and the only image is the mark. The
// square rendition is the compact row's, never shown here.
func TestCoverMailShowsTheCoverImageBlock(t *testing.T) {
	event := goldenMailEvent()
	for _, f := range coverFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, body)
			read := textOf(doc)
			for _, want := range []string{event.OrganizationName, event.VenueName, formatEventDate(event.StartsAt, event.Timezone, f.locale)} {
				if !strings.Contains(read, want) {
					t.Errorf("the Cover Image block does not say %q", want)
				}
			}
			headings := findAll(doc, func(n *html.Node) bool { return n.Data == "h1" })
			if len(headings) != 1 || textOf(headings[0]) != "Noche de Jazz" {
				t.Fatalf("want one <h1> reading the Event's name, got %d", len(headings))
			}

			images := map[string]bool{}
			var first *html.Node
			for _, img := range findAll(doc, func(n *html.Node) bool { return n.Data == "img" }) {
				images[attr(img, "src")] = true
				if src := attr(img, "src"); src != goldenChrome.MarkURL {
					if attr(img, "alt") != "" {
						t.Errorf("<img src=%q> has alt %q, want empty: its name is printed beside it", src, attr(img, "alt"))
					}
					if first == nil {
						first = img
					}
				}
			}
			if images[event.SquareURL] {
				t.Errorf("the Cover Image block shows the square rendition")
			}
			withImages := !strings.Contains(f.name, "-noimages-")
			for _, src := range []string{event.WideURL, event.LogoURL} {
				if images[src] != withImages {
					t.Errorf("image %q shown = %v, want %v", src, images[src], withImages)
				}
			}
			if withImages && attr(first, "src") != event.WideURL {
				t.Errorf("the card opens with %q, want the wide rendition", attr(first, "src"))
			}
			if !withImages && len(images) != 1 {
				t.Errorf("with no renditions the mail shows %d images, want only the mark", len(images))
			}
		})
	}
}

// Each kind's eyebrow sits over the Event's name; only the Sale Confirmation's
// is the success green with a check.
func TestCoverMailEyebrows(t *testing.T) {
	want := map[string]map[Locale]string{
		"saleconfirmation": {LocaleEN: "Purchase confirmed", LocaleES: "Compra confirmada"},
		"ticketassignment": {LocaleEN: "A ticket for you", LocaleES: "Una entrada para usted"},
		"salereaddressing": {LocaleEN: "A purchase re-addressed to you", LocaleES: "Una compra redirigida a usted"},
	}
	for _, f := range coverFixtures() {
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		h1 := findAll(parseMail(t, body), func(n *html.Node) bool { return n.Data == "h1" })[0]
		eyebrow := h1.PrevSibling
		for eyebrow != nil && eyebrow.Type != html.ElementNode {
			eyebrow = eyebrow.PrevSibling
		}
		if eyebrow == nil || !strings.Contains(textOf(eyebrow), want[coverKind(f)][f.locale]) {
			t.Fatalf("%s: the <h1> is not under the eyebrow %q", f.name, want[coverKind(f)][f.locale])
		}
		if confirmed := hasClass(eyebrow, "t-success"); confirmed != (coverKind(f) == "saleconfirmation") {
			t.Errorf("%s: eyebrow in the success green = %v", f.name, confirmed)
		}
	}
}

// The Logo tile is painted white twice wherever a Logo shows: only the
// background-image gradient survives Gmail's forced inversion.
func TestCoverMailLogoTileIsPaintedWhiteTwice(t *testing.T) {
	logos := 0
	for _, f := range coverFixtures() {
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		for _, img := range findAll(parseMail(t, body), func(n *html.Node) bool {
			return n.Data == "img" && attr(n, "src") == goldenMailEvent().LogoURL
		}) {
			logos++
			style := attr(img.Parent, "style")
			if !hasClass(img.Parent, "tile") || !strings.Contains(style, "background-color:#ffffff") ||
				!strings.Contains(style, "background-image:linear-gradient(#ffffff,#ffffff)") {
				t.Errorf("%s: the Logo's tile is not painted white twice: %q", f.name, style)
			}
		}
	}
	if logos == 0 {
		t.Fatal("no fixture shows a Logo")
	}
}

// Each kind has exactly one button, to the text's own link: the Confirmation
// Link, the Assignment Link, the Re-addressing Link.
func TestCoverMailButtonIsTheTextsLink(t *testing.T) {
	labels := map[string]map[Locale]string{
		"saleconfirmation": {LocaleEN: "View your tickets", LocaleES: "Vea sus entradas"},
		"ticketassignment": {LocaleEN: "Accept your ticket", LocaleES: "Aceptar su entrada"},
		"salereaddressing": {LocaleEN: "Accept this purchase", LocaleES: "Aceptar esta compra"},
	}
	for _, f := range coverFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			buttons := findAll(parseMail(t, body), isPrimaryButton)
			if len(buttons) != 1 {
				t.Fatalf("found %d buttons, want 1", len(buttons))
			}
			if href := attr(buttons[0], "href"); !strings.Contains(f.text, ":\n"+href+"\n") {
				t.Errorf("the button goes to %q, which is not the text's link", href)
			}
			if got, want := textOf(buttons[0]), labels[coverKind(f)][f.locale]; got != want {
				t.Errorf("button reads %q, want %q", got, want)
			}
		})
	}
}

// With no Confirmation Link the receipt has no button, and drops the link's
// note and the Outstanding Answers sentence that points at it, as the text
// does.
func TestSaleConfirmationHTMLWithoutALinkHasNoButton(t *testing.T) {
	c := goldenSaleConfirmation(LocaleEN, goldenMailEvent())
	c.ConfirmationLink, c.HasOutstandingAnswers = "", true
	body, err := c.HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := parseMail(t, body)
	if n := len(findAll(doc, isPrimaryButton)); n != 0 {
		t.Fatalf("found %d buttons with no Confirmation Link", n)
	}
	read := textOf(doc)
	for _, absent := range []string{saleConfirmationLinkNoteCopy.in(LocaleEN), saleConfirmationOutstandingCopy.in(LocaleEN)} {
		if strings.Contains(read, absent) {
			t.Errorf("with no link the HTML still says %q", absent)
		}
	}
	assertMailStructure(t, body, c.Text(), LocaleEN, goldenAssetOrigin, goldenMediaOrigin)
}

// The receipt's Reference is large and monospace in the details box, with the
// total and the Tax ID beside each other under it.
func TestSaleConfirmationHTMLDetailsBox(t *testing.T) {
	for _, f := range coverFixtures() {
		if coverKind(f) != "saleconfirmation" {
			continue
		}
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		doc := parseMail(t, body)
		refs := findAll(doc, func(n *html.Node) bool { return n.Data == "p" && hasClass(n, "ref") })
		if len(refs) != 1 || textOf(refs[0]) != "ABC123" || !strings.Contains(attr(refs[0], "style"), "monospace") {
			t.Fatalf("%s: want the Reference alone, monospace, in the details box", f.name)
		}
		box := refs[0].Parent
		read := textOf(box)
		for _, want := range []string{saleConfirmationReferenceLabelCopy.in(f.locale), saleConfirmationTotalLabelCopy.in(f.locale), "25.00 USD"} {
			if !strings.Contains(read, want) {
				t.Errorf("%s: the details box does not say %q", f.name, want)
			}
		}
		if hasTaxID := strings.Contains(read, "Cédula 1712345675"); hasTaxID != strings.Contains(f.text, "Cédula: 1712345675") {
			t.Errorf("%s: the details box shows the Tax ID = %v, unlike the text", f.name, hasTaxID)
		}
	}
}

// The consent block is its own section at the bottom of the card, behind a
// hairline, with the "Confirm my preferences" link to the consent URL; never
// a second button. A receipt with nothing pending has no such section.
func TestSaleConfirmationHTMLConsentBlock(t *testing.T) {
	for _, f := range coverFixtures() {
		if coverKind(f) != "saleconfirmation" {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, body)
			label := saleConfirmationConsentLinkCopy.in(f.locale)
			links := findAll(doc, func(n *html.Node) bool { return n.Data == "a" && strings.Contains(textOf(n), label) })
			pending := strings.Contains(f.text, saleConfirmationConsentBodyCopy.in(f.locale))
			if !pending {
				if len(links) != 0 {
					t.Fatalf("a receipt with nothing pending carries the consent link")
				}
				return
			}
			if len(links) != 1 || isPrimaryButton(links[0]) {
				t.Fatalf("want one plain %q link, got %d", label, len(links))
			}
			if href := attr(links[0], "href"); !strings.Contains(f.text, "\n"+href+"\n") {
				t.Errorf("the consent link goes to %q, which is not the text's", href)
			}
			section := links[0]
			for section != nil && section.Data != "td" {
				section = section.Parent
			}
			if section == nil || !strings.Contains(attr(section, "style"), "border-top:1px solid") {
				t.Fatalf("the consent block is not in a bordered section of its own")
			}
			for _, want := range []string{saleConfirmationConsentHeadingCopy.in(f.locale), saleConfirmationConsentBodyCopy.in(f.locale)} {
				if !strings.Contains(textOf(section), want) {
					t.Errorf("the consent section does not say %q", want)
				}
			}
			if next := section.Parent.NextSibling; next != nil && strings.TrimSpace(textOf(next)) != "" {
				t.Errorf("something follows the consent section inside the card")
			}
		})
	}
}

// Each footer says why this reader got this mail.
func TestCoverMailFooterReasons(t *testing.T) {
	want := map[string]map[Locale]string{
		"saleconfirmation": {LocaleEN: "you bought tickets for Noche de Jazz", LocaleES: "compró entradas para Noche de Jazz"},
		"ticketassignment": {LocaleEN: "someone who bought tickets for Noche de Jazz gave us this address", LocaleES: "alguien que compró entradas para Noche de Jazz nos dio esta dirección"},
		"salereaddressing": {LocaleEN: "the organizer of Noche de Jazz asked us to re-address a purchase to this address", LocaleES: "la organización de Noche de Jazz nos pidió redirigir una compra a esta dirección"},
	}
	for _, f := range coverFixtures() {
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		if reason := want[coverKind(f)][f.locale]; !strings.Contains(textOf(parseMail(t, body)), reason) {
			t.Errorf("%s: the footer does not say %q", f.name, reason)
		}
	}
}

// With no Organization name the Organization row is dropped, Logo and all,
// and with no zone the date is; the block still opens with the Cover Image.
func TestCoverMailDropsWhatItDoesNotKnow(t *testing.T) {
	event := goldenMailEvent()
	event.OrganizationName, event.Timezone, event.VenueName = "", "", ""
	body, err := goldenTicketAssignment(LocaleEN, event).HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := parseMail(t, body)
	if n := len(findAll(doc, func(n *html.Node) bool { return hasClass(n, "tile") })); n != 0 {
		t.Fatalf("found %d Logo tiles with no Organization name", n)
	}
	if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "img" && attr(n, "src") == event.WideURL })); n != 1 {
		t.Fatalf("the wide rendition went missing with the Organization row")
	}
	for _, p := range findAll(doc, func(n *html.Node) bool { return n.Data == "p" }) {
		if strings.TrimSpace(textOf(p)) == "" {
			t.Fatalf("an empty paragraph was rendered")
		}
	}
}

// What people typed reaches the inbox as text: the Event's, the
// Organization's, the venue's, the Ticket Type's and the buyer's names.
func TestCoverMailEscapesTypedNames(t *testing.T) {
	const hostile = `<b>"Fest" & 'Co'</b>`
	event := goldenMailEvent()
	event.OrganizationName, event.VenueName = hostile, hostile
	confirmation := goldenSaleConfirmation(LocaleEN, event)
	confirmation.EventName, confirmation.CustomerName = hostile, hostile
	assignment := goldenTicketAssignment(LocaleEN, event)
	assignment.EventName, assignment.TicketTypeName = hostile, hostile
	reAddressing := goldenSaleReAddressing(LocaleES, event)
	reAddressing.EventName = hostile

	for name, render := range map[string]func(MailChrome) (string, error){
		"saleconfirmation": confirmation.HTML, "ticketassignment": assignment.HTML, "salereaddressing": reAddressing.HTML,
	} {
		t.Run(name, func(t *testing.T) {
			body, err := render(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(body, "<b>") || strings.Contains(body, `"Fest"`) {
				t.Fatalf("typed markup reached the HTML unescaped")
			}
			doc := parseMail(t, body)
			if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "b" })); n != 0 {
				t.Fatalf("found %d <b> elements; typed markup became markup", n)
			}
			if !strings.Contains(textOf(doc), hostile) {
				t.Fatalf("the typed name does not read as the text that was typed")
			}
		})
	}
}
