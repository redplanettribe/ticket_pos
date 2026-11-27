package integration

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Event Recap's sales figures (spec #884, #886), through the sweep
// endpoint and the recording mail sender: what the mail states about an Event
// sold on every Sales Channel, with Sales reversed on two of them.
//
// THE SALES ARE MADE THROUGH THE REAL APIS: online checkout, Sale Import, the
// Customer's own undo and the Sale Import undo. A door sale has no recording
// endpoint, so it is an imported batch moved onto the in_person channel in
// SQL, as the Platform Trends tests stage one.
//
// THE FIGURES ARE HELD TO TWO THINGS: the numbers the fixture adds up to, and
// what the staff surfaces the mail links to (the Sales summary and Sales
// Trends) answer for the same Event at the same moment.

// recapSalesEvent is an Event sold on every Sales Channel, with two Ticket
// Types and two reversed Sales, over at day 0, 23:00.
//
//	online      Ana 2 GA; Bea 1 GA + 1 VIP        4 tickets, 2 sales
//	in person   Dora 3 GA; Dario 2 GA             5 tickets, 2 sales
//	import      Caro 4 GA; Ciro 1 VIP             5 tickets, 2 sales
//	reversed    Rita 3 GA online; Ramon 7 GA imported
//
// GA is 20.00 and VIP 50.00, so the Event took 12 x 20.00 + 2 x 50.00 =
// 340.00, of which the Online Sales left 3 x 20.00 + 50.00 = 110.00.
func recapSalesEvent(t *testing.T, env *testEnv, f recapFixture) (eventID, gaID, vipID string) {
	t.Helper()
	eventID, gaID = publishCheckoutEvent(t, env, f.admin, "Jazz Night", "jazz-night", 2000, 100)
	vipID = createTicketTypeWithCapacity(t, env, f.admin, eventID, "VIP", 5000, 20)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")

	for _, cart := range []map[string]any{
		checkoutBody("ana.online@example.com", "Anastasia", "Onlineson", cartLine(gaID, 2)),
		checkoutBody("bea.online@example.com", "Beatrix", "Webber", cartLine(gaID, 1), cartLine(vipID, 1)),
	} {
		begun := beginCheckoutOK(t, env, testOrgSlug, "jazz-night", cart)
		confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	}
	begun := beginCheckoutOK(t, env, testOrgSlug, "jazz-night",
		checkoutBody("rita.reversed@example.com", "Rita", "Undoer", cartLine(gaID, 3)))
	undone := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	undoOwnSale(t, env, "rita.reversed@example.com", undone.ConfirmationRef)

	row := func(email, first, last, ticketTypeID string, quantity int) map[string]any {
		return map[string]any{
			"customer_email": email, "customer_first_name": first, "customer_last_name": last,
			"ticket_type_id": ticketTypeID, "quantity": quantity, "payment_method": "cash", "sold_at": "2026-07-07T11:00:00Z",
		}
	}
	commitBatch(t, env, f.admin, eventID, "imported", []map[string]any{
		row("caro.import@example.com", "Carolina", "Importer", gaID, 4),
		row("ciro.import@example.com", "Ciro", "Spreadsheet", vipID, 1),
	})
	// One batch per door sale: moveImportToTheDoor moves a batch of one.
	for key, sale := range map[string]map[string]any{
		"door-1": row("dora.door@example.com", "Dorotea", "Doorman", gaID, 3),
		"door-2": row("dario.door@example.com", "Dario", "Cashbox", gaID, 2),
	} {
		moveImportToTheDoor(t, env, commitBatch(t, env, f.admin, eventID, key, []map[string]any{sale}))
	}
	undoneBatch := commitBatch(t, env, f.admin, eventID, "undone", []map[string]any{
		row("ramon.reversed@example.com", "Ramon", "Mistaken", gaID, 7),
	})
	undoBatch(t, env, f.admin, eventID, undoneBatch)

	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Jazz Night", over.Add(-3*time.Hour), &over)
	return eventID, gaID, vipID
}

