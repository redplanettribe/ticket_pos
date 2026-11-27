package platform

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The Event Recap (spec #884, #891): the one email that tells the people
// answerable for an Event how it went, sent the morning after it is over.
//
// STAFF MAIL, in the platform chrome with no Event imagery and no tracking,
// written in its reader's Staff Locale. It names the Event and nobody else: no
// Customer, no buyer, no Holder. It says that the Event is over, the instant
// its figures are as of, the Event's sales figures (#886), and the three staff
// surfaces that hold the live ones. The figures later tickets add go where the
// sales figures are, between the as-of paragraph and the links.

// EventRecapWentLiveAt is the instant the Event Recap was switched on: the
// start of its deploy day on the platform clock. Only an Event that becomes
// over at or after it is recapped, so launch day brings no mail about Events
// from months ago (spec #884, "No backfill").
//
// A CONSTANT WITH NO DARK-SHIP FLAG, on the pattern of
// TicketAssignmentWentLiveAt: the mail carries no new personal data and
// recording that it was sent is not a privacy act, so nothing needs to ship
// closed. It is the same in every environment. If the deploy slips past this
// day, move it forward to the real one BEFORE the deploy, or the first run
// recaps the Events that ended in between; once the feature is live it is a
// historical fact and must not be moved again.
var EventRecapWentLiveAt = time.Date(2026, 11, 27, 5, 0, 0, 0, time.UTC)

// EventRecap is one Event Recap to one reader.
type EventRecap struct {
	To     string
	Locale Locale
	// EventName is the Event the recap is about, named in the subject so
	// recaps for different Events can be told apart in an inbox.
	EventName string
	// SentAt is the instant the recap was composed, which is the instant its
	// figures are as of. Rendered on the platform clock in either language.
	SentAt time.Time
	// SalesTrendsURL, ReachURL and SalesURL are the Event's Sales Trends, Reach
	// and Sales list in the staff app, where the live figures are. Each is ""
	// where the deployment cannot build it, which drops that link.
	SalesTrendsURL string
	ReachURL       string
	SalesURL       string
	// Sales is the Event's sales figures as they stood at SentAt (#886), or nil
	// for a recap that states none: an Event with External Registration, which
	// produces no Ticket Sale and so has no Tickets Sold to say anything about.
	Sales *EventRecapSales
}

// EventRecapSales is an Event's sales figures as the Event Recap states them
// (#886): active Ticket Sales only, each Sale Reversal having dropped its Sale
// whole from every one of them.
//
// THE MAIL DOES NO ARITHMETIC. Every figure arrives computed, by the read
// models behind the staff surfaces the mail links to, so the two agree. This
// type only says how each is written down.
//
// IT CARRIES NO PERSON: counts, money, Sales Channels and the names of Ticket
// Types, never a buyer.
type EventRecapSales struct {
	// Currency is the Organization's, which Takings and Net Proceeds are in.
	// The reader's Staff Locale decides the marks around an amount and never
	// which currency it is in.
	Currency string
	// TicketsSold and TicketSales are for the Event entire, on every Sales
	// Channel: how many tickets it moved, and in how many checkouts.
	TicketsSold int
	TicketSales int
	// TakingsCents is what the Event made on whatever Sales Channel it sold
	// (ADR 0040), and NetProceedsCents what its Online Sales left the
	// Organization, which is what the platform hands over.
	TakingsCents     int
	NetProceedsCents int
	// ReversedSales is how many of the Event's Ticket Sales are reversed. They
	// are in none of the figures above, and this is what says so.
	ReversedSales int
	// Channels is Tickets Sold and Ticket Sales split by Sales Channel, in the
	// order they are to be listed.
	Channels []EventRecapChannel
	// TicketTypes is Tickets Sold split by Ticket Type, the Event's whole
	// catalog in its display order, a Ticket Type that sold nothing included.
	TicketTypes []EventRecapTicketType
}

// EventRecapChannel is what one Sales Channel sold: `online`, `in_person` or
// `import`.
type EventRecapChannel struct {
	Channel     string
	TicketsSold int
	TicketSales int
}

// EventRecapTicketType is how many tickets of one Ticket Type sold.
type EventRecapTicketType struct {
	Name        string
	TicketsSold int
}

var (
	eventRecapSubjectCopy = translated(
		"Event Recap: %s",
		"Resumen del evento: %s",
	)
	eventRecapOverCopy = translated(
		"%s is over.",
		"%s ha terminado.",
	)
	// The as-of sentence: what the recap says is a reading at one instant, and
	// the three things that still move the live figures afterwards.
	eventRecapAsOfCopy = translated(
		"The figures in this Event Recap are as they stood on %s (Ecuador time), when it was sent. A door sale recorded late, a Sale Import or a Sale Reversal still changes the live figures, which the staff app holds.",
		"Las cifras de este Resumen del evento son las que había el %s (hora de Ecuador), cuando se envió. Una venta en puerta registrada tarde, una importación de ventas o una reversión de venta todavía cambian las cifras en vivo, que están en la aplicación del personal.",
	)
	// The sales figures (#886). The figures are named in the staff app's words
	// (its casing where it has the label, the glossary's for a term it does
	// not label), so the mail reads as the surfaces it links to do; Spanish
	// says "entradas vendidas"
	// and never "asistentes", since a ticket sold is not a person who came.
	eventRecapTicketsSoldCopy = translated(
		"Tickets sold",
		"Entradas vendidas",
	)
	eventRecapTicketSalesCopy = translated(
		"Ticket sales",
		"Ventas de entradas",
	)
	eventRecapTakingsCopy = translated(
		"Takings",
		"Recaudación",
	)
	eventRecapNetProceedsCopy = translated(
		"Net Proceeds of Online Sales",
		"Ingresos netos de las ventas en línea",
	)
	eventRecapReversedSalesCopy = translated(
		"Reversed sales",
		"Ventas revertidas",
	)
	// Said only when something was reversed: the totals above are smaller than
	// the reader may remember, and this is why.
	eventRecapReversedLeftOutCopy = translated(
		"%s (left out of every figure here)",
		"%s (fuera de todas las cifras de este correo)",
	)
	eventRecapSalesChannelCopy = translated(
		"Sales Channel",
		"Canal de venta",
	)
	eventRecapTicketTypeCopy = translated(
		"Ticket Type",
		"Tipo de entrada",
	)
	eventRecapChannelOnlineCopy = translated(
		"Online",
		"En línea",
	)
	eventRecapChannelInPersonCopy = translated(
		"In person",
		"Presencial",
	)
	eventRecapChannelImportCopy = translated(
		"Import",
		"Importación",
	)
	eventRecapSalesTrendsLeadCopy = translated(
		"See how its sales moved in Sales Trends:",
		"Vea cómo se movieron sus ventas en Tendencias de ventas:",
	)
	eventRecapSalesTrendsButtonCopy = translated(
		"Open Sales Trends",
		"Abrir Tendencias de ventas",
	)
	eventRecapReachLeadCopy = translated(
		"Reach:",
		"Alcance:",
	)
	eventRecapSalesLeadCopy = translated(
		"Sales list:",
		"Lista de ventas:",
	)
	eventRecapPreheaderCopy = translated(
		"%s is over. The live figures are in the staff app.",
		"%s ha terminado. Las cifras en vivo están en la aplicación del personal.",
	)
	// The footer reason: an Event Owner, or whoever else answers for the
	// Event, which is why it names the Event and not a role.
	eventRecapFooterReasonCopy = translated(
		"You're getting this because you answer for %s.",
		"Recibe este correo porque responde por %s.",
	)
)

// Subject names the Event.
func (r EventRecap) Subject() string {
	return fmt.Sprintf(eventRecapSubjectCopy.in(r.Locale), r.EventName)
}

// Text is the body: the sections, then each link as its lead and its URL on
// the next line.
func (r EventRecap) Text() string {
	sections := r.sections()
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, section.text())
	}
	text := strings.Join(parts, "\n\n")
	for _, link := range r.links() {
		text += link.text(r.Locale)
	}
	return text
}

