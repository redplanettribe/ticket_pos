package platform

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/net/html"
)

// The HTML mail harness (ADR 0082, spec #870), reused by every kind that gains
// an HTML part: one golden file per kind, fixture and locale under
// testdata/mail, and the structural assertions run over every one of them.
//
//	go test ./internal/platform -run TestMailHTMLGoldens -update
//
// rewrites the goldens; review the diff before committing it.

var updateMailGoldens = flag.Bool("update", false, "rewrite the golden HTML mail files")

// Invented throughout: the platform's own asset origin, a media bucket, and an
// Issuer nobody registered.
const (
	goldenAssetOrigin = "https://tickets.example.com"
	goldenMediaOrigin = "https://media.example.com"
)

var goldenChrome = MailChrome{
	MarkURL:         goldenAssetOrigin + "/icon-192.png",
	OperatorName:    "FUNDACION EJEMPLO",
	OperatorCountry: "Ecuador",
	SupportEmail:    DefaultSupportEmail,
}

// mailFixture is one rendered mail: a golden file name, the locale it was
// written in, and both parts.
type mailFixture struct {
	name   string
	locale Locale
	html   func(MailChrome) (string, error)
	text   string
}

// mailFixtures is every kind with an HTML part, in both languages. A kind that
// gains one belongs here, and its goldens with it.
func mailFixtures() []mailFixture {
	var fixtures []mailFixture
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		otp := OTPMessage{Code: "482913", Locale: locale}
		fixtures = append(fixtures, mailFixture{"otp-" + string(locale), locale, otp.HTML, otp.Text()})

		for _, variant := range []struct {
			name                  string
			marketing, networking bool
		}{
			{"marketing", true, false},
			{"networking", false, true},
			{"both", true, true},
		} {
			c := ConsentWithdrawalConfirmation{To: "ana@example.com", Locale: locale, MarketingConsent: variant.marketing, NetworkingConsent: variant.networking}
			fixtures = append(fixtures, mailFixture{"consentwithdrawal-" + variant.name + "-" + string(locale), locale, c.HTML, c.Text()})
		}
	}
	return fixtures
}

func TestMailHTMLGoldens(t *testing.T) {
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			path := filepath.Join("testdata", "mail", f.name+".html")
			if *updateMailGoldens {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			if got != string(want) {
				t.Fatalf("%s differs from its golden; rerun with -update and review the diff\n%s", f.name, firstDifference(string(want), got))
			}
		})
	}
}

// Every golden fixture, read the way a client does.
func TestMailHTMLStructure(t *testing.T) {
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertMailStructure(t, body, f.text, f.locale, goldenAssetOrigin, goldenMediaOrigin)
		})
	}
}

// The button component, which no kind in this first set uses, is held to the
// same structure: one primary button, its raw link printed below it.
func TestMailButtonCarriesItsRawLinkFallback(t *testing.T) {
	const link = "https://tickets.example.com/c/abc?token=x&y=1"
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		t.Run(string(locale), func(t *testing.T) {
			templates, err := parseMailTemplates(withKind(t, "buttontest", `{{define "card"}}{{template "cardOpen"}}{{template "button" .}}{{template "cardClose"}}{{end}}`), withCard(mailKinds, "buttontest", mailButton{}))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			saved := mailTemplates
			mailTemplates = templates
			t.Cleanup(func() { mailTemplates = saved })

			body, err := renderMail("buttontest", locale, goldenChrome, "Subject", "Preheader", "Reason", newMailButton(locale, "View your tickets", link))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			assertMailStructure(t, body, "", locale, goldenAssetOrigin, goldenMediaOrigin)
			doc := parseMail(t, body)
			if n := len(findAll(doc, isPrimaryButton)); n != 1 {
				t.Fatalf("found %d primary buttons, want 1", n)
			}
			if !strings.Contains(textOf(doc), mailLinkFallbackCopy.in(locale)) {
				t.Fatalf("the fallback sentence %q is missing", mailLinkFallbackCopy.in(locale))
			}
		})
	}
}