// recapSalesCustomers is everybody who bought in recapSalesEvent, by the
// words that would name them.
var recapSalesCustomers = []string{
	"ana.online@example.com", "Anastasia", "Onlineson",
	"bea.online@example.com", "Beatrix", "Webber",
	"rita.reversed@example.com", "Rita", "Undoer",
	"caro.import@example.com", "Carolina", "Importer",
	"ciro.import@example.com", "Ciro", "Spreadsheet",
	"dora.door@example.com", "Dorotea", "Doorman",
	"dario.door@example.com", "Dario", "Cashbox",
	"ramon.reversed@example.com", "Ramon", "Mistaken",
}

// onlyRecap sweeps the morning after day 0 and returns the one recap mailed.
func onlyRecap(t *testing.T, env *testEnv) platform.EventRecap {
	t.Helper()
	sharedEmail.Reset()
	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	recaps := sharedEmail.EventRecapsSent()
	if len(recaps) != 1 {
		t.Fatalf("got %d recaps, want one", len(recaps))
	}
	return recaps[0]
}

// recapParts is the mail's two parts as a reader reads them, the spacing
// that pads the text part's tables collapsed.
func recapParts(t *testing.T, recap platform.EventRecap) map[string]string {
	t.Helper()
	body, err := recap.HTML(platform.MailChrome{})
	if err != nil {
		t.Fatalf("render the HTML part: %v", err)
	}
	return map[string]string{
		"text": strings.Join(strings.Fields(recap.Text()), " "),
		"HTML": strings.Join(strings.Fields(html.UnescapeString(recapTag.ReplaceAllString(body, " "))), " "),
	}
}

// recapTag is one tag of the HTML part, which recapParts reads past.
var recapTag = regexp.MustCompile(`<[^>]*>`)

// Tickets Sold and Ticket Sales for the Event entire and by Sales Channel,
// Tickets Sold by Ticket Type, Takings, Net Proceeds and the count of
// reversed Sales, with each reversed Sale out of every total.
func TestEventRecapStatesTheEventsSalesFigures(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	recapSalesEvent(t, env, f)

	recap := onlyRecap(t, env)
	sales := recap.Sales
	if sales == nil {
		t.Fatal("the recap states no sales figures")
	}

	if sales.TicketsSold != 14 || sales.TicketSales != 6 {
		t.Errorf("Tickets Sold / Ticket Sales = %d / %d, want 14 / 6: the reversed 3 and 7 are out", sales.TicketsSold, sales.TicketSales)
	}
	if sales.Currency != "USD" || sales.TakingsCents != 34000 || sales.NetProceedsCents != 11000 {
		t.Errorf("Takings / Net Proceeds = %d / %d %s, want 34000 / 11000 USD", sales.TakingsCents, sales.NetProceedsCents, sales.Currency)
	}
	if sales.ReversedSales != 2 {
		t.Errorf("reversed Sales = %d, want 2", sales.ReversedSales)
	}
	wantChannels := []platform.EventRecapChannel{
		{Channel: "online", TicketsSold: 4, TicketSales: 2},
		{Channel: "in_person", TicketsSold: 5, TicketSales: 2},
		{Channel: "import", TicketsSold: 5, TicketSales: 2},
	}
	if len(sales.Channels) != len(wantChannels) {
		t.Fatalf("channels = %+v, want %+v", sales.Channels, wantChannels)
	}
	for i, want := range wantChannels {
		if sales.Channels[i] != want {
			t.Errorf("channel %d = %+v, want %+v", i, sales.Channels[i], want)
		}
	}
	wantTypes := []platform.EventRecapTicketType{{Name: "GA", TicketsSold: 12}, {Name: "VIP", TicketsSold: 2}}
	if len(sales.TicketTypes) != len(wantTypes) {
		t.Fatalf("Ticket Types = %+v, want %+v", sales.TicketTypes, wantTypes)
	}
	for i, want := range wantTypes {
		if sales.TicketTypes[i] != want {
			t.Errorf("Ticket Type %d = %+v, want %+v", i, sales.TicketTypes[i], want)
		}
	}

	for part, read := range recapParts(t, recap) {
		for _, figure := range []string{
			"Tickets sold: 14",
			"Ticket sales: 6",
			"Takings: $340.00",
			"Net Proceeds of Online Sales: $110.00",
			"Reversed sales: 2 (left out of every figure here)",
			"Sales Channel Tickets sold Ticket sales",
			"Online 4 2",
			"In person 5 2",
			"Import 5 2",
			"Ticket Type Tickets sold",
			"GA 12",
			"VIP 2",
		} {
			if !strings.Contains(read, figure) {
				t.Errorf("the %s part does not say %q:\n%s", part, figure, read)
			}
		}
	}
}

