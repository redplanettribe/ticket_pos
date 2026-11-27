package platform

import (
	"fmt"
	"strings"
	"time"
)

// The Event Recap (spec #884, #891): the one email that tells the people
// answerable for an Event how it went, sent the morning after it is over.
//
// STAFF MAIL, in the platform chrome with no Event imagery and no tracking,
// written in its reader's Staff Locale. It names the Event and nobody else: no
// Customer, no buyer, no Holder. This ticket's mail is minimal - that the Event
// is over, the instant its figures are as of, and the three staff surfaces that
// hold the live ones - and the figures themselves arrive with later tickets,
// between the as-of paragraph and the links.

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

// Text is the body: the paragraphs, then each link as its lead and its URL on
// the next line.
func (r EventRecap) Text() string {
	text := strings.Join(r.blocks(), "\n\n")
	for _, link := range r.links() {
		text += link.text(r.Locale)
	}
	return text
}

// blocks is the recap's body before its links, one string per paragraph,
// which both parts carry word for word.
func (r EventRecap) blocks() []string {
	return []string{
		fmt.Sprintf(eventRecapOverCopy.in(r.Locale), r.EventName),
		fmt.Sprintf(eventRecapAsOfCopy.in(r.Locale), formatEcuadorInstant(r.SentAt, r.Locale)),
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

// eventRecapCard is the Event Recap's card: the heading, the paragraphs, the
// first link as the one button with the sentence leading to it, and the other
// links as plain ones, each under its lead.
type eventRecapCard struct {
	Heading  string
	Blocks   [][]string
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
// part's own paragraphs, Sales Trends as the one button, and Reach and the
// Sales list as plain links.
func (r EventRecap) HTML(chrome MailChrome) (string, error) {
	card := eventRecapCard{Heading: r.Subject()}
	for _, block := range r.blocks() {
		card.Blocks = append(card.Blocks, strings.Split(block, "\n"))
	}
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
