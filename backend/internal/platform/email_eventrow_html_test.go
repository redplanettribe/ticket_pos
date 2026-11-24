package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The compact Event row kinds' HTML parts (ADR 0082, #878): the Answer
// Reminder, the Assignment Reminder, Sale Voided, Sale Reversal Refused and No
// Longer Holding. The goldens and the structural assertions cover their shape;
// these cover what a reader is told and shown.

func eventRowFixtures() []mailFixture {
	return append(eventRowMailFixtures(LocaleEN), eventRowMailFixtures(LocaleES)...)
}

func eventRowKind(f mailFixture) string {
	name := strings.TrimSuffix(strings.TrimSuffix(f.name, "-en"), "-es")
	for _, variant := range []string{"-noimages", "-plural", "-golive"} {
		name = strings.TrimSuffix(name, variant)
	}
	return name
}

// All five kinds, in both languages, with renditions and without.
func TestEventRowMailFixturesCoverEveryKind(t *testing.T) {
	kinds := map[string]int{}
	for _, f := range eventRowFixtures() {
		if strings.Contains(f.name, "-noimages-") {
			kinds[eventRowKind(f)]++
		}
	}
	if len(kinds) != 5 {
		t.Fatalf("no-rendition fixtures cover %d kinds, want 5: %v", len(kinds), kinds)
	}
	for kind, n := range kinds {
		if n != 2 {
			t.Errorf("%s has %d no-rendition fixtures, want one per language", kind, n)
		}
	}
}

// Every line of the text part reads in the HTML part. A "Label: value" line
// the Event row stands in for ("Event: ...", "Ticket: ...", "Starts: ...")
// reads as its value.
func TestEventRowMailHTMLCarriesTheTextPart(t *testing.T) {
	for _, f := range eventRowFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			read := textOf(parseMail(t, body))
			for _, line := range strings.Split(f.text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.Contains(read, line) {
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

// The Event row shows the square rendition, the date and venue, and the
// Organization with its Logo; with no renditions, only the mark is an image
// and the Organization is its name alone.
func TestEventRowMailShowsTheEventRow(t *testing.T) {
	event := goldenMailEvent()
	for _, f := range eventRowFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, body)
			read := textOf(doc)
			for _, want := range []string{event.OrganizationName, event.VenueName, formatEventDate(event.StartsAt, event.Timezone, f.locale)} {
				if !strings.Contains(read, want) {
					t.Errorf("the Event row does not say %q", want)
				}
			}
			images := map[string]bool{}
			for _, img := range findAll(doc, func(n *html.Node) bool { return n.Data == "img" }) {
				images[attr(img, "src")] = true
				if src := attr(img, "src"); src != goldenChrome.MarkURL && attr(img, "alt") != "" {
					t.Errorf("<img src=%q> has alt %q, want empty: its name is printed beside it", src, attr(img, "alt"))
				}
			}
			if images[event.WideURL] {
				t.Errorf("the compact Event row shows the wide Cover Image")
			}
			withImages := !strings.Contains(f.name, "-noimages-")
			for _, src := range []string{event.SquareURL, event.LogoURL} {
				if images[src] != withImages {
					t.Errorf("image %q shown = %v, want %v", src, images[src], withImages)
				}
			}
			if !withImages && len(images) != 1 {
				t.Errorf("with no renditions the mail shows %d images, want only the mark", len(images))
			}
		})
	}
}

// The Logo tile is painted white twice in every rendering that shows a Logo:
// only the background-image gradient survives Gmail's forced inversion.
func TestEventRowMailLogoTileIsPaintedWhiteTwice(t *testing.T) {
	logos := 0
	for _, f := range eventRowFixtures() {
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		for _, img := range findAll(parseMail(t, body), func(n *html.Node) bool {
			return n.Data == "img" && attr(n, "src") == goldenMailEvent().LogoURL
		}) {
			logos++
			tile := img.Parent
			style := attr(tile, "style")
			if !hasClass(tile, "tile") || !strings.Contains(style, "background-color:#ffffff") ||
				!strings.Contains(style, "background-image:linear-gradient(#ffffff,#ffffff)") {
				t.Errorf("%s: the Logo's tile is not painted white twice: class=%q style=%q", f.name, attr(tile, "class"), style)
			}
		}
	}
	if logos == 0 {
		t.Fatal("no fixture shows a Logo")
	}
}