// The mail agrees with the staff surfaces it links to: what it states is what
// the Sales summary and Sales Trends answer for the Event at that moment.
func TestEventRecapAgreesWithTheSalesSummaryAndSalesTrends(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID, _, _ := recapSalesEvent(t, env, f)

	sales := onlyRecap(t, env).Sales
	if sales == nil {
		t.Fatal("the recap states no sales figures")
	}

	summary := salesSummaryOK(t, env, f.admin, eventID)
	if sales.TicketsSold != summary.TicketsSold || sales.TicketSales != summary.SalesCount || sales.NetProceedsCents != summary.NetProceedsCents {
		t.Errorf("the recap says %d tickets in %d sales and %d Net Proceeds; the Sales summary says %d, %d and %d",
			sales.TicketsSold, sales.TicketSales, sales.NetProceedsCents, summary.TicketsSold, summary.SalesCount, summary.NetProceedsCents)
	}

	trends := salesTrendsOK(t, env, f.admin, eventID)
	takings, sold := 0, map[string]int{}
	for _, day := range trends.Days {
		for _, line := range day.Lines {
			takings += line.TakingsCents
			sold[line.TicketTypeID] += line.Quantity
		}
	}
	if sales.TakingsCents != takings || sales.ReversedSales != trends.ReversedCount || sales.Currency != trends.Currency {
		t.Errorf("the recap says %d %s Takings and %d reversed; Sales Trends says %d %s and %d",
			sales.TakingsCents, sales.Currency, sales.ReversedSales, takings, trends.Currency, trends.ReversedCount)
	}
	if len(sales.TicketTypes) != len(trends.TicketTypes) {
		t.Fatalf("the recap lists %d Ticket Types, Sales Trends %d", len(sales.TicketTypes), len(trends.TicketTypes))
	}
	for i, ticketType := range trends.TicketTypes {
		if got := sales.TicketTypes[i]; got.Name != ticketType.Name || got.TicketsSold != sold[ticketType.ID] {
			t.Errorf("Ticket Type %d is %+v in the recap; Sales Trends has %s with %d sold", i, got, ticketType.Name, sold[ticketType.ID])
		}
	}
}