// The footer's legal line names the Issuer as registered, in the mail's
// language, and is absent when no Issuer is configured.
func TestMailFooterLegalLine(t *testing.T) {
	cases := []struct {
		locale Locale
		want   string
	}{
		{LocaleEN, "Multiticketing is operated by FUNDACION EJEMPLO, Ecuador."},
		{LocaleES, "Multiticketing es operado por FUNDACION EJEMPLO, Ecuador."},
	}
	for _, tc := range cases {
		t.Run(string(tc.locale), func(t *testing.T) {
			withIssuer, err := OTPMessage{Code: "1", Locale: tc.locale}.HTML(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(textOf(parseMail(t, withIssuer)), tc.want) {
				t.Fatalf("legal line %q missing from the footer", tc.want)
			}

			noIssuer := goldenChrome
			noIssuer.OperatorName, noIssuer.OperatorCountry = "", ""
			without, err := ConsentWithdrawalConfirmation{Locale: tc.locale, MarketingConsent: true}.HTML(noIssuer)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			text := textOf(parseMail(t, without))
			for _, absent := range []string{"operated by", "operado por", "FUNDACION"} {
				if strings.Contains(text, absent) {
					t.Fatalf("with no Issuer the footer still says %q", absent)
				}
			}
			if !strings.Contains(text, DefaultSupportEmail) {
				t.Fatalf("with no Issuer the support contact went missing too")
			}
		})
	}
}

// Half an Issuer is no Issuer: a name without a country, or the reverse,
// omits the legal line rather than printing half of it.
func TestMailFooterLegalLineNeedsBothNameAndCountry(t *testing.T) {
	for _, chrome := range []MailChrome{
		{OperatorName: "FUNDACION EJEMPLO"},
		{OperatorCountry: "Ecuador"},
	} {
		body, err := OTPMessage{Code: "1", Locale: LocaleEN}.HTML(chrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(textOf(parseMail(t, body)), "operated by") {
			t.Fatalf("chrome %+v printed a legal line", chrome)
		}
	}
}

// The support contact defaults when the chrome names none, as a mailto link.
func TestMailFooterSupportLink(t *testing.T) {
	body, err := OTPMessage{Code: "1", Locale: LocaleEN}.HTML(MailChrome{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	links := findAll(parseMail(t, body), func(n *html.Node) bool {
		return n.Data == "a" && attr(n, "href") == "mailto:"+DefaultSupportEmail
	})
	if len(links) != 1 || textOf(links[0]) != DefaultSupportEmail {
		t.Fatalf("want one mailto:%s link reading the address, got %d", DefaultSupportEmail, len(links))
	}
}

// What people and operators typed reaches the inbox as text. Of the fields
// these kinds interpolate, the Issuer's razón social is the one somebody
// entered by hand.
func TestMailHTMLEscapesEnteredText(t *testing.T) {
	const hostile = `<b>"ACME" & 'Co'</b><img src=x onerror=alert(1)>`
	chrome := goldenChrome
	chrome.OperatorName = hostile
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(chrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(body, "<b>") || strings.Contains(body, "<img src=x") || strings.Contains(body, `"ACME"`) {
				t.Fatalf("entered markup reached the HTML unescaped")
			}
			doc := parseMail(t, body)
			if !strings.Contains(textOf(doc), hostile) {
				t.Fatalf("the entered name does not read as the text that was entered")
			}
			if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "b" })); n != 0 {
				t.Fatalf("found %d <b> elements; entered markup became markup", n)
			}
			assertMailStructure(t, body, f.text, f.locale, goldenAssetOrigin, goldenMediaOrigin)
		})
	}
}

