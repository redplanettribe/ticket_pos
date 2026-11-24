package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The Follow Digest's HTML part (ADR 0082, #880). The goldens and the
// structural assertions cover its shape; these cover what a reader is told
// and shown, and that the unsubscribe line is this kind's alone (ADR 0034).

func digestFixtures() []mailFixture {
	return append(digestMailFixtures(LocaleEN), digestMailFixtures(LocaleES)...)
}

func isDigestFixture(f mailFixture) bool { return strings.HasPrefix(f.name, "followdigest") }

func renderDigest(t *testing.T, d FollowDigest) *html.Node {
	t.Helper()
	body, err := d.HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertMailStructure(t, body, d.Text(), d.Locale, goldenAssetOrigin, goldenMediaOrigin)
	return parseMail(t, body)
}

// linksTo is every <a> in doc pointing at url.
func linksTo(doc *html.Node, url string) []*html.Node {
	return findAll(doc, func(n *html.Node) bool { return n.Data == "a" && attr(n, "href") == url })
}

// footerOf is the footer cell below the card: the last row of the layout's
// 600px table.
func footerOf(t *testing.T, doc *html.Node) *html.Node {
	t.Helper()
	wraps := findAll(doc, func(n *html.Node) bool { return n.Data == "table" && hasClass(n, "wrap") })
	if len(wraps) != 1 {
		t.Fatalf("found %d layout tables, want 1", len(wraps))
	}
	rows := findAll(wraps[0], func(n *html.Node) bool { return n.Data == "tr" && n.Parent.Parent == wraps[0] })
	if len(rows) == 0 {
		t.Fatal("the layout table has no rows")
	}
	return rows[len(rows)-1]
}

// Every line of the text part reads in the HTML part. A "Label: address" line
// reads as its label, linked to its address; the unsubscribe line is read
// sentence by sentence in the footer (TestFollowDigestFooterCarriesTheUnsubscribeLink).
func TestFollowDigestHTMLCarriesTheTextPart(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		for _, f := range digestMailFixtures(locale) {
			t.Run(f.name, func(t *testing.T) {
				body, err := f.html(goldenChrome)
				if err != nil {
					t.Fatalf("render: %v", err)
				}
				doc := parseMail(t, body)
				read := textOf(doc)
				for _, line := range strings.Split(f.text, "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.Contains(read, line) || strings.HasPrefix(line, digestUnsubscribeQuestionCopy.in(locale)) {
						continue
					}
					if label, url, ok := strings.Cut(line, ": http"); ok && strings.Contains(read, label) && len(linksTo(doc, "http"+url)) > 0 {
						continue
					}
					t.Errorf("the HTML part does not say %q", line)
				}
			})
		}
	}
}

// Each section that has Events sits under its own heading, New first; an
// empty section prints no heading at all, as in the text.
func TestFollowDigestHTMLSections(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			headings := func(d FollowDigest) []string {
				var out []string
				for _, h := range findAll(renderDigest(t, d), func(n *html.Node) bool { return n.Data == "h2" }) {
					out = append(out, textOf(h))
				}
				return out
			}
			both := headings(goldenFollowDigest(locale))
			if want := []string{digestNewHeadingCopy.in(locale), digestHappeningHeadingCopy.in(locale)}; strings.Join(both, "|") != strings.Join(want, "|") {
				t.Fatalf("section headings = %q, want %q", both, want)
			}

			agenda := goldenFollowDigest(locale)
			agenda.New = nil
			if got := headings(agenda); len(got) != 1 || got[0] != digestHappeningHeadingCopy.in(locale) {
				t.Fatalf("an agenda-only Digest's headings = %q, want only %q", got, digestHappeningHeadingCopy.in(locale))
			}
			news := goldenFollowDigest(locale)
			news.Happening = nil
			if got := headings(news); len(got) != 1 || got[0] != digestNewHeadingCopy.in(locale) {
				t.Fatalf("a news-only Digest's headings = %q, want only %q", got, digestNewHeadingCopy.in(locale))
			}
		})
	}
}

