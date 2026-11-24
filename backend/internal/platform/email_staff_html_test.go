package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The staff and operator kinds' HTML parts (ADR 0082, #875): the Certificate
// Expiry Warning, the five Payout Request notices, both Question Review
// notices and Ticket Question Revoked, in the platform chrome with no Event
// imagery. The goldens and the structural assertions cover their shape; these
// cover what a reader is told.

// staffFixtures is every staff and operator golden fixture.
func staffFixtures() []mailFixture {
	return append(staffMailFixtures(LocaleEN), staffMailFixtures(LocaleES)...)
}

// All nine kinds, in both languages.
func TestStaffMailFixturesCoverEveryKind(t *testing.T) {
	kinds := map[string]bool{}
	for _, f := range staffFixtures() {
		name := strings.TrimSuffix(strings.TrimSuffix(f.name, "-en"), "-es")
		for _, variant := range []string{"-expiring", "-expired", "-shortfall"} {
			name = strings.TrimSuffix(name, variant)
		}
		kinds[name] = true
	}
	if len(kinds) != 9 {
		t.Fatalf("staff fixtures cover %d kinds, want 9: %v", len(kinds), kinds)
	}
}

// Every line of the text part reads in the HTML part too, so neither part
// says something the other does not, in the reader's Staff Locale.
func TestStaffMailHTMLCarriesTheTextPartWordForWord(t *testing.T) {
	for _, f := range staffFixtures() {
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

// Staff and operator mail is the platform's own: the only image in it is the
// mark above the card.
func TestStaffMailCarriesNoEventImagery(t *testing.T) {
	for _, f := range staffFixtures() {
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

// A button only where the text carries a link: the Certificate Expiry
// Warning's Issuer page is the one, and every other staff kind has none.
func TestStaffMailHasAButtonOnlyWhereTheTextHasALink(t *testing.T) {
	for _, f := range staffFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			buttons := findAll(parseMail(t, body), isPrimaryButton)
			if !strings.HasPrefix(f.name, "certificateexpiry-") {
				if len(buttons) != 0 {
					t.Fatalf("a kind with no link has %d buttons", len(buttons))
				}
				return
			}
			want := goldenCertificateExpiryWarning(f.locale).IssuerURL
			if len(buttons) != 1 || attr(buttons[0], "href") != want || textOf(buttons[0]) != certificateExpiryButtonCopy.in(f.locale) {
				t.Fatalf("want one button %q to %s", certificateExpiryButtonCopy.in(f.locale), want)
			}
		})
	}
}

// The footer says why this reader got it: a Platform Operator, the one who
// asked to be paid, the one who submitted the questions, an Org Admin.
func TestStaffMailFooterSaysWhyTheReaderGotIt(t *testing.T) {
	for _, f := range staffFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			var want string
			switch {
			case strings.HasPrefix(f.name, "certificateexpiry-"), strings.HasPrefix(f.name, "payoutrequest-submitted-"), strings.HasPrefix(f.name, "questionreview-submitted-"):
				want = operatorFooterReasonCopy.in(f.locale)
			case strings.HasPrefix(f.name, "payoutrequest-"):
				want = strings.Replace(payoutAskerFooterReasonCopy.in(f.locale), "%s", "Club de Jazz", 1)
			case strings.HasPrefix(f.name, "questionreview-answered-"):
				want = strings.Replace(questionReviewAnsweredFooterReasonCopy.in(f.locale), "%s", "Noche de Jazz", 1)
			case strings.HasPrefix(f.name, "ticketquestionrevoked-"):
				want = strings.Replace(questionRevokedFooterReasonCopy.in(f.locale), "%s", "Club de Jazz", 1)
			default:
				t.Fatalf("no footer reason expected for %s", f.name)
			}
			if !strings.Contains(textOf(parseMail(t, body)), want) {
				t.Fatalf("the footer does not say %q", want)
			}
		})
	}
}

// What staff and operators typed (an Organization's name, an Event's, a
// note, a question's label, an operator's reason) reaches the inbox as the
// text that was typed.
func TestStaffMailHTMLEscapesWhatPeopleTyped(t *testing.T) {
	const hostile = `<b>"Fest"</b> & <img src=x onerror=alert(1)>`
	submitted := goldenPayoutRequestSubmitted(LocaleEN)
	submitted.OrganizationName, submitted.Note = hostile, hostile
	declined := goldenPayoutRequestDeclined(LocaleES)
	declined.OrganizationName, declined.Reason = hostile, hostile
	failed := goldenPayoutRequestTransferFailed(LocaleEN)
	failed.OrganizationName, failed.Reason = hostile, hostile
	review := goldenQuestionReviewSubmitted(LocaleES)
	review.OrganizationName, review.EventName, review.Note = hostile, hostile, hostile
	answered := goldenQuestionReviewAnswered(LocaleEN)
	answered.EventName = hostile
	answered.Items[2].QuestionLabel, answered.Items[2].Reason = hostile, hostile
	revoked := goldenTicketQuestionRevoked(LocaleES)
	revoked.OrganizationName, revoked.EventName, revoked.QuestionLabel, revoked.Reason = hostile, hostile, hostile, hostile

	for name, render := range map[string]func(MailChrome) (string, error){
		"payout request submitted":       submitted.HTML,
		"payout request declined":        declined.HTML,
		"payout request transfer failed": failed.HTML,
		"question review submitted":      review.HTML,
		"question review answered":       answered.HTML,
		"ticket question revoked":        revoked.HTML,
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
				t.Fatalf("the typed text does not read as the text that was typed")
			}
		})
	}
}

// A failed transfer is not a refusal in the HTML part either: its preheader
// and footer are new words, and `failed` and `declined` must never share a
// sentence (ADR 0026 amendment).
func TestPayoutTransferFailedHTMLNeverReadsAsARefusal(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		failed := goldenPayoutRequestTransferFailed(locale)
		body, err := failed.HTML(goldenChrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		read := strings.ToLower(textOf(parseMail(t, body)))
		for _, forbidden := range []string{"declined", "refused", "rejected your", "rechaz"} {
			if strings.Contains(read, forbidden) {
				t.Errorf("%s: the transfer-failed HTML reads as a refusal (%q)", locale, forbidden)
			}
		}
	}
}
