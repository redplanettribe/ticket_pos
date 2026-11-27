package platform

import (
	"regexp"
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
		for _, variant := range []string{"-expiring", "-expired", "-shortfall", "-external"} {
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
// Warning's Issuer page and the Event Recap's Sales Trends, or its Reach for
// an Event with External Registration, are the ones, and every other staff
// kind has none.
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
			case strings.HasPrefix(f.name, "eventrecap-external-"):
				want, label = goldenExternalEventRecap(f.locale).ReachURL, eventRecapReachButtonCopy.in(f.locale)
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

// The Event Recap of an Event with External Registration links to Reach and
// to nothing else, in both parts and both languages, even when it is given
// the Sales Trends and Sales list URLs (#888).
func TestEventRecapOfAnEventWithExternalRegistrationLinksToReachOnly(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		recap := goldenExternalEventRecap(locale)
		body, err := recap.HTML(goldenChrome)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		// Every staff surface the card links to. The button and the line to
		// paste under it are one link said twice, and the chrome's own links
		// go to the platform, not the staff app.
		hrefs := map[string]bool{}
		for _, link := range findAll(parseMail(t, body), func(n *html.Node) bool { return n.Data == "a" }) {
			if href := attr(link, "href"); strings.HasPrefix(href, "https://staff.example.com/") {
				hrefs[href] = true
			}
		}
		if len(hrefs) != 1 || !hrefs[recap.ReachURL] {
			t.Errorf("%s: the HTML part links to %v in the staff app, want Reach only", locale, hrefs)
		}
		for part, read := range eventRecapParts(t, recap) {
			if part == "text" && !strings.Contains(read, recap.ReachURL) {
				t.Errorf("%s: the text part does not carry %s", locale, recap.ReachURL)
			}
			for _, unwanted := range []string{
				recap.SalesTrendsURL, recap.SalesURL,
				"Sales Trends", "Sales list", "Tendencias de ventas", "Lista de ventas",
			} {
				if strings.Contains(read, unwanted) {
					t.Errorf("%s: the %s part says %q:\n%s", locale, part, unwanted, read)
				}
			}
		}
	}
}

// The Event Recap of an Event with External Registration states its
// Registration Link clicks in place of every sales figure, beside its Page
// Views and its Affiliate Links' Clicks, in the reader's marks (#888).
func TestEventRecapOfAnEventWithExternalRegistrationStatesClicksInPlaceOfSalesFigures(t *testing.T) {
	for locale, c := range map[Locale]struct{ want, unwanted []string }{
		LocaleEN: {
			want: []string{
				"Registration Link clicks: 1,960",
				"Page views until the Event was over: 15,320",
				"Affiliate Link Clicks Instagram de María 2,140 Radio Quito 880",
			},
			unwanted: []string{
				"Tickets sold", "Ticket sales", "Takings", "Net Proceeds", "Reversed sales",
				"Sales Channel", "Ticket Type", "Attributed sales", "$", "door sale", "Sale Import", "Sale Reversal",
			},
		},
		LocaleES: {
			want: []string{
				"Clics en el Enlace de inscripción: 1.960",
				"Vistas de página hasta que terminó el evento: 15.320",
				"Enlace de Afiliado Clics Instagram de María 2.140 Radio Quito 880",
			},
			unwanted: []string{
				"Entradas vendidas", "Ventas de entradas", "Recaudación", "Ingresos netos", "Ventas revertidas",
				"Canal de venta", "Tipo de entrada", "Ventas atribuidas", "$", "venta en puerta", "importación de ventas", "reversión de venta",
			},
		},
	} {
		recap := goldenExternalEventRecap(locale)
		// Sales figures handed to such a recap are still not stated.
		recap.Sales = goldenEventRecap(locale).Sales
		for part, read := range eventRecapParts(t, recap) {
			for _, figure := range c.want {
				if !strings.Contains(read, figure) {
					t.Errorf("%s: the %s part does not say %q:\n%s", locale, part, figure, read)
				}
			}
			for _, figure := range c.unwanted {
				if strings.Contains(read, figure) {
					t.Errorf("%s: the %s part says %q:\n%s", locale, part, figure, read)
				}
			}
		}
	}
}

// The Event Recap never renders clicks as people (spec #884, #888): the
// platform loses sight of the visitor at the Registration Link and never
// learns whether they signed up, and a Click or a Page View is a load, not a
// person.
//
// HELD OVER EVERY PART AND THE SUBJECT, in both languages, for the External
// Registration fixture, one nobody clicked and one clicked once. The
// Registration Link is named, as the thing that was clicked; once its name is
// read past, no word for a registration, a person or somebody going is left.
// The recap of an Event that sells Ticket Types is not held to the word list:
// it states no Registration Link clicks, and its "In person" is a Sales
// Channel and its "registrada" a sale recorded.
func TestEventRecapNeverRendersClicksAsPeople(t *testing.T) {
	edited := func(locale Locale, clicks int) EventRecap {
		recap := goldenExternalEventRecap(locale)
		recap.RegistrationLinkClicks = goldenCount(clicks)
		return recap
	}
	linkName := map[Locale]string{LocaleEN: "Registration Link", LocaleES: "Enlace de inscripción"}
	peopleWords := regexp.MustCompile(`(?i)(registr|regist[eé]r|inscri|sign[- ]?up|sign(ed|ing) up|rsvp|apunt)\pL*` +
		`|\b(people|person|persons|attendees?|attending|attended|guests?|visitors?|going|went|came|audience|headcount|turnout` +
		`|personas?|gente|asistentes?|asistir\pL*|asistieron|asistencia|invitad[oa]s?|visitantes?|público|van|fueron|vinieron)\b`)

	for _, locale := range []Locale{LocaleEN, LocaleES} {
		for name, recap := range map[string]EventRecap{
			"external": goldenExternalEventRecap(locale), "nobody clicked": edited(locale, 0),
			"one click": edited(locale, 1),
		} {
			parts := eventRecapParts(t, recap)
			parts["subject"] = recap.Subject()
			for part, read := range parts {
				if part != "subject" && !strings.Contains(read, linkName[locale]) {
					t.Errorf("%s, %s: the %s part does not name the Registration Link:\n%s", locale, name, part, read)
				}
				read = strings.ReplaceAll(read, linkName[locale], "")
				if word := peopleWords.FindString(read); word != "" {
					t.Errorf("%s, %s: the %s part says %q, a word for people or registrations:\n%s", locale, name, part, word, read)
				}
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
	recap.Reach = nil
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

// eventRecapParts is one recap's two parts as a reader reads them, spacing
// collapsed. The HTML part is read without its style sheet, which no reader
// sees.
func eventRecapParts(t *testing.T, recap EventRecap) map[string]string {
	t.Helper()
	body, err := recap.HTML(goldenChrome)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := parseMail(t, body)
	read := textOf(doc)
	for _, style := range findAll(doc, func(n *html.Node) bool { return n.Data == "style" }) {
		read = strings.Replace(read, textOf(style), "", 1)
	}
	return map[string]string{
		"HTML": read,
		"text": strings.Join(strings.Fields(recap.Text()), " "),
	}
}

// The Event Recap states the attention the Event's page drew until the Event
// was over: its Page Views, and its busiest Affiliate Links with their Clicks
// and Attributed Sales, in the marks of the reader's Staff Locale (#887).
func TestEventRecapStatesTheReachFiguresInTheReadersStaffLocale(t *testing.T) {
	for locale, want := range map[Locale][]string{
		LocaleEN: {
			"Page views until the Event was over: 15,320",
			"Its busiest Affiliate Links, by Clicks until the Event was over:",
			"Affiliate Link Clicks Attributed sales",
			"Instagram de María 2,140 96",
			"Radio Quito 880 41",
			"Volantes 15 0",
		},
		LocaleES: {
			"Vistas de página hasta que terminó el evento: 15.320",
			"Sus Enlaces de Afiliado con más clics hasta que terminó el evento:",
			"Enlace de Afiliado Clics Ventas atribuidas",
			"Instagram de María 2.140 96",
			"Radio Quito 880 41",
			"Volantes 15 0",
		},
	} {
		for part, read := range eventRecapParts(t, goldenEventRecap(locale)) {
			for _, figure := range want {
				if !strings.Contains(read, figure) {
					t.Errorf("%s: the %s part does not say %q:\n%s", locale, part, figure, read)
				}
			}
		}
	}
}

// An Event with no Affiliate Link to list states its Page Views, a zero
// included, and has no Affiliate Link block; one whose links attribute no
// sale, an Event with External Registration, lists their Clicks alone; and a
// recap with no reach figures states none.
func TestEventRecapReachWithoutAffiliateLinksHasNoBlock(t *testing.T) {
	recap := goldenEventRecap(LocaleEN)
	recap.Reach = &EventRecapReach{}
	for part, read := range eventRecapParts(t, recap) {
		if !strings.Contains(read, "Page views until the Event was over: 0") {
			t.Errorf("the %s part does not state zero Page Views:\n%s", part, read)
		}
		if strings.Contains(read, "Affiliate Link") || strings.Contains(read, "Clicks") || strings.Contains(read, "Attributed sales") {
			t.Errorf("the %s part has an Affiliate Link block with no link to list:\n%s", part, read)
		}
	}

	recap.Reach = &EventRecapReach{PageViews: 40, AffiliateLinks: []EventRecapAffiliateLink{{Name: "Flyer", Clicks: 12}}}
	for part, read := range eventRecapParts(t, recap) {
		if !strings.Contains(read, "Affiliate Link Clicks Flyer 12") {
			t.Errorf("the %s part does not list the link's Clicks alone:\n%s", part, read)
		}
		if strings.Contains(read, "Attributed sales") {
			t.Errorf("the %s part states Attributed Sales nobody measured:\n%s", part, read)
		}
	}

	recap.Reach = nil
	for part, read := range eventRecapParts(t, recap) {
		if strings.Contains(read, "Page views") || strings.Contains(read, "Affiliate Link") {
			t.Errorf("the %s part of a recap with no reach figures states some:\n%s", part, read)
		}
	}
}

// The Event Recap never renders a rate built on Page Views (spec #884, the
// rule Platform Trends and Organization Trends keep): a figure that counts
// loads cannot be divided into one that counts sales or people without lying
// when crawler traffic moves.
//
// HELD THREE WAYS, in both parts and both languages, over the fixture and
// over recaps whose figures would make a tidy rate: no percent sign, no word
// for a rate, and none of the quotients the figures could be divided into.
func TestEventRecapNeverRendersARateBuiltOnPageViews(t *testing.T) {
	// 200 Page Views, 50 Tickets Sold in 40 Ticket Sales, a link with 80
	// Clicks and 20 Attributed Sales: every quotient is a round figure that
	// would be easy to print and easy to find.
	tidy := func(locale Locale) EventRecap {
		recap := goldenEventRecap(locale)
		recap.Sales = &EventRecapSales{
			Currency: "USD", TicketsSold: 50, TicketSales: 40, TakingsCents: 100000, NetProceedsCents: 90000,
			Channels:    []EventRecapChannel{{Channel: "online", TicketsSold: 50, TicketSales: 40}},
			TicketTypes: []EventRecapTicketType{{Name: "General", TicketsSold: 50}},
		}
		recap.Reach = &EventRecapReach{
			PageViews:      200,
			AffiliateLinks: []EventRecapAffiliateLink{{Name: "Flyer", Clicks: 80, AttributedSales: goldenCount(20)}},
		}
		return recap
	}
	withoutLinks := func(locale Locale) EventRecap {
		recap := tidy(locale)
		recap.Reach.AffiliateLinks = nil
		return recap
	}
	withoutSales := func(locale Locale) EventRecap {
		recap := tidy(locale)
		recap.Sales = nil
		recap.Reach.AffiliateLinks[0].AttributedSales = nil
		return recap
	}
	rateWords := regexp.MustCompile(`(?i)\b(rate|rates|ratio|conversion|converted|per view|per page view|percent|tasa|tasas|proporción|conversión|porcentaje|por vista|por ciento)\b`)
	// Tickets Sold, Ticket Sales, Clicks and Attributed Sales over Page
	// Views, and Attributed Sales over Clicks, as a share or a percentage in
	// either locale's marks.
	quotients := regexp.MustCompile(`\b(0[.,]25|0[.,]2|0[.,]4|0[.,]1|25|20|40|10)\s?%|\b0[.,](25|2|4|1)\b`)

	for _, locale := range []Locale{LocaleEN, LocaleES} {
		for name, recap := range map[string]EventRecap{
			"fixture": goldenEventRecap(locale), "tidy": tidy(locale),
			"without links": withoutLinks(locale), "without sales": withoutSales(locale),
		} {
			parts := eventRecapParts(t, recap)
			parts["subject"] = recap.Subject()
			for part, read := range parts {
				if strings.Contains(read, "%") {
					t.Errorf("%s, %s: the %s part has a percent sign:\n%s", locale, name, part, read)
				}
				if word := rateWords.FindString(read); word != "" {
					t.Errorf("%s, %s: the %s part says %q, the word of a rate:\n%s", locale, name, part, word, read)
				}
				if name != "fixture" {
					if quotient := quotients.FindString(read); quotient != "" {
						t.Errorf("%s, %s: the %s part states %q, a figure divided into another:\n%s", locale, name, part, quotient, read)
					}
				}
			}
		}
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