// Every entry carries its one call to action as a link with "Because you
// follow ..." on the same line; an entry the reader holds Tickets to is
// marked and sent to their tickets, never to a purchase.
func TestFollowDigestHTMLEntries(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			d := goldenFollowDigest(locale)
			doc := renderDigest(t, d)
			read := textOf(doc)
			for _, e := range append(append([]FollowDigestEvent{}, d.New...), d.Happening...) {
				label, url := e.action(locale)
				links := linksTo(doc, url)
				if len(links) != 1 || !strings.HasPrefix(textOf(links[0]), label) {
					t.Errorf("%s: want one %q link to %s, got %d", e.Name, label, url, len(links))
					continue
				}
				if line := textOf(links[0].Parent); !strings.Contains(line, e.because(locale)) {
					t.Errorf("%s: the call to action's line %q does not say %q", e.Name, line, e.because(locale))
				}
			}

			going := findAll(doc, func(n *html.Node) bool { return hasClass(n, "pill") })
			if len(going) != 1 || !strings.Contains(textOf(going[0]), digestAttendingCopy.in(locale)) {
				t.Fatalf("want one %q mark, got %d", digestAttendingCopy.in(locale), len(going))
			}
			if strings.Count(read, digestAttendingCopy.in(locale)) != 1 {
				t.Fatalf("%q is said other than on the one entry the reader holds", digestAttendingCopy.in(locale))
			}
			attending := d.Happening[0]
			if links := linksTo(doc, attending.URL); len(links) != 0 {
				t.Fatalf("the entry the reader holds links to its purchase page")
			}
		})
	}
}

// An entry with no address to press still says why it is there, and the
// reader holding Tickets with nowhere to see them is still marked.
func TestFollowDigestHTMLEntryWithNoLink(t *testing.T) {
	d := goldenFollowDigest(LocaleEN)
	d.Happening[0].TicketSaleURL = ""
	doc := renderDigest(t, d)
	read := textOf(doc)
	for _, want := range []string{"You're going", "Because you follow: Club de Jazz, Cine"} {
		if !strings.Contains(read, want) {
			t.Errorf("the entry with no link does not say %q", want)
		}
	}
	if strings.Contains(read, digestYourTicketsCopy.in(LocaleEN)) {
		t.Errorf("the entry with no link offers %q with nowhere to go", digestYourTicketsCopy.in(LocaleEN))
	}
}

// A capped section ends with "+N more": a link to the rest when there is
// somewhere to see it, the count alone when there is not.
func TestFollowDigestHTMLOverflow(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			d := goldenFollowDigest(locale)
			d.NewOverflow = FollowDigestOverflow{Count: 4, URL: goldenAssetOrigin + "/?tags=jazz"}
			d.HappeningOverflow = FollowDigestOverflow{Count: 2}
			doc := renderDigest(t, d)
			more := linksTo(doc, d.NewOverflow.URL)
			if want := strings.Replace(digestMoreWithoutLinkCopy.in(locale), "%d", "4", 1); len(more) != 1 || !strings.HasPrefix(textOf(more[0]), want) {
				t.Fatalf("want one %q link to %s, got %d", want, d.NewOverflow.URL, len(more))
			}
			if want := strings.Replace(digestMoreWithoutLinkCopy.in(locale), "%d", "2", 1); !strings.Contains(textOf(doc), want) {
				t.Fatalf("the capped section with no link does not say %q", want)
			}

			uncapped := textOf(renderDigest(t, goldenFollowDigest(locale)))
			if strings.Contains(uncapped, "+") {
				t.Fatalf("a Digest whose sections fitted announces an overflow")
			}
		})
	}
}

// The one primary button opens every upcoming Event; with no Storefront
// origin there is none.
func TestFollowDigestHTMLButton(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			d := goldenFollowDigest(locale)
			buttons := findAll(renderDigest(t, d), isPrimaryButton)
			if len(buttons) != 1 || attr(buttons[0], "href") != d.ExploreURL || textOf(buttons[0]) != digestExploreCopy.in(locale) {
				t.Fatalf("want one %q button to %s", digestExploreCopy.in(locale), d.ExploreURL)
			}
			d.ExploreURL = ""
			if n := len(findAll(renderDigest(t, d), isPrimaryButton)); n != 0 {
				t.Fatalf("with no Storefront origin the Digest still has %d buttons", n)
			}
		})
	}
}