// With no Organization name the Organization row is dropped, Logo and all: a
// Logo with an empty alt never stands alone.
func TestEventRowMailDropsTheOrganizationRowWithoutAName(t *testing.T) {
	event := goldenMailEvent()
	event.OrganizationName = ""
	body, err := goldenSaleVoided(LocaleEN, event).HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := parseMail(t, body)
	if n := len(findAll(doc, func(n *html.Node) bool { return hasClass(n, "tile") })); n != 0 {
		t.Fatalf("found %d Logo tiles with no Organization name", n)
	}
	if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "img" && attr(n, "src") == event.SquareURL })); n != 1 {
		t.Fatalf("the square rendition went missing with the Organization row")
	}
}

// With neither a date nor a venue the muted line is dropped, not left empty.
func TestEventRowMailDropsAnEmptyDateAndVenueLine(t *testing.T) {
	body, err := goldenNoLongerHolding(LocaleEN, MailEvent{}).HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, p := range findAll(parseMail(t, body), func(n *html.Node) bool { return n.Data == "p" }) {
		if strings.TrimSpace(textOf(p)) == "" {
			t.Fatalf("an empty paragraph was rendered")
		}
	}
}

// The reminders have the one button their text's link is; the bad-news kinds
// have no link and no button.
func TestEventRowMailHasAButtonOnlyWhereTheTextHasALink(t *testing.T) {
	for _, f := range eventRowFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			buttons := findAll(parseMail(t, body), isPrimaryButton)
			switch kind := eventRowKind(f); kind {
			case "answerreminder", "assignmentreminder":
				if len(buttons) != 1 {
					t.Fatalf("found %d buttons, want 1", len(buttons))
				}
				if href := attr(buttons[0], "href"); !strings.Contains(f.text, "\n"+href) {
					t.Errorf("the button goes to %q, which is not the text's link", href)
				}
			default:
				if len(buttons) != 0 {
					t.Errorf("%s has %d buttons, want none", kind, len(buttons))
				}
			}
		})
	}
}

// Each footer says why this reader got this mail.
func TestEventRowMailFooterReasons(t *testing.T) {
	want := map[string]map[Locale]string{
		"answerreminder":      {LocaleEN: "a ticket you hold still needs an answer", LocaleES: "una entrada que usted tiene aún necesita una respuesta"},
		"assignmentreminder":  {LocaleEN: "you bought tickets for Noche de Jazz", LocaleES: "compró entradas para Noche de Jazz"},
		"salevoided":          {LocaleEN: "you bought tickets for Noche de Jazz", LocaleES: "compró entradas para Noche de Jazz"},
		"salereversalrefused": {LocaleEN: "you bought tickets for Noche de Jazz", LocaleES: "compró entradas para Noche de Jazz"},
		"nolongerholding":     {LocaleEN: "you held a ticket for Noche de Jazz", LocaleES: "tenía una entrada para Noche de Jazz"},
	}
	for _, f := range eventRowFixtures() {
		body, err := f.html(goldenChrome)
		if err != nil {
			t.Fatalf("%s: render: %v", f.name, err)
		}
		if reason := want[eventRowKind(f)][f.locale]; !strings.Contains(textOf(parseMail(t, body)), reason) {
			t.Errorf("%s: the footer does not say %q", f.name, reason)
		}
	}
}

// What people typed reaches the inbox as text: the Event's, the
// Organization's, the venue's, the Ticket Type's and the buyer's names.
func TestEventRowMailEscapesTypedNames(t *testing.T) {
	const hostile = `<b>"Fest" & 'Co'</b>`
	event := goldenMailEvent()
	event.OrganizationName, event.VenueName = hostile, hostile
	answer := goldenHolderAnswerReminder(LocaleEN, event)
	answer.Tickets[0].EventName, answer.Tickets[0].TicketTypeName = hostile, hostile
	assign := goldenAssignmentReminder(LocaleEN, event)
	assign.EventName, assign.FirstName = hostile, hostile
	voided := goldenSaleVoided(LocaleEN, event)
	voided.EventName, voided.CustomerName = hostile, hostile
	refused := goldenSaleReversalRefused(LocaleEN, event)
	refused.EventName, refused.CustomerName = hostile, hostile
	gone := goldenNoLongerHolding(LocaleEN, event)
	gone.EventName = hostile

	for name, render := range map[string]func(MailChrome) (string, error){
		"answerreminder": answer.HTML, "assignmentreminder": assign.HTML,
		"salevoided": voided.HTML, "salereversalrefused": refused.HTML, "nolongerholding": gone.HTML,
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

// An Event with no zone prints no date in its row rather than a UTC one.
func TestEventRowMailPrintsNoDateWithoutAZone(t *testing.T) {
	event := goldenMailEvent()
	event.Timezone = ""
	row := newEventRow(LocaleEN, "Noche de Jazz", "", event)
	if row.Meta != event.VenueName {
		t.Fatalf("meta = %q, want the venue alone", row.Meta)
	}
}