// eventRecapSection is one piece of the recap's body before its links: a
// paragraph, as its lines, or a table. Both parts are written from the same
// sections, so they carry the same words.
type eventRecapSection struct {
	Lines []string
	Table *mailTable
}

func (s eventRecapSection) text() string {
	if s.Table != nil {
		return s.Table.text()
	}
	return strings.Join(s.Lines, "\n")
}

// sections is the recap's body before its links: that the Event is over, the
// instant its figures are as of, and the sales figures where it states any.
func (r EventRecap) sections() []eventRecapSection {
	sections := []eventRecapSection{
		{Lines: []string{fmt.Sprintf(eventRecapOverCopy.in(r.Locale), r.EventName)}},
		{Lines: []string{fmt.Sprintf(eventRecapAsOfCopy.in(r.Locale), formatEcuadorInstant(r.SentAt, r.Locale))}},
	}
	if r.Sales != nil {
		sections = append(sections, r.Sales.sections(r.Locale)...)
	}
	return sections
}

// sections is the sales figures: the Event's totals as one figure a line,
// then Tickets Sold and Ticket Sales by Sales Channel and Tickets Sold by
// Ticket Type, each as a table.
//
// AN EVENT THAT SOLD NOTHING HAS NOTHING TO SPLIT, so its two tables are
// dropped and its totals stand alone, each a zero. That it sold nothing is
// news the recap exists to carry, so the totals are always stated.
func (s EventRecapSales) sections(locale Locale) []eventRecapSection {
	reversed := formatStaffCount(s.ReversedSales, locale)
	if s.ReversedSales > 0 {
		reversed = fmt.Sprintf(eventRecapReversedLeftOutCopy.in(locale), reversed)
	}
	sections := []eventRecapSection{{Lines: []string{
		eventRecapTicketsSoldCopy.in(locale) + ": " + formatStaffCount(s.TicketsSold, locale),
		eventRecapTicketSalesCopy.in(locale) + ": " + formatStaffCount(s.TicketSales, locale),
		eventRecapTakingsCopy.in(locale) + ": " + formatStaffMoney(s.TakingsCents, s.Currency, locale),
		eventRecapNetProceedsCopy.in(locale) + ": " + formatStaffMoney(s.NetProceedsCents, s.Currency, locale),
		eventRecapReversedSalesCopy.in(locale) + ": " + reversed,
	}}}
	if s.TicketsSold == 0 {
		return sections
	}
	if len(s.Channels) > 0 {
		table := &mailTable{Columns: []string{
			eventRecapSalesChannelCopy.in(locale), eventRecapTicketsSoldCopy.in(locale), eventRecapTicketSalesCopy.in(locale),
		}}
		for _, channel := range s.Channels {
			table.Rows = append(table.Rows, []string{
				salesChannelName(channel.Channel, locale),
				formatStaffCount(channel.TicketsSold, locale),
				formatStaffCount(channel.TicketSales, locale),
			})
		}
		sections = append(sections, eventRecapSection{Table: table})
	}
	if len(s.TicketTypes) > 0 {
		table := &mailTable{Columns: []string{eventRecapTicketTypeCopy.in(locale), eventRecapTicketsSoldCopy.in(locale)}}
		for _, ticketType := range s.TicketTypes {
			table.Rows = append(table.Rows, []string{ticketType.Name, formatStaffCount(ticketType.TicketsSold, locale)})
		}
		sections = append(sections, eventRecapSection{Table: table})
	}
	return sections
}