// Money is in the Organization's currency and written in the marks of each
// reader's own Staff Locale, and Spanish says entradas vendidas, never
// asistentes.
func TestEventRecapWritesMoneyInEachReadersStaffLocale(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID, _, _ := recapSalesEvent(t, env, f)
	duenaID, duena := addRecapMember(t, env, f.admin, "duena@example.com", "event_owner")
	setStaffLocale(t, env, duena, "es")
	assignToEvent(t, env, f.admin, eventID, duenaID, "event_owner")
	sharedEmail.Reset()

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	recaps := map[string]platform.EventRecap{}
	for _, recap := range sharedEmail.EventRecapsSent() {
		recaps[recap.To] = recap
	}
	if len(recaps) != 2 {
		t.Fatalf("recaps went to %d addresses, want the two Event Owners", len(recaps))
	}

	for to, want := range map[string][]string{
		"owner@example.com": {
			"Takings: $340.00", "Net Proceeds of Online Sales: $110.00", "Tickets sold: 14", "In person 5 2",
		},
		"duena@example.com": {
			"Recaudación: $340,00", "Ingresos netos de las ventas en línea: $110,00", "Entradas vendidas: 14",
			"Ventas de entradas: 6", "Ventas revertidas: 2", "Canal de venta Entradas vendidas Ventas de entradas",
			"Presencial 5 2", "Tipo de entrada Entradas vendidas",
		},
	} {
		for part, read := range recapParts(t, recaps[to]) {
			for _, figure := range want {
				if !strings.Contains(read, figure) {
					t.Errorf("%s: the %s part does not say %q:\n%s", to, part, figure, read)
				}
			}
			if strings.Contains(strings.ToLower(read), "asistentes") {
				t.Errorf("%s: the %s part says asistentes", to, part)
			}
		}
	}
	if es, en := recaps["duena@example.com"].Sales, recaps["owner@example.com"].Sales; es == nil || en == nil ||
		es.TakingsCents != en.TakingsCents || es.Currency != en.Currency {
		t.Errorf("the two readers were told different money: %+v and %+v", en, es)
	}
}

// The mail names no Customer, whichever Sales Channel they bought on and
// whether or not their Sale was reversed.
func TestEventRecapWithSalesFiguresNamesNoCustomer(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	recapSalesEvent(t, env, f)

	recap := onlyRecap(t, env)
	body, err := recap.HTML(platform.MailChrome{})
	if err != nil {
		t.Fatalf("render the HTML part: %v", err)
	}
	for part, read := range map[string]string{"subject": recap.Subject(), "text": recap.Text(), "HTML": body} {
		for _, customer := range recapSalesCustomers {
			if strings.Contains(read, customer) {
				t.Errorf("the %s names a Customer (%q)", part, customer)
			}
		}
	}
}

// An Event that sold nothing is recapped with its totals stated as zeros and
// nothing to split them by.
func TestEventRecapOfAnEventThatSoldNothingStatesZeros(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Sold Nothing", recapAt(0, 23, 0))

	recap := onlyRecap(t, env)
	if recap.Sales == nil || recap.Sales.TicketsSold != 0 || recap.Sales.TicketSales != 0 ||
		recap.Sales.TakingsCents != 0 || recap.Sales.NetProceedsCents != 0 || recap.Sales.ReversedSales != 0 {
		t.Fatalf("sales figures = %+v, want every total zero", recap.Sales)
	}
	for part, read := range recapParts(t, recap) {
		for _, figure := range []string{"Tickets sold: 0", "Ticket sales: 0", "Takings: $0.00", "Net Proceeds of Online Sales: $0.00", "Reversed sales: 0"} {
			if !strings.Contains(read, figure) {
				t.Errorf("the %s part does not say %q:\n%s", part, figure, read)
			}
		}
		if strings.Contains(read, "Sales Channel") || strings.Contains(read, "Ticket Type") {
			t.Errorf("the %s part splits totals that are all zero:\n%s", part, read)
		}
	}
}

// An Event with External Registration produces no Ticket Sale, so its recap
// states no sales figure at all, not a row of zeros.
func TestEventRecapOfAnEventWithExternalRegistrationStatesNoSalesFigures(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := createExternalAffiliateEvent(t, env, f.admin, "Meetup", "meetup")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Meetup", over.Add(-3*time.Hour), &over)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	publishRecapEvent(t, env, f.admin, eventID)

	recap := onlyRecap(t, env)
	if recap.Sales != nil {
		t.Fatalf("sales figures = %+v, want none", recap.Sales)
	}
	for part, read := range recapParts(t, recap) {
		for _, figure := range []string{"Tickets sold", "Ticket sales", "Takings", "Net Proceeds", "Reversed sales"} {
			if strings.Contains(read, figure) {
				t.Errorf("the %s part states %q for an Event that sells nothing here:\n%s", part, figure, read)
			}
		}
	}
}