// A template that does not parse, or that names a field its card lacks, is
// refused when the templates are parsed, which happens at package init: the
// process fails at boot, never at a send.
func TestBrokenMailTemplateFailsAtBoot(t *testing.T) {
	cases := map[string]string{
		"does not parse":         `{{define "card"}}<tr><td>{{.Heading</td></tr>{{end}}`,
		"names a missing field":  `{{define "card"}}<tr><td>{{.NoSuchField}}</td></tr>{{end}}`,
		"breaks the escaping":    `{{define "card"}}<tr><td><a href="{{.Heading}}></td></tr>{{end}}`,
		"defines no card at all": `{{define "notacard"}}{{end}}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := withKind(t, "otp", source)
			if _, err := parseMailTemplates(fsys, mailKinds); err == nil {
				t.Fatal("parseMailTemplates accepted a broken template")
			}
			defer func() {
				if recover() == nil {
					t.Fatal("mustParseMailTemplates did not panic on a broken template")
				}
			}()
			mustParseMailTemplates(fsys, mailKinds)
		})
	}

	t.Run("a declared kind with no file", func(t *testing.T) {
		kinds := withCard(mailKinds, "missing", otpCard{})
		if _, err := parseMailTemplates(mailTemplateFiles, kinds); err == nil {
			t.Fatal("parseMailTemplates accepted a kind with no template file")
		}
	})
	t.Run("the shipped templates parse", func(t *testing.T) {
		if _, err := parseMailTemplates(mailTemplateFiles, mailKinds); err != nil {
			t.Fatalf("parse: %v", err)
		}
	})
}

// withCard is kinds with one more declared.
func withCard(kinds map[mailKind]any, kind mailKind, card any) map[mailKind]any {
	all := map[mailKind]any{kind: card}
	for name, c := range kinds {
		all[name] = c
	}
	return all
}

// withKind is the shipped templates with one kind's file replaced (or added).
func withKind(t *testing.T, kind mailKind, source string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, name := range []string{"mailtemplates/layout.html", "mailtemplates/components.html", "mailtemplates/kinds/otp.html", "mailtemplates/kinds/consentwithdrawal.html"} {
		data, err := mailTemplateFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		fsys[name] = &fstest.MapFile{Data: data}
	}
	fsys["mailtemplates/kinds/"+string(kind)+".html"] = &fstest.MapFile{Data: []byte(source)}
	return fsys
}

// assertMailStructure holds one rendered body (and its text part, when the
// fixture has one) to the spec's structural rules.
func assertMailStructure(t *testing.T, body, text string, locale Locale, assetOrigin, mediaOrigin string) {
	t.Helper()
	doc := parseMail(t, body)

	htmlElements := findAll(doc, func(n *html.Node) bool { return n.Data == "html" })
	if len(htmlElements) != 1 || attr(htmlElements[0], "lang") != string(locale) {
		t.Errorf("<html lang> is not %q", locale)
	}

	for _, table := range findAll(doc, func(n *html.Node) bool { return n.Data == "table" }) {
		if attr(table, "role") != "presentation" {
			t.Errorf("a layout table has no role=\"presentation\"")
		}
	}

	for _, img := range findAll(doc, func(n *html.Node) bool { return n.Data == "img" }) {
		if !hasAttr(img, "alt") {
			t.Errorf("<img src=%q> has no alt", attr(img, "src"))
		}
		src := attr(img, "src")
		if !strings.HasPrefix(src, assetOrigin+"/") && !strings.HasPrefix(src, mediaOrigin+"/") {
			t.Errorf("<img src=%q> points outside the media bucket and the platform's assets", src)
		}
	}

	buttons := findAll(doc, isPrimaryButton)
	if len(buttons) > 1 {
		t.Errorf("found %d primary buttons, want at most one", len(buttons))
	}
	for _, button := range buttons {
		href := attr(button, "href")
		fallback := findAll(doc, func(n *html.Node) bool {
			return n.Data == "a" && !isPrimaryButton(n) && attr(n, "href") == href && textOf(n) == href
		})
		if len(fallback) == 0 {
			t.Errorf("the button to %q has no raw-link fallback", href)
			continue
		}
		if !strings.Contains(textOf(fallback[0].Parent), mailLinkFallbackCopy.in(locale)) {
			t.Errorf("the raw link to %q is not introduced by %q", href, mailLinkFallbackCopy.in(locale))
		}
	}

	for name, part := range map[string]string{"html": body, "text": text} {
		if strings.ContainsRune(part, '\u2014') {
			t.Errorf("the %s part contains an em dash", name)
		}
	}

	preheaders := findAll(doc, func(n *html.Node) bool { return n.Data == "div" && hasClass(n, "preheader") })
	if len(preheaders) != 1 {
		t.Fatalf("found %d preheaders, want 1", len(preheaders))
	}
	preheader := preheaders[0]
	if !strings.Contains(attr(preheader, "style"), "display:none") {
		t.Errorf("the preheader is not hidden")
	}
	if strings.TrimSpace(strings.Trim(textOf(preheader), "\u034f\u200c\u00a0 ")) == "" {
		t.Errorf("the preheader is empty")
	}
	if !strings.Contains(textOf(preheader), strings.Repeat("\u034f\u200c\u00a0", 60)) {
		t.Errorf("the preheader is not padded")
	}
	if first := firstElementChild(findAll(doc, func(n *html.Node) bool { return n.Data == "body" })[0]); first != preheader {
		t.Errorf("the preheader is not the first thing in the body")
	}
}

func isPrimaryButton(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "button")
}

func parseMail(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

func firstElementChild(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// textOf is the text a reader sees in n, with whitespace runs collapsed.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' }), " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

// firstDifference shows where a render left its golden, which a full dump of
// two pages of markup would bury.
func firstDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	from := max(0, i-80)
	return "golden: ..." + want[from:min(len(want), i+80)] + "...\nrender: ..." + got[from:min(len(got), i+80)] + "..."
}
