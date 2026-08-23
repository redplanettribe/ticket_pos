package platform

import (
	"strings"
	"testing"
	"time"
)

// What an Assignment Reminder actually says (#362, parent #361, ADR 0051).
//
// These assert on the RENDERED words, on email_answerreminder_test.go's terms:
// this mail is sent by a job nobody is watching, days after the sale, so the
// rendered text is the only place its wording is ever inspected — and, like
// the Answer Reminder, what is ABSENT is as much under test as what is present.

const assignmentReminderLink = "https://storefront.test/tickets/confirm?token=abc.def"

func assignmentReminder() AssignmentReminder {
	return AssignmentReminder{
		To:                "ana@example.com",
		FirstName:         "Ana",
		EventName:         "Noche de Jazz",
		EventStartsAt:     time.Date(2026, 9, 12, 1, 30, 0, 0, time.UTC), // 20:30 the 11th in Guayaquil
		EventTimezone:     "America/Guayaquil",
		UnassignedTickets: 2,
		TotalTickets:      3,
		ConfirmationLink:  assignmentReminderLink,
		SaleCreatedAt:     time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC),
	}
}

// The zero Locale is English, as everywhere: the floor of ADR 0033's chain.
func TestAssignmentReminderIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	reminder := assignmentReminder()

	if got := reminder.Subject(); got != "2 of your 3 tickets for Noche de Jazz have no name yet" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Hi Ana,",
		"Noche de Jazz",
		"Friday 11 September, 20:30",
		"2 of your 3 tickets have no address yet",
		"Assign them from your tickets page:\n" + assignmentReminderLink,
		"we will email the ticket to that address",
		"the organizer will see the address next to the ticket",
		"Assigning is optional and your tickets are valid either way.",
		"at most one more reminder",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

// Written in the SALE LOCALE, which for this reader is the language they
// bought in (ADR 0033). Where the Locale comes from is the sweep's business and
// is pinned in the integration suite; this pins the copy and its REGISTER —
// usted, matching every other Customer-facing message.
func TestAssignmentReminderIsWrittenInSpanish(t *testing.T) {
	reminder := assignmentReminder()
	reminder.Locale = LocaleES

	if got := reminder.Subject(); got != "2 de sus 3 entradas para Noche de Jazz aún no tienen nombre" {
		t.Fatalf("subject = %q, want the Spanish subject", got)
	}
	text := reminder.Text()
	for _, want := range []string{
		"Hola Ana,",
		"Noche de Jazz",
		"viernes 11 de septiembre, 20:30",
		"2 de sus 3 entradas aún no tienen dirección",
		"Asígnelas desde su página de entradas:\n" + assignmentReminderLink,
		"enviaremos la entrada a esa dirección",
		"la organización verá la dirección junto a la entrada",
		"Asignar es opcional y sus entradas son válidas igualmente.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "have no address") || strings.Contains(text, "Assign them") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

// ONE TICKET UNASSIGNED READS AS ONE TICKET, in both languages: a tally that
// said "1 tickets" would be the kind of mail a reader stops trusting.
func TestAssignmentReminderCountsOneTicketInTheSingular(t *testing.T) {
	reminder := assignmentReminder()
	reminder.UnassignedTickets = 1

	if got := reminder.Subject(); got != "1 of your 3 tickets for Noche de Jazz has no name yet" {
		t.Fatalf("subject = %q", got)
	}
	if text := reminder.Text(); !strings.Contains(text, "1 of your 3 tickets has no address yet") {
		t.Fatalf("text = %q, want the singular tally", text)
	}
	reminder.Locale = LocaleES
	if text := reminder.Text(); !strings.Contains(text, "1 de sus 3 entradas aún no tiene dirección") {
		t.Fatalf("text = %q, want the singular Spanish tally", text)
	}
}

// IT NAMES NOTHING A FORWARDED MAIL SHOULD CARRY. The struct has no field for
// a price, a Tax ID or a Sale Confirmation reference, which is the real
// enforcement — but a template is edited by hand, so the rendered words are
// pinned (ADR 0044's disclosure rule, kept by ADR 0051).
func TestAssignmentReminderNamesNoPriceTaxIDOrReference(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		reminder := assignmentReminder()
		reminder.Locale = locale
		rendered := reminder.Subject() + "\n" + reminder.Text()
		for _, forbidden := range []string{
			"reference", "Reference", "referencia", "Referencia",
			"$", "Tax ID", "RUC", "cédula", "price", "precio", "total paid", "pagado",
		} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("the Assignment Reminder (%s) contains %q:\n%s", locale, forbidden, rendered)
			}
		}
	}
}

