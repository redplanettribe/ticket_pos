package platform

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The staff and operator kinds' HTML parts (ADR 0082, #875): the Certificate
// Expiry Warning, the five Payout Request notices, both Question Review
// notices, Ticket Question Revoked and the Event Recap, in the platform chrome
// with no Event imagery. The goldens and the structural assertions cover their shape; these
// cover what a reader is told.

// staffFixtures is every staff and operator golden fixture.
func staffFixtures() []mailFixture {
	return append(staffMailFixtures(LocaleEN), staffMailFixtures(LocaleES)...)
}

// All ten kinds, in both languages.
func TestStaffMailFixturesCoverEveryKind(t *testing.T) {
	kinds := map[string]bool{}
	for _, f := range staffFixtures() {
		name := strings.TrimSuffix(strings.TrimSuffix(f.name, "-en"), "-es")
		for _, variant := range []string{"-expiring", "-expired", "-shortfall"} {
			name = strings.TrimSuffix(name, variant)
		}
		kinds[name] = true
	}
	if len(kinds) != 10 {
		t.Fatalf("staff fixtures cover %d kinds, want 10: %v", len(kinds), kinds)
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
				// Word for word, whatever the spacing: the text part pads a
				// table's columns with spaces, which are not words.
				if line = strings.Join(strings.Fields(line), " "); line != "" && !strings.Contains(read, line) {
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
// Warning's Issuer page and the Event Recap's Sales Trends are the two, and
// every other staff kind has none.
func TestStaffMailHasAButtonOnlyWhereTheTextHasALink(t *testing.T) {
	for _, f := range staffFixtures() {
		t.Run(f.name, func(t *testing.T) {
			body, err := f.html(goldenChrome)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			buttons := findAll(parseMail(t, body), isPrimaryButton)
			var want, label string
			switch {
			case strings.HasPrefix(f.name, "certificateexpiry-"):
				want, label = goldenCertificateExpiryWarning(f.locale).IssuerURL, certificateExpiryButtonCopy.in(f.locale)
			case strings.HasPrefix(f.name, "eventrecap-"):
				want, label = goldenEventRecap(f.locale).SalesTrendsURL, eventRecapSalesTrendsButtonCopy.in(f.locale)
			default:
				if len(buttons) != 0 {
					t.Fatalf("a kind with no link has %d buttons", len(buttons))
				}
				return
			}
			if len(buttons) != 1 || attr(buttons[0], "href") != want || textOf(buttons[0]) != label {
				t.Fatalf("want one button %q to %s", label, want)
			}
		})
	}
}

// The Event Recap links to the Event's Sales Trends, Reach and Sales list, in
// both parts, in both languages.
func TestEventRecapLinksToSalesTrendsReachAndTheSalesList(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		recap := goldenEventRecap(locale)
		body, err := recap.HTML(goldenChrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		hrefs := map[string]bool{}
		for _, link := range findAll(parseMail(t, body), func(n *html.Node) bool { return n.Data == "a" }) {
			hrefs[attr(link, "href")] = true
		}
		for _, url := range []string{recap.SalesTrendsURL, recap.ReachURL, recap.SalesURL} {
			if !hrefs[url] {
				t.Errorf("%s: the HTML part does not link to %s", locale, url)
			}
			if !strings.Contains(recap.Text(), url) {
				t.Errorf("%s: the text part does not carry %s", locale, url)
			}
		}
	}
}

// The Event Recap names the Event in its subject and says when its figures
// are as of, on the platform clock.
func TestEventRecapNamesTheEventAndSaysWhenItsFiguresAreAsOf(t *testing.T) {
	for locale, asOf := range map[Locale]string{
		LocaleEN: "6 December 2026 at 09:00 (Ecuador time)",
		LocaleES: "6 de diciembre de 2026 a las 09:00 (hora de Ecuador)",
	} {
		recap := goldenEventRecap(locale)
		if !strings.Contains(recap.Subject(), recap.EventName) {
			t.Errorf("%s: subject %q does not name the Event", locale, recap.Subject())
		}
		if !strings.Contains(recap.Text(), asOf) {
			t.Errorf("%s: text %q does not say its figures are as of %q", locale, recap.Text(), asOf)
		}
	}
}

// The footer says why this reader got it: a Platform Operator, the one who
// asked to be paid, the one who submitted the questions, an Org Admin, the
// one who answers for the Event.
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
			case strings.HasPrefix(f.name, "eventrecap-"):
				want = strings.Replace(eventRecapFooterReasonCopy.in(f.locale), "%s", "Noche de Jazz", 1)
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
	recap := goldenEventRecap(LocaleEN)
	recap.EventName = hostile

	for name, render := range map[string]func(MailChrome) (string, error){
		"payout request submitted":       submitted.HTML,
		"payout request declined":        declined.HTML,
		"payout request transfer failed": failed.HTML,
		"question review submitted":      review.HTML,
		"question review answered":       answered.HTML,
		"ticket question revoked":        revoked.HTML,
		"event recap":                    recap.HTML,
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

// The Event Recap states the Event's sales figures in both parts: its totals,
// Tickets Sold and Ticket Sales by Sales Channel, Tickets Sold by Ticket Type,
// and the count of reversed Sales, with money and counts in the marks of the
// reader's Staff Locale (#886).
func TestEventRecapStatesTheSalesFiguresInTheReadersStaffLocale(t *testing.T) {
	for locale, want := range map[Locale][]string{
		LocaleEN: {
			"Tickets sold: 1,284",
			"Ticket sales: 713",
			"Takings: $23,450.50",
			"Net Proceeds of Online Sales: $16,500.00",
			"Reversed sales: 2 (left out of every figure here)",
			"Sales Channel Tickets sold Ticket sales",
			"Online 1,100 640",
			"In person 150 60",
			"Import 34 13",
			"Ticket Type Tickets sold",
			"General 1,004",
			"Palco 280",
			"Cortesía 0",
		},
		LocaleES: {
			"Entradas vendidas: 1.284",
			"Ventas de entradas: 713",
			"Recaudación: $23.450,50",
			"Ingresos netos de las ventas en línea: $16.500,00",
			"Ventas revertidas: 2 (fuera de todas las cifras de este correo)",
			"Canal de venta Entradas vendidas Ventas de entradas",
			"En línea 1.100 640",
			"Presencial 150 60",
			"Importación 34 13",
			"Tipo de entrada Entradas vendidas",
			"General 1.004",
			"Palco 280",
			"Cortesía 0",
		},
	} {
		recap := goldenEventRecap(locale)
		body, err := recap.HTML(goldenChrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		parts := map[string]string{
			"HTML": textOf(parseMail(t, body)),
			"text": strings.Join(strings.Fields(recap.Text()), " "),
		}
		for part, read := range parts {
			for _, figure := range want {
				if !strings.Contains(read, figure) {
					t.Errorf("%s: the %s part does not say %q:\n%s", locale, part, figure, read)
				}
			}
			if strings.Contains(strings.ToLower(read), "asistentes") {
				t.Errorf("%s: the %s part says asistentes; a ticket sold is not a person who came", locale, part)
			}
		}
	}
}

// The text part's tables are padded into columns, the figures set to the
// right, so they read as tables where the client keeps the spacing.
func TestEventRecapTextPartAlignsItsTables(t *testing.T) {
	text := goldenEventRecap(LocaleEN).Text()
	for _, table := range []string{
		"Sales Channel  Tickets sold  Ticket sales\n" +
			"Online                1,100           640\n" +
			"In person               150            60\n" +
			"Import                   34            13",
		"Ticket Type  Tickets sold\n" +
			"General             1,004\n" +
			"Palco                 280\n" +
			"Cortesía                0",
	} {
		if !strings.Contains(text, table) {
			t.Errorf("the text part does not carry this table:\n%s\n\nit reads:\n%s", table, text)
		}
	}
}

// An Event that sold nothing states its totals as zeros and has no table to
// split them in; a recap with no sales figures at all states none.
func TestEventRecapOfAnEventThatSoldNothingStatesItsTotalsAndNoTable(t *testing.T) {
	recap := goldenEventRecap(LocaleEN)
	recap.Sales = &EventRecapSales{
		Currency:    "USD",
		Channels:    []EventRecapChannel{{Channel: "online"}, {Channel: "in_person"}, {Channel: "import"}},
		TicketTypes: []EventRecapTicketType{{Name: "General"}},
	}
	body, err := recap.HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := parseMail(t, body)
	for _, part := range []string{textOf(doc), recap.Text()} {
		for _, figure := range []string{"Tickets sold: 0", "Ticket sales: 0", "Takings: $0.00", "Net Proceeds of Online Sales: $0.00", "Reversed sales: 0"} {
			if !strings.Contains(part, figure) {
				t.Errorf("the mail does not say %q:\n%s", figure, part)
			}
		}
		if strings.Contains(part, "Sales Channel") || strings.Contains(part, "Ticket Type") || strings.Contains(part, "left out") {
			t.Errorf("an Event that sold nothing has a table or a reversal note:\n%s", part)
		}
	}
	if n := len(findAll(doc, func(n *html.Node) bool { return n.Data == "th" })); n != 0 {
		t.Errorf("found %d column headings, want no table", n)
	}

	recap.Sales = nil
	if text := recap.Text(); strings.Contains(text, "Tickets sold") || strings.Contains(text, "Takings") {
		t.Errorf("a recap with no sales figures states some:\n%s", text)
	}
}

// Money is written as the staff app writes it for the same Staff Locale, in
// the Organization's currency whoever reads.
func TestFormatStaffMoney(t *testing.T) {
	for _, c := range []struct {
		cents    int
		currency string
		locale   Locale
		want     string
	}{
		{0, "USD", LocaleEN, "$0.00"},
		{0, "USD", LocaleES, "$0,00"},
		{2505, "USD", LocaleEN, "$25.05"},
		{2505, "USD", LocaleES, "$25,05"},
		{123450, "USD", LocaleEN, "$1,234.50"},
		{123450, "USD", LocaleES, "$1.234,50"},
		{123456789, "USD", LocaleEN, "$1,234,567.89"},
		{123456789, "USD", LocaleES, "$1.234.567,89"},
		{-123450, "USD", LocaleEN, "-$1,234.50"},
		{-123450, "USD", LocaleES, "$-1.234,50"},
		{123450, "COP", LocaleEN, "COP 1,234.50"},
		{123450, "COP", LocaleES, "COP 1.234,50"},
		{123450, "USD", Locale(""), "$1,234.50"},
	} {
		if got := formatStaffMoney(c.cents, c.currency, c.locale); got != c.want {
			t.Errorf("formatStaffMoney(%d, %s, %q) = %q, want %q", c.cents, c.currency, c.locale, got, c.want)
		}
	}
}