// An entry shows its square rendition (from its Thumbnail, or cropped from
// its Cover Image, which is the rendition's business) and its Organization's
// Logo; with neither rendition the image cell is dropped, not left empty.
func TestFollowDigestHTMLImages(t *testing.T) {
	d := goldenFollowDigest(LocaleEN)
	doc := renderDigest(t, d)
	srcs := map[string]int{}
	for _, img := range findAll(doc, func(n *html.Node) bool { return n.Data == "img" }) {
		srcs[attr(img, "src")]++
	}
	for _, e := range append(append([]FollowDigestEvent{}, d.New...), d.Happening...) {
		if e.SquareURL != "" && srcs[e.SquareURL] != 1 {
			t.Errorf("%s: its square rendition shows %d times, want once", e.Name, srcs[e.SquareURL])
		}
	}
	thumbs := findAll(doc, func(n *html.Node) bool { return n.Data == "img" && hasClass(n, "thumb-sm") })
	if len(thumbs) != 3 {
		t.Fatalf("found %d square cells, want 3: the Event with no rendition has none", len(thumbs))
	}

	none := goldenFollowDigest(LocaleEN)
	for _, section := range [][]FollowDigestEvent{none.New, none.Happening} {
		for i := range section {
			section[i].SquareURL, section[i].LogoURL = "", ""
		}
	}
	imgs := findAll(renderDigest(t, none), func(n *html.Node) bool { return n.Data == "img" })
	if len(imgs) != 1 || attr(imgs[0], "src") != goldenChrome.MarkURL {
		t.Fatalf("with no renditions the Digest shows %d images, want only the mark", len(imgs))
	}
}

// The footer says why the Digest arrived and carries the unsubscribe line:
// the question, the link that turns the Digest off, and what that keeps.
// Without a link (no signing key) the line is dropped, as in the text.
func TestFollowDigestFooterCarriesTheUnsubscribeLink(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			d := goldenFollowDigest(locale)
			doc := renderDigest(t, d)
			footer := footerOf(t, doc)
			links := linksTo(footer, d.UnsubscribeURL)
			if len(links) != 1 || textOf(links[0]) != digestUnsubscribeLinkCopy.in(locale) {
				t.Fatalf("want one %q link to %s in the footer", digestUnsubscribeLinkCopy.in(locale), d.UnsubscribeURL)
			}
			if n := len(linksTo(doc, d.UnsubscribeURL)); n != 1 {
				t.Fatalf("the unsubscribe link appears %d times, want once, in the footer", n)
			}
			line := textOf(links[0].Parent)
			for _, want := range []string{digestUnsubscribeQuestionCopy.in(locale), digestUnsubscribeKeepCopy.in(locale)} {
				if !strings.Contains(line, want) {
					t.Errorf("the unsubscribe line %q does not say %q", line, want)
				}
			}
			if !strings.Contains(textOf(footer), digestClosingCopy.in(locale)) {
				t.Errorf("the footer does not say why the Digest arrived")
			}

			d.UnsubscribeURL = ""
			if read := textOf(footerOf(t, renderDigest(t, d))); strings.Contains(read, digestUnsubscribeLinkCopy.in(locale)) {
				t.Fatalf("with no unsubscribe link the footer still offers %q", digestUnsubscribeLinkCopy.in(locale))
			}
		})
	}
}