// salesChannelName is a Sales Channel as the staff app names it. A channel
// this does not know is printed as it is stored, rather than dropped with its
// figures.
func salesChannelName(channel string, locale Locale) string {
	switch channel {
	case "online":
		return eventRecapChannelOnlineCopy.in(locale)
	case "in_person":
		return eventRecapChannelInPersonCopy.in(locale)
	case "import":
		return eventRecapChannelImportCopy.in(locale)
	default:
		return channel
	}
}

// formatStaffCount writes a count with the reader's marks, as the staff app's
// formatNumber does: 1,500 in English and 1.500 in Spanish.
func formatStaffCount(count int, locale Locale) string {
	sign := ""
	if count < 0 {
		sign, count = "-", -count
	}
	digits := strconv.Itoa(count)
	separator := ","
	if locale == LocaleES {
		separator = "."
	}
	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteString(separator)
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

// formatStaffMoney writes an amount held in minor units for a reader of staff
// mail, with the marks the staff app's formatMoney gives the same Staff Locale
// (en-US and es-EC): "$1,234.50" in English and "$1.234,50" in Spanish.
//
// THE LOCALE DECIDES THE MARKS AND NEVER THE CURRENCY, which is the
// Organization's whoever reads. US dollars are written with their sign, as
// the staff app writes them in both languages; any other currency is written
// with its code before the amount, which is unambiguous in either.
//
// Distinct from formatMoney, which writes a Customer's receipt line plainly
// and in no locale, for reconciling against a card statement.
func formatStaffMoney(cents int, currency string, locale Locale) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	decimal := "."
	if locale == LocaleES {
		decimal = ","
	}
	amount := fmt.Sprintf("%s%s%02d", formatStaffCount(cents/100, locale), decimal, cents%100)
	symbol := currency + " "
	if currency == "USD" {
		symbol = "$"
	}
	switch {
	case !negative:
		return symbol + amount
	case locale == LocaleES:
		// es-EC puts the sign after the currency.
		return symbol + "-" + amount
	default:
		return "-" + symbol + amount
	}
}

