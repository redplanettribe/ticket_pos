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
// its figures are as of, the Event's sales figures (#886), the attention its
// page drew before it was over (#887), and the staff surfaces that hold the
// live ones: three for an Event that sells Ticket Types.
//
// AN EVENT WITH EXTERNAL REGISTRATION (#888) sells nothing here, so its recap
// states its Registration Link clicks in place of every sales figure, beside
// the same reach figures, and links to Reach only.

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
	// ToOrgAdmin is whether To is written to as an Org Admin of the Event's
	// Organization, because the Event has no Event Owner (#885), rather than
	// as one of its Event Owners. It changes the footer's reason and nothing
	// else.
	ToOrgAdmin bool
	// EventName is the Event the recap is about, named in the subject so
	// recaps for different Events can be told apart in an inbox.
	EventName string
	// SentAt is the instant the recap was composed, which is the instant its
	// figures are as of. Rendered on the platform clock in either language.
	SentAt time.Time
	// SalesTrendsURL, ReachURL and SalesURL are the Event's Sales Trends, Reach
	// and Sales list in the staff app, where the live figures are. Each is ""
	// where the deployment cannot build it, which drops that link. The recap
	// of an Event with External Registration prints ReachURL alone, whatever
	// the other two hold.
	SalesTrendsURL string
	ReachURL       string
	SalesURL       string
	// Sales is the Event's sales figures as they stood at SentAt (#886), or nil
	// for a recap that states none: an Event with External Registration, which
	// produces no Ticket Sale and so has no Tickets Sold to say anything about.
	Sales *EventRecapSales
	// Reach is the attention the Event's page drew until the Event was over
	// (#887), or nil for a recap that states none.
	Reach *EventRecapReach
	// RegistrationLinkClicks is how many times the Event's page handed
	// somebody to its Registration Link, as the count stood when the run read
	// the Event, moments before SentAt (#888).
	// Set for an Event with External Registration and nil for one that sells
	// Ticket Types. Set, it makes the recap the External Registration one: the
	// count stands in place of every sales figure, Sales is not stated even if
	// given, and the mail links to Reach only.
	//
	// CLICKS, NEVER REGISTRATIONS AND NEVER PEOPLE. The platform loses sight
	// of the buyer at the link and never learns whether they signed up, and
	// the count is not deduplicated. The mail calls it clicks and nothing else.
	RegistrationLinkClicks *int
}

// footerReasonCopy is the footer's reason, by why the reader was written to.
func (r EventRecap) footerReasonCopy() mailCopy {
	if r.ToOrgAdmin {
		return eventRecapOrgAdminFooterReasonCopy
	}
	return eventRecapFooterReasonCopy
}

// externalRegistration is whether this is the recap of an Event with External
// Registration.
func (r EventRecap) externalRegistration() bool {
	return r.RegistrationLinkClicks != nil
}

// EventRecapReach is the attention an Event's page drew before the Event was
// over, as the Event Recap states it (#887): loads counted in the hourly
// buckets of ADR 0057 whose hour began before the over instant, so somebody
// looking at a finished Event does not inflate them.
//
// LOADS, NEVER PEOPLE, AND NEVER A RATE. Every figure is a count, a floor on
// what the page drew. The mail divides none of them into another and states
// no rate built on Page Views, because a figure that counts loads cannot be
// divided into one that counts sales without lying when crawler traffic
// moves. EventRecapReach has no field a rate could be read from.
type EventRecapReach struct {
	// PageViews is every load of the Event's page until the Event was over,
	// the ones that came through an Affiliate Link included, each once.
	PageViews int
	// AffiliateLinks is the Event's busiest Affiliate Links, the one with the
	// most Clicks first. Empty where no Affiliate Link drew a Click, which
	// drops the block.
	AffiliateLinks []EventRecapAffiliateLink
}