// The Digest is the only mail a Customer can turn off (ADR 0034), so no
// other kind's footer, in either language, offers to turn anything off.
func TestOnlyTheFollowDigestCarriesAnUnsubscribeLink(t *testing.T) {
	digests := 0
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			doc := parseMail(t, body)
			var unsubscribe []*html.Node
			for _, locale := range []Locale{LocaleEN, LocaleES} {
				label := digestUnsubscribeLinkCopy.in(locale)
				unsubscribe = append(unsubscribe, findAll(doc, func(n *html.Node) bool { return n.Data == "a" && textOf(n) == label })...)
			}
			footerLinks := findAll(footerOf(t, doc), func(n *html.Node) bool { return n.Data == "a" })
			switch {
			case !isDigestFixture(f):
				if len(unsubscribe) != 0 || strings.Contains(textOf(doc), "unsubscribe") || len(footerLinks) != 1 {
					t.Fatalf("a %s footer carries an unsubscribe link", f.name)
				}
			case strings.Contains(f.name, "-nolinks-"):
				if len(unsubscribe) != 0 {
					t.Fatalf("a Digest with no unsubscribe link offers one")
				}
			default:
				digests++
				if len(unsubscribe) != 1 || len(footerLinks) != 2 {
					t.Fatalf("the Digest's footer carries %d unsubscribe links among %d, want 1 beside the support link", len(unsubscribe), len(footerLinks))
				}
			}
		})
	}
	if digests == 0 {
		t.Fatal("no Digest fixture carried an unsubscribe link")
	}
}

// What the reader, the Organizations and Tag names put in reaches the inbox
// as text.
func TestFollowDigestHTMLEscapesEnteredText(t *testing.T) {
	const hostile = `<b>"Fest" & 'Co'</b>`
	d := goldenFollowDigest(LocaleEN)
	d.CustomerName = hostile
	d.New[0].Name, d.New[0].OrganizationName, d.New[0].Venue = hostile, hostile, hostile
	d.Happening[1].MatchedTagNames = []string{hostile}
	body, err := d.HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, "<b>") {
		t.Fatal("entered markup reached the HTML unescaped")
	}
	doc := parseMail(t, body)
	if n := strings.Count(textOf(doc), hostile); n < 5 {
		t.Fatalf("the entered text reads %d times, want it in the greeting, the name, the venue, the Organization and the Tag", n)
	}
	assertMailStructure(t, body, d.Text(), d.Locale, goldenAssetOrigin, goldenMediaOrigin)
}

// The preheader names the first Event listed, and admits to more.
func TestFollowDigestPreheader(t *testing.T) {
	d := goldenFollowDigest(LocaleEN)
	if got, want := d.preheader(), "Noche de Jazz and more from the things you follow."; got != want {
		t.Fatalf("preheader = %q, want %q", got, want)
	}
	d.New, d.Happening = d.New[:1], nil
	if got, want := d.preheader(), "Noche de Jazz, from the things you follow."; got != want {
		t.Fatalf("preheader = %q, want %q", got, want)
	}
	d.NewOverflow.Count = 3
	if got, want := d.preheader(), "Noche de Jazz and more from the things you follow."; got != want {
		t.Fatalf("a capped one-entry preheader = %q, want %q", got, want)
	}
}

// The text part's unsubscribe line is the three sentences with no em dash,
// its address last, and the explorer has its own line.
func TestFollowDigestTextLinks(t *testing.T) {
	for _, tc := range []struct {
		locale            Locale
		unsubscribe, more string
	}{
		{LocaleEN,
			"Don't want these? You'll keep everything you follow. Turn off the digest: https://tickets.example.com/unsubscribe?token=abc",
			"See all upcoming events: https://tickets.example.com/"},
		{LocaleES,
			"¿No quiere recibirlos? Conservará todo lo que sigue. Desactive el resumen: https://tickets.example.com/unsubscribe?token=abc",
			"Ver todos los próximos eventos: https://tickets.example.com/"},
	} {
		text := goldenFollowDigest(tc.locale).Text()
		if !strings.HasSuffix(text, "\n"+tc.unsubscribe) {
			t.Errorf("%s: the text does not end with %q:\n%s", tc.locale, tc.unsubscribe, text)
		}
		if !strings.Contains(text, "\n\n"+tc.more+"\n\n") {
			t.Errorf("%s: the text has no line %q:\n%s", tc.locale, tc.more, text)
		}
	}
	noLinks := goldenFollowDigest(LocaleEN)
	noLinks.ExploreURL = ""
	if strings.Contains(noLinks.Text(), "See all upcoming events") {
		t.Error("with no Storefront origin the text still offers the explorer")
	}
}