// links is the three staff surfaces, Sales Trends first: it is the HTML
// part's one button, and the other two are plain links under it.
func (r EventRecap) links() []mailLink {
	return []mailLink{
		{lead: eventRecapSalesTrendsLeadCopy, label: eventRecapSalesTrendsButtonCopy, url: r.SalesTrendsURL},
		{lead: eventRecapReachLeadCopy, url: r.ReachURL},
		{lead: eventRecapSalesLeadCopy, url: r.SalesURL},
	}
}

// formatEcuadorInstant renders an instant as the date and the minute it falls
// on in Ecuador, in the reader's language: formatEcuadorDate with the time of
// day, for a mail that states when it was true and not only on which day.
func formatEcuadorInstant(instant time.Time, locale Locale) string {
	clock := instant.In(ecuadorLocation()).Format("15:04")
	if locale == LocaleES {
		return formatEcuadorDate(instant, locale) + " a las " + clock
	}
	return formatEcuadorDate(instant, locale) + " at " + clock
}

// eventRecapCard is the Event Recap's card: the heading, the sections, the
// first link as the one button with the sentence leading to it, and the other
// links as plain ones, each under its lead.
type eventRecapCard struct {
	Heading  string
	Sections []eventRecapSection
	LinkLead string
	Button   *mailButton
	Links    []eventRecapLinkView
}

// eventRecapLinkView is one plain link: its lead and the URL, printed whole
// as the text part prints it.
type eventRecapLinkView struct {
	Lead string
	URL  string
}

// HTML is the Event Recap's HTML part: the subject as its heading, the text
// part's own paragraphs and tables, Sales Trends as the one button, and Reach
// and the Sales list as plain links.
func (r EventRecap) HTML(chrome MailChrome) (string, error) {
	card := eventRecapCard{Heading: r.Subject(), Sections: r.sections()}
	for i, link := range r.links() {
		switch {
		case link.url == "":
		case i == 0:
			button := newMailButton(r.Locale, link.label.in(r.Locale), link.url)
			card.LinkLead, card.Button = link.lead.in(r.Locale), &button
		default:
			card.Links = append(card.Links, eventRecapLinkView{Lead: link.lead.in(r.Locale), URL: link.url})
		}
	}
	return renderMail(mailKindEventRecap, r.Locale, chrome, r.Subject(),
		fmt.Sprintf(eventRecapPreheaderCopy.in(r.Locale), r.EventName),
		fmt.Sprintf(eventRecapFooterReasonCopy.in(r.Locale), r.EventName),
		card,
	)
}
