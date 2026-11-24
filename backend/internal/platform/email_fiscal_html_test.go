package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The fiscal kinds' HTML parts (ADR 0082, #874): the Tax Document Delivery and
// the Annulment Notice in the platform chrome, with no Event imagery. The
// goldens and the structural assertions cover their shape; these cover what a
// reader is told.

// fiscalFixtures is every fiscal golden fixture, by name.
func fiscalFixtures() []mailFixture {
	var fiscal []mailFixture
	for _, f := range mailFixtures() {
		if strings.HasPrefix(f.name, "taxdocument-") || strings.HasPrefix(f.name, "annulmentnotice-") {
			fiscal = append(fiscal, f)
		}
	}
	return fiscal
}

// Every line of the text part reads in the HTML part too, the link included,
// so neither part says something the other does not.
func TestFiscalMailHTMLCarriesTheTextPartWordForWord(t *testing.T) {
	fixtures := fiscalFixtures()
	if len(fixtures) == 0 {
		t.Fatal("no fiscal fixtures")
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			read := textOf(parseMail(t, body))
			for _, line := range strings.Split(f.text, "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.Contains(read, line) {
					t.Errorf("the HTML part does not say %q", line)
				}
			}
		})
	}
}

// Fiscal mail is official mail: no Event imagery, so the only image in it is
// the platform's own mark above the card.
func TestFiscalMailCarriesNoEventImagery(t *testing.T) {
	for _, f := range fiscalFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, img := range findAll(parseMail(t, body), func(n *html.Node) bool { return n.Data == "img" }) {
				if src := attr(img, "src"); src != goldenChrome.MarkURL {
					t.Errorf("found an image other than the mark: %q", src)
				}
			}
		})
	}
}

// The Customer Area link is the one button, labelled in the mail's language;
// with no storefront configured there is no link, and the HTML part drops
// the button and the sentence that leads to it, as the text drops the link.
func TestFiscalMailButtonFollowsTheCustomerAreaLink(t *testing.T) {
	cases := []struct {
		name  string
		html  func(chrome MailChrome, url string) (string, error)
		lead  mailCopy
		label mailCopy
	}{
		{"tax document delivery", func(c MailChrome, url string) (string, error) {
			d := goldenTaxDocumentDelivery(LocaleES)
			d.CustomerAreaURL = url
			return d.HTML(c)
		}, taxDocumentLinkLeadCopy, taxDocumentButtonCopy},
		{"annulment notice", func(c MailChrome, url string) (string, error) {
			n := goldenAnnulmentNotice(LocaleES)
			n.CustomerAreaURL = url
			return n.HTML(c)
		}, annulmentNoticeLinkLeadCopy, annulmentNoticeButtonCopy},
	}
	const link = "https://tickets.example.com/tickets#sale-1"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			with, err := tc.html(goldenChrome, link)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, with)
			buttons := findAll(doc, isPrimaryButton)
			if len(buttons) != 1 || attr(buttons[0], "href") != link || textOf(buttons[0]) != tc.label.in(LocaleES) {
				t.Fatalf("want one button %q to %s", tc.label.in(LocaleES), link)
			}
			if !strings.Contains(textOf(doc), tc.lead.in(LocaleES)) {
				t.Fatalf("the sentence leading to the button is missing")
			}

			without, err := tc.html(goldenChrome, "")
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc = parseMail(t, without)
			if n := len(findAll(doc, isPrimaryButton)); n != 0 {
				t.Fatalf("with no link the mail still has %d buttons", n)
			}
			if strings.Contains(textOf(doc), tc.lead.in(LocaleES)) || strings.Contains(textOf(doc), mailLinkFallbackCopy.in(LocaleES)) {
				t.Fatalf("with no link the mail still leads to one")
			}
		})
	}
}

// The buyer's name and the Event's are typed by people, and reach the inbox
// as the text that was typed.
func TestFiscalMailHTMLEscapesTheNamesPeopleTyped(t *testing.T) {
	const hostile = `<b>"Fest"</b> & <img src=x onerror=alert(1)>`
	d := goldenTaxDocumentDelivery(LocaleEN)
	d.CustomerName, d.EventName = hostile, hostile
	n := goldenAnnulmentNotice(LocaleES)
	n.CustomerName, n.EventName = hostile, hostile
	for name, render := range map[string]func(MailChrome) (string, error){
		"tax document delivery": d.HTML,
		"annulment notice":      n.HTML,
	} {
		t.Run(name, func(t *testing.T) {
			body, err := render(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(body, "<b>") || strings.Contains(body, "<img src=x") {
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
