package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Every kind is sent multipart (ADR 0082, spec #870, #881): each EmailSender
// method sends a message value with an HTML rendering beside its text, and
// that rendering is pinned by a golden file in both languages. A method added
// to EmailSender fails here until its message renders HTML and its fixtures
// and goldens are in mailFixtures.

// emailSenderMessageType is the type of the message value an EmailSender
// method sends: its one argument after the context, except the passcode,
// whose method takes the code and its language and sends them as an
// OTPMessage.
func emailSenderMessageType(t *testing.T, method reflect.Method) reflect.Type {
	t.Helper()
	if method.Name == "SendOTP" {
		return reflect.TypeOf(OTPMessage{})
	}
	if method.Type.NumIn() != 2 {
		t.Fatalf("%s takes %d arguments; say here which message value it sends", method.Name, method.Type.NumIn())
	}
	return method.Type.In(1)
}

func TestEveryEmailSenderKindHasAnHTMLRenderingAndGoldensInBothLanguages(t *testing.T) {
	sender := reflect.TypeOf((*EmailSender)(nil)).Elem()
	rendering := reflect.TypeOf((*mailMessage)(nil)).Elem()

	goldens := map[reflect.Type]map[Locale][]string{}
	for _, f := range mailFixtures() {
		kind := reflect.TypeOf(f.message)
		if goldens[kind] == nil {
			goldens[kind] = map[Locale][]string{}
		}
		goldens[kind][f.locale] = append(goldens[kind][f.locale], f.name)
	}

	if sender.NumMethod() != 22 {
		t.Errorf("EmailSender has %d kinds, the spec's twenty-two no longer; update this count and the spec", sender.NumMethod())
	}
	for i := range sender.NumMethod() {
		method := sender.Method(i)
		t.Run(method.Name, func(t *testing.T) {
			kind := emailSenderMessageType(t, method)
			if !kind.Implements(rendering) {
				t.Fatalf("%s sends %s, which has no HTML(MailChrome) rendering beside its Text()", method.Name, kind)
			}
			for _, locale := range []Locale{LocaleEN, LocaleES} {
				names := goldens[kind][locale]
				if len(names) == 0 {
					t.Errorf("%s has no golden fixture in %q; add one to mailFixtures", kind, locale)
				}
				for _, name := range names {
					if _, err := os.Stat(filepath.Join("testdata", "mail", name+".html")); err != nil {
						t.Errorf("%s: golden %s.html is missing (run with -update to create it): %v", kind, name, err)
					}
				}
			}
		})
	}
}

// No link in any golden is rewritten (spec #870, "No tracking"): every href
// is the support address or a link the message's own text part carries,
// whole, as one word of it, so nothing points through a redirect that
// records the click.
func TestNoGoldenLinkIsRewritten(t *testing.T) {
	for _, f := range mailFixtures() {
		t.Run(f.name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", "mail", f.name+".html"))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			destinations := map[string]bool{}
			for _, word := range strings.Fields(f.text) {
				destinations[word] = true
			}
			doc := parseMail(t, string(golden))
			for _, link := range findAll(doc, func(n *html.Node) bool { return n.Data == "a" }) {
				href := attr(link, "href")
				if href == "mailto:"+goldenChrome.SupportEmail {
					continue
				}
				if !destinations[href] {
					t.Errorf("link %q is not a destination the text part carries", href)
				}
			}
		})
	}
}