// EventRecapAffiliateLink is one Affiliate Link in the recap: the name staff
// gave it, its Clicks until the Event was over, and its Attributed Sales.
type EventRecapAffiliateLink struct {
	Name   string
	Clicks int
	// AttributedSales is how many active Ticket Sales are attributed to the
	// link, a reversed one dropped. Nil on an Event with External
	// Registration, whose links can attribute none: not measured, which a
	// zero would misstate.
	AttributedSales *int
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
	// The as-of sentence of an Event with External Registration (#888), which
	// has no door sale, Sale Import or Sale Reversal to move its figures.
	eventRecapExternalAsOfCopy = translated(
		"The figures in this Event Recap are as they stood on %s (Ecuador time), when it was sent. The staff app holds the live figures.",
		"Las cifras de este Resumen del evento son las que había el %s (hora de Ecuador), cuando se envió. Las cifras en vivo están en la aplicación del personal.",
	)
	// The one figure an Event with External Registration states in place of
	// its sales figures (#888), named by the glossary's term for the link and
	// the staff app's word for the count. Clicks: never registrations, never
	// people, never anybody going.
	eventRecapRegistrationLinkClicksCopy = translated(
		"Registration Link clicks",
		"Clics en el Enlace de inscripción",
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
	// The reach figures (#887), in the staff app's words for them. Each says
	// "until the Event was over", since the Reach surface the mail links to
	// goes on counting loads afterwards and will state more.
	eventRecapPageViewsCopy = translated(
		"Page views until the Event was over",
		"Vistas de página hasta que terminó el evento",
	)
	eventRecapBusiestLinksCopy = translated(
		"Its busiest Affiliate Links, by Clicks until the Event was over:",
		"Sus Enlaces de Afiliado con más clics hasta que terminó el evento:",
	)
	eventRecapAffiliateLinkCopy = translated(
		"Affiliate Link",
		"Enlace de Afiliado",
	)
	eventRecapClicksCopy = translated(
		"Clicks",
		"Clics",
	)
	eventRecapAttributedSalesCopy = translated(
		"Attributed sales",
		"Ventas atribuidas",
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
	// Reach as the one link, and so the button, of an Event with External
	// Registration (#888).
	eventRecapReachOnlyLeadCopy = translated(
		"See the attention its page drew in Reach:",
		"Vea la atención que recibió su página en Alcance:",
	)
	eventRecapReachButtonCopy = translated(
		"Open Reach",
		"Abrir Alcance",
	)
	eventRecapPreheaderCopy = translated(
		"%s is over. The live figures are in the staff app.",
		"%s ha terminado. Las cifras en vivo están en la aplicación del personal.",
	)
	// The footer reason, by why the reader was written to: as an Event Owner
	// of the Event, or as an Org Admin of its Organization because the Event
	// has no Event Owner (#885). In the glossary's Spanish terms.
	eventRecapFooterReasonCopy = translated(
		"You're getting this because you're an Event Owner of %s.",
		"Recibe este correo porque es Responsable del evento %s.",
	)
	eventRecapOrgAdminFooterReasonCopy = translated(
		"You're getting this because %s has no Event Owner and you're an Org Admin of its Organization.",
		"Recibe este correo porque %s no tiene Responsable del evento y usted es Administrador de la organización.",
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
// instant its figures are as of, the sales figures where it states any, and
// then the reach figures where it states any.
//
// AN EVENT WITH EXTERNAL REGISTRATION states its Registration Link clicks
// where the sales figures would be, and no sales figure at all (#888).
func (r EventRecap) sections() []eventRecapSection {
	asOf := eventRecapAsOfCopy
	if r.externalRegistration() {
		asOf = eventRecapExternalAsOfCopy
	}
	sections := []eventRecapSection{
		{Lines: []string{fmt.Sprintf(eventRecapOverCopy.in(r.Locale), r.EventName)}},
		{Lines: []string{fmt.Sprintf(asOf.in(r.Locale), formatEcuadorInstant(r.SentAt, r.Locale))}},
	}
	switch {
	case r.externalRegistration():
		sections = append(sections, eventRecapSection{Lines: []string{
			eventRecapRegistrationLinkClicksCopy.in(r.Locale) + ": " + formatStaffCount(*r.RegistrationLinkClicks, r.Locale),
		}})
	case r.Sales != nil:
		sections = append(sections, r.Sales.sections(r.Locale)...)
	}
	if r.Reach != nil {
		sections = append(sections, r.Reach.sections(r.Locale)...)
	}
	return sections
}

// sections is the reach figures: the Page Views as one figure on a line,
// always stated, a zero included, and then the busiest Affiliate Links as a
// table under the sentence that says what it ranks them by.
//
// AN EVENT WITH NO AFFILIATE LINK THAT DREW A CLICK HAS NO BLOCK: neither the
// sentence nor the table. The Attributed sales column is dropped where no
// link carries the figure, which is an Event with External Registration.
//
// NO RATE. The counts are written down and nothing is divided.
func (r EventRecapReach) sections(locale Locale) []eventRecapSection {
	sections := []eventRecapSection{{Lines: []string{
		eventRecapPageViewsCopy.in(locale) + ": " + formatStaffCount(r.PageViews, locale),
	}}}
	if len(r.AffiliateLinks) == 0 {
		return sections
	}
	attributed := false
	for _, link := range r.AffiliateLinks {
		attributed = attributed || link.AttributedSales != nil
	}
	table := &mailTable{Columns: []string{eventRecapAffiliateLinkCopy.in(locale), eventRecapClicksCopy.in(locale)}}
	if attributed {
		table.Columns = append(table.Columns, eventRecapAttributedSalesCopy.in(locale))
	}
	for _, link := range r.AffiliateLinks {
		row := []string{link.Name, formatStaffCount(link.Clicks, locale)}
		if attributed {
			sales := 0
			if link.AttributedSales != nil {
				sales = *link.AttributedSales
			}
			row = append(row, formatStaffCount(sales, locale))
		}
		table.Rows = append(table.Rows, row)
	}
	return append(sections,
		eventRecapSection{Lines: []string{eventRecapBusiestLinksCopy.in(locale)}},
		eventRecapSection{Table: table},
	)
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

// links is the staff surfaces the recap links to. For an Event that sells
// Ticket Types they are three, Sales Trends first: it is the HTML part's one
// button, and the other two are plain links under it.
//
// AN EVENT WITH EXTERNAL REGISTRATION LINKS TO REACH ONLY (#888), whatever
// URLs it was given: Sales Trends and the Sales list have nothing to show for
// an Event that produces no Ticket Sale. Reach is then the one button.
func (r EventRecap) links() []mailLink {
	if r.externalRegistration() {
		return []mailLink{{lead: eventRecapReachOnlyLeadCopy, label: eventRecapReachButtonCopy, url: r.ReachURL}}
	}
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
// part's own paragraphs and tables, the first link as the one button (Sales
// Trends, or Reach for an Event with External Registration), and any other
// links as plain ones.
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
		fmt.Sprintf(r.footerReasonCopy().in(r.Locale), r.EventName),
		card,
	)
}