// THE DISCLOSURE SENTENCE IS NOT OPTIONAL (ADR 0047): the buyer is told, before
// they act, that giving an address mails the Holder and shows the address to
// the Organization. Both halves must survive in both languages.
func TestAssignmentReminderDisclosesWhatAssigningDoes(t *testing.T) {
	en := assignmentReminder().Text()
	if !strings.Contains(en, "email the ticket") || !strings.Contains(en, "organizer will see the address") {
		t.Fatalf("English text = %q, want both halves of the ADR 0047 disclosure", en)
	}
	es := assignmentReminder()
	es.Locale = LocaleES
	if text := es.Text(); !strings.Contains(text, "enviaremos la entrada") || !strings.Contains(text, "organización verá la dirección") {
		t.Fatalf("Spanish text = %q, want both halves of the ADR 0047 disclosure", text)
	}
}

// THE CONFIRMATION LINK APPEARS EXACTLY ONCE, and it is the only URL. The link
// opens the Sale without a sign-in, so a second copy anywhere is a second
// credential in a mail that gets forwarded.
func TestAssignmentReminderCarriesTheConfirmationLinkOnce(t *testing.T) {
	text := assignmentReminder().Text()
	if got := strings.Count(text, assignmentReminderLink); got != 1 {
		t.Fatalf("the link appears %d times, want once:\n%s", got, text)
	}
	if got := strings.Count(text, "http"); got != 1 {
		t.Fatalf("%d URLs in the mail, want the Confirmation Link alone:\n%s", got, text)
	}
}

// AN UNKNOWN TIMEZONE DROPS THE DATE RATHER THAN LYING ABOUT IT, exactly as
// the Follow Digest does: the Event is still named and the link still works.
func TestAssignmentReminderOmitsTheDateItCannotPlace(t *testing.T) {
	reminder := assignmentReminder()
	reminder.EventTimezone = "Mars/Olympus"
	text := reminder.Text()
	if strings.Contains(text, "September") || strings.Contains(text, "20:30") {
		t.Fatalf("text = %q, want no date line for a zone Go cannot load", text)
	}
	if !strings.Contains(text, "Noche de Jazz") || !strings.Contains(text, assignmentReminderLink) {
		t.Fatalf("text = %q, want the Event and the link regardless", text)
	}
}

// THE GO-LIVE SENTENCE (ADR 0051, #364). The fixture's Sale is from after
// TicketAssignmentWentLiveAt, so its mail carries nothing about the feature
// being new: the ordinary reminder, unchanged. A Sale from before it reads one
// extra sentence, in both languages, between the tally and the action.
const (
	assignmentReminderGoLiveEN = "When you bought, tickets could not yet be assigned; now they can."
	assignmentReminderGoLiveES = "Cuando compró, las entradas aún no se podían asignar; ahora sí."
)

func TestAssignmentReminderTellsAPreFeatureBuyerThatAssigningIsNew(t *testing.T) {
	reminder := assignmentReminder()
	reminder.SaleCreatedAt = TicketAssignmentWentLiveAt.Add(-time.Second)

	text := reminder.Text()
	if !strings.Contains(text, assignmentReminderGoLiveEN) {
		t.Fatalf("text = %q, want the English go-live sentence", text)
	}
	tally := strings.Index(text, "have no address yet")
	sentence := strings.Index(text, assignmentReminderGoLiveEN)
	action := strings.Index(text, "Assign them from your tickets page")
	if !(tally < sentence && sentence < action) {
		t.Fatalf("text = %q, want the go-live sentence between the tally and the action", text)
	}

	reminder.Locale = LocaleES
	text = reminder.Text()
	if !strings.Contains(text, assignmentReminderGoLiveES) {
		t.Fatalf("text = %q, want the Spanish go-live sentence", text)
	}
	if strings.Contains(text, "could not yet be assigned") {
		t.Fatalf("text = %q, want no English left in it", text)
	}
}

func TestAssignmentReminderSaysNothingAboutGoLiveToANewerBuyer(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		reminder := assignmentReminder()
		reminder.Locale = locale
		reminder.SaleCreatedAt = TicketAssignmentWentLiveAt // the moment itself is "after"
		text := reminder.Text()
		if strings.Contains(text, assignmentReminderGoLiveEN) || strings.Contains(text, assignmentReminderGoLiveES) {
			t.Fatalf("text (%s) = %q, want no go-live sentence for a Sale made once the feature existed", locale, text)
		}
		if strings.Contains(text, "could not yet") || strings.Contains(text, "no se podían") {
			t.Fatalf("text (%s) = %q, want no trace of the go-live sentence", locale, text)
		}
	}
}
