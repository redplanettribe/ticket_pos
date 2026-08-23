package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A Holder is told when they stop holding a Ticket (#327, parent #322,
// ADR 0046).
//
// ONE RULE WITH TWO CAUSES. An accepted Holder stops holding a Ticket because
// the buyer reassigned it, or because the Ticket Sale was reversed. From where
// the Holder sits those are the same event — they had a ticket and now they do
// not — so the platform must not be talkative in one case and silent in the
// other. Nearly every test below is written as a PAIR for that reason: whatever
// is asserted about a reassignment is asserted about a reversal beside it, so a
// change that fixed one and forgot the other fails here rather than in an inbox.
//
// THE HARDEST RULE TO GET RIGHT IS THE SILENCE. Somebody in `assigned` who never
// accepted is mailed in NEITHER case. They were never told they had the ticket,
// so telling them they lost it would be the platform's first and only word to a
// stranger, about something they never knew existed. That is
// TestAHolderWhoNeverAcceptedIsToldNothingInEitherCase, and it is asserted by
// COUNTING mail rather than by reading any, because silence has no words in it.
//
// The tests run at the HTTP seam per docs/testing.md, and reach for the captured
// sender the way a Holder reaches for their inbox.

// noLongerHoldingMailsTo returns every No Longer Holding notice sent to an
// address, in order. Callers assert on the LENGTH first: "told exactly once" and
// "never told at all" are both facts about how many exist.
func noLongerHoldingMailsTo(env *testEnv, address string) []platform.NoLongerHolding {
	var found []platform.NoLongerHolding
	for _, mail := range env.email.NoLongerHoldingsSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	return found
}

// theOneNoLongerHoldingMailTo insists on exactly one notice to an address.
func theOneNoLongerHoldingMailTo(t *testing.T, env *testEnv, address string) platform.NoLongerHolding {
	t.Helper()
	found := noLongerHoldingMailsTo(env, address)
	if len(found) != 1 {
		t.Fatalf("No Longer Holding mails to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// assertMailGivesNoCauseAndNamesNoBuyer reads the words the Holder actually
// reads and insists they disclose nothing about the buyer or about what the
// buyer did.
//
// ASSERTED ON THE RENDERED MESSAGE, not on the struct, because the struct having
// no buyer field is only half the property: somebody could compose a name or a
// cause into the copy, which reads friendlier and is exactly the widening
// ADR 0044's disclosure rule — carried over unchanged by ADR 0046 — forbids.
func assertMailGivesNoCauseAndNamesNoBuyer(t *testing.T, mail platform.NoLongerHolding, buyerRef string) {
	t.Helper()
	rendered := mail.Subject() + "\n" + mail.Text()
	for _, forbidden := range []string{
		// The buyer, by name, by address, and by their purchase.
		"Ana", "Lopez", "ana@example.com", buyerRef,
		// The cause, in either direction. Each is false in the case it does not
		// describe, and each is a fact about the buyer's decisions.
		"cancel", "revers", "refund", "reassign", "someone else",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("the No Longer Holding mail contains %q.\n"+
				"It names the Event and nothing else: no cause, because one message must be true of\n"+
				"BOTH a reassignment and a Sale Reversal, and no buyer, because who bought the ticket\n"+
				"and what they decided is a fact about somebody else's purchase (ADR 0044, ADR 0046).",
				forbidden)
		}
	}
}

// customerAreaOf reads a signed-in Customer's Area, decoding only the two things
// #327 is about: what they hold, and whether their own purchases survived.
func customerAreaOf(t *testing.T, env *testEnv, session string) (holding []struct {
	TicketID string `json:"ticket_id"`
	Event    struct {
		Name string `json:"name"`
	} `json:"event"`
}, raw string) {
	t.Helper()
	resp, body := env.get(t, "/api/v1/customer/ticket-sales", authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var area struct {
		Holding []struct {
			TicketID string `json:"ticket_id"`
			Event    struct {
				Name string `json:"name"`
			} `json:"event"`
		} `json:"holding"`
	}
	if err := json.Unmarshal(body.Data, &area); err != nil {
		t.Fatalf("decode customer area: %v", err)
	}
	return area.Holding, string(body.Data)
}

// assertHolderKeptTheirRecord insists the Customer survives losing the ticket,
// Verified, with the name they gave.
//
// AN ACCEPTANCE CRITERION AND A DELETION HAZARD. A Holder who stops holding a
// Ticket is still a person who proved an address and typed their name; nothing
// in this flow may delete a Customer, unverify one, or blank what they said. It
// reads the table because no API hands a test somebody else's Customer record.
func assertHolderKeptTheirRecord(t *testing.T, env *testEnv, email, firstName string) {
	t.Helper()
	customer, exists := readHolderCustomer(t, env, email)
	if !exists {
		t.Fatalf("the Customer for %s is gone; losing a ticket is not a reason to delete a person", email)
	}
	if !customer.verifiedAt.Valid {
		t.Errorf("%s is no longer Verified; they proved that address by clicking, and losing the ticket does not unprove it", email)
	}
	if customer.firstName != firstName {
		t.Errorf("%s's first name reads %q, want %q — the Holder keeps the name they gave", email, customer.firstName, firstName)
	}
}

// THE BUYER REASSIGNS AN ACCEPTED TICKET, AND THE HOLDER IS TOLD ONCE.
//
// The first cause, end to end and at the HTTP seam: Carla accepts, gives her
// name and answers her question; Ana hands the same Ticket to Elena; Carla gets
// exactly one mail that explains nothing, the Event leaves her Area, and she is
// still a Customer with everything she said.
//
// Elena gets her own Assignment mail at the same moment. The two are separate
// messages to separate people saying opposite things, and neither names the
// other or the buyer.
func TestReassigningAnAcceptedTicketTellsTheDisplacedHolderOnce(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// Carla accepts and makes herself known: a name, and her own answer.
	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	carlasToken := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, carlasToken)
	if resp, body, _ := publicLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": carlasToken, "first_name": "Carla", "last_name": "Ruiz",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("Carla could not give her name: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body, _ := publicLinkRequest(t, env, http.MethodPut,
		assignmentLinkQuestionPath+f.sizeQuestion.ID, map[string]any{"token": carlasToken, "text": "S"},
	); resp.StatusCode != http.StatusOK {
		t.Fatalf("Carla could not answer her own question: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// She is holding the Event before anything happens to it.
	carla := customerSignIn(t, env, "carla@example.com")
	if holding, _ := customerAreaOf(t, env, carla); len(holding) != 1 {
		t.Fatalf("Carla holds %d Tickets before the reassignment, want 1", len(holding))
	}
	if len(noLongerHoldingMailsTo(env, "carla@example.com")) != 0 {
		t.Fatal("Carla was told she stopped holding a Ticket she is still holding")
	}

	// Ana hands the Ticket to Elena.
	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "elena@example.com")

	// ONE MAIL, TO CARLA, NAMING THE EVENT AND NOTHING ELSE.
	mail := theOneNoLongerHoldingMailTo(t, env, "carla@example.com")
	if mail.EventName != "Buyer Fest" {
		t.Errorf("the mail names event=%q, want Buyer Fest", mail.EventName)
	}
	assertMailGivesNoCauseAndNamesNoBuyer(t, mail, f.anaRef)
	// It does not name Elena either. Who has the ticket now is no more Carla's
	// business than who bought it.
	if strings.Contains(mail.Subject()+mail.Text(), "elena") {
		t.Error("the mail names the person who now holds the Ticket")
	}

	// AND NOBODY ELSE IS TOLD. Not Elena, who has just been GIVEN a ticket, and
	// not the buyer, who is looking at the page that told them.
	for _, address := range []string{"elena@example.com", "ana@example.com"} {
		if got := len(noLongerHoldingMailsTo(env, address)); got != 0 {
			t.Errorf("%s received %d No Longer Holding mails", address, got)
		}
	}
	// Elena gets the OTHER message: her own Assignment mail, with her own link.
	// assignmentMailFor insists on exactly one, so this fails loudly on none.
	assignmentMailFor(t, env, "elena@example.com")

	// THE EVENT LEFT HER CUSTOMER AREA. What she sees now reflects what she
	// holds, which is nothing.
	holding, raw := customerAreaOf(t, env, carla)
	if len(holding) != 0 {
		t.Errorf("Carla's Area still holds %d Tickets after the Ticket was handed on: %+v", len(holding), holding)
	}
	if strings.Contains(raw, "Buyer Fest") {
		t.Error("the Event is still on Carla's Area; leaving it there tells somebody to turn up to an Event they cannot get into")
	}

	// AND SHE IS STILL A CUSTOMER, Verified, with her name.
	assertHolderKeptTheirRecord(t, env, "carla@example.com", "Carla")

	// The buyer keeps their own Sale throughout — nothing about assignment moves
	// the Ticket Sale, the money or the Reversal Window off them.
	if _, buyerRaw := customerAreaOf(t, env, f.ana); !strings.Contains(buyerRaw, f.anaRef) {
		t.Error("the buyer lost their own Ticket Sale when they reassigned a Ticket on it")
	}
}

// THE TICKET SALE IS REVERSED, AND EVERY ACCEPTED HOLDER ON IT IS TOLD.
//
// The second cause, through a real reversal at the HTTP seam — a Sale Import
// undo, which is one of the three routes to a Sale Reversal and the one this
// fixture's `import` sale can reach.
//
// TWO HOLDERS, TWO MAILS, ONE EACH. A Sale Reversal takes every Holder on the
// Sale with it (CONTEXT.md), so the count here is per accepted Ticket and not
// per Sale.
//
// THE BUYER KEEPS THE REVERSED SALE, which is the asymmetry this test exists to
// pin. A reversed Ticket Sale is never deleted: it keeps its Sale Confirmation
// reference and stays visible to the Customer and the Organization. Only the
// HOLDER's view loses it.
//
// notify_buyers IS FALSE, deliberately. That toggle is about the imported
// BUYERS, who may never have heard of this platform; it does not reach a Holder,
// who came here, proved an address and is expecting to attend. A build that let
// it gate this mail would be silent about a reversal while talkative about a
// reassignment, which is what #327 forbids.
func TestReversingASaleTellsEveryAcceptedHolderAndLeavesTheBuyersSaleVisible(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// Both of Ana's Tickets are accepted, by two different people.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "elena@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "elena@example.com")))

	carla := customerSignIn(t, env, "carla@example.com")
	elena := customerSignIn(t, env, "elena@example.com")

	// A different Customer entirely, on the same Event and a DIFFERENT Sale.
	// Bruno's batch is not the one being undone, so nobody on it may be touched.
	assignTicketOK(t, env, customerSignIn(t, env, "bruno@example.com"), f.brunoSaleID, f.brunoTicketIDs[0], "diego@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "diego@example.com")))

	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	// ONE MAIL EACH, TO BOTH ACCEPTED HOLDERS, saying exactly what a
	// reassignment says.
	for _, address := range []string{"carla@example.com", "elena@example.com"} {
		mail := theOneNoLongerHoldingMailTo(t, env, address)
		if mail.EventName != "Buyer Fest" {
			t.Errorf("the mail to %s names event=%q", address, mail.EventName)
		}
		assertMailGivesNoCauseAndNamesNoBuyer(t, mail, f.anaRef)
	}

	// AND NOBODY ON ANOTHER SALE IS TOLD ANYTHING. A Sale Reversal is whole-Sale
	// and stops there.
	if got := len(noLongerHoldingMailsTo(env, "diego@example.com")); got != 0 {
		t.Errorf("Diego, holding a Ticket on a DIFFERENT Sale, received %d No Longer Holding mails", got)
	}
	if got := len(env.email.NoLongerHoldingsSent()); got != 2 {
		t.Errorf("the reversal sent %d No Longer Holding mails in total, want 2 — one per accepted Holder on the reversed Sale", got)
	}

	// THE EVENT LEFT BOTH HOLDERS' AREAS.
	for name, session := range map[string]string{"Carla": carla, "Elena": elena} {
		holding, raw := customerAreaOf(t, env, session)
		if len(holding) != 0 {
			t.Errorf("%s's Area still holds %d Tickets on a reversed Sale", name, len(holding))
		}
		if strings.Contains(raw, f.anaRef) {
			t.Errorf("%s's Area carries the buyer's Sale Confirmation reference", name)
		}
	}
	// Diego's did not, because his Sale was not reversed.
	if holding, _ := customerAreaOf(t, env, customerSignIn(t, env, "diego@example.com")); len(holding) != 1 {
		t.Errorf("Diego holds %d Tickets, want the 1 on the Sale nobody reversed", len(holding))
	}

	// THE BUYER STILL SEES THE REVERSED SALE. This is the one place a Holder's
	// view and a buyer's differ, and it is deliberate: the buyer has a financial
	// record here — they paid, and it was undone — and a Holder has none.
	if _, buyerRaw := customerAreaOf(t, env, f.ana); !strings.Contains(buyerRaw, f.anaRef) {
		t.Error("the buyer's reversed Ticket Sale vanished from their Area.\n" +
			"A reversed Ticket Sale is never deleted: it keeps its Sale Confirmation reference and\n" +
			"stays visible to both the Customer and the Organization (CONTEXT.md). Only the\n" +
			"HOLDER's view loses it.")
	}

	// AND BOTH HOLDERS ARE STILL CUSTOMERS.
	assertHolderKeptTheirRecord(t, env, "carla@example.com", "")
	assertHolderKeptTheirRecord(t, env, "elena@example.com", "")

	// A SALE REVERSAL REMAINS WHOLE-SALE. Nothing here voided part of one: Ana's
	// Sale is reversed entire, and Bruno's is untouched.
	var anaStatus, brunoStatus string
	if err := env.db.QueryRow(`SELECT status FROM ticket_sales WHERE id = $1`, f.anaSaleID).Scan(&anaStatus); err != nil {
		t.Fatalf("read Ana's Sale: %v", err)
	}
	if err := env.db.QueryRow(`SELECT status FROM ticket_sales WHERE id = $1`, f.brunoSaleID).Scan(&brunoStatus); err != nil {
		t.Fatalf("read Bruno's Sale: %v", err)
	}
	if anaStatus != "reversed" || brunoStatus != "active" {
		t.Errorf("Ana's Sale = %q and Bruno's = %q, want reversed and active", anaStatus, brunoStatus)
	}
}

// A HOLDER IN `assigned` WHO NEVER ACCEPTED IS TOLD NOTHING, IN EITHER CASE.
//
// THE SHARPEST RULE IN THIS TICKET, and the one a reasonable implementation gets
// wrong by being helpful. The platform never told this person they had anything:
// a mail arrived, they ignored it, and ignoring it IS how somebody declines
// (ADR 0046). Writing to them now would be the platform's first and only word to
// a stranger, about a ticket they never knew existed — and about which they can
// do nothing.
//
// It runs BOTH causes over the same never-accepting address, because the rule is
// one rule: a build that filtered on `accepted` in the reassignment path and
// forgot to in the reversal path would pass half of this.
func TestAHolderWhoNeverAcceptedIsToldNothingInEitherCase(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// Carla is mailed a link on one Ticket and never presses it. Diego likewise
	// on the other.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	assignmentMailFor(t, env, "carla@example.com") // the one word the platform said to her
	assignmentMailFor(t, env, "diego@example.com")

	// CAUSE ONE: the buyer hands Carla's Ticket to Elena.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "elena@example.com")
	if got := len(env.email.NoLongerHoldingsSent()); got != 0 {
		t.Fatalf("reassigning a never-accepted Ticket sent %d No Longer Holding mails, want 0.\n"+
			"An address that was typed and ignored was never told it had anything; telling it now\n"+
			"that it has lost something would be the platform's first and only word to that person.", got)
	}
	// And no Customer was minted for her, so there is nobody there to tell.
	if _, exists := readHolderCustomer(t, env, "carla@example.com"); exists {
		t.Error("a Customer exists for an address that never accepted")
	}

	// CAUSE TWO: the whole Sale is reversed, with Diego and Elena both merely
	// `assigned`.
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)
	if got := len(env.email.NoLongerHoldingsSent()); got != 0 {
		t.Fatalf("reversing a Sale whose Tickets were assigned but never accepted sent %d No Longer Holding mails, want 0", got)
	}
}

// THE MAIL IS WRITTEN IN THE HOLDER'S OWN LANGUAGE.
//
// ADR 0033's chain, read RECIPIENT-FIRST for the reason #325's Assignment mail
// reads it that way: this reader is not party to the sale. They did not buy
// anything, were not on the page the buyer paid on, and may not share the
// buyer's language at all.
//
// Unlike the Assignment mail's reader, though, this one is CERTAINLY a Customer
// — they accepted, which minted or matched a record — so the remembered Mail
// Locale is there to be found, and finding it is what this asserts.
func TestTheNoLongerHoldingMailIsWrittenInTheHoldersOwnLanguage(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// Diego has read this platform in Spanish; his record remembers it. Ana's
	// imported sale names no language at all.
	customerSignInWithLocale(t, env, "diego@example.com", "es")

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "diego@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "diego@example.com")))
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	// Both are displaced by one reversal, so the two mails differ only in the
	// language each reader remembers.
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	diego := theOneNoLongerHoldingMailTo(t, env, "diego@example.com")
	if diego.Locale != platform.LocaleES {
		t.Errorf("the mail to Diego is in %q, want es — his own Mail Locale, not the buyer's language", diego.Locale)
	}
	if !strings.Contains(diego.Subject(), "Ya no tiene una entrada") {
		t.Errorf("subject = %q, want the Spanish", diego.Subject())
	}

	// Carla accepted from an English browser and her record remembers nothing, so
	// the floor is English.
	carla := theOneNoLongerHoldingMailTo(t, env, "carla@example.com")
	if carla.Locale != platform.LocaleEN {
		t.Errorf("the mail to Carla is in %q, want the English floor", carla.Locale)
	}
}

// AN ASSIGNMENT LINK FOR A TICKET THE HOLDER NO LONGER HOLDS SAYS SO, AND NAMES
// NO BUYER IN THAT STATE EITHER.
//
// The Holder is told by mail, but a link sitting in an inbox is pressed anyway —
// weeks later, by somebody who did not read the mail. It must refuse
// INFORMATIVELY rather than rendering blank or erroring: a coded refusal the
// Storefront turns into a sentence, which it does ("This link no longer works.
// Tickets can change hands, and a link stops working when that happens.").
//
// IT REFUSES IDENTICALLY FOR BOTH CAUSES, which is the disclosure rule holding
// in the error state (CONTEXT.md). "Your friend gave your ticket to somebody
// else" and "your friend cancelled the purchase" are both facts about the
// buyer's decisions, so a reassigned link and a reversed one answer exactly as a
// forged one does — and none of the three names anybody.
func TestAnAssignmentLinkForATicketTheHolderNoLongerHoldsSaysSoAndNamesNoBuyer(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// Carla accepts one Ticket and is later displaced by a reassignment; Elena
	// accepts the other and is later displaced by the Sale Reversal.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	carlasToken := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, carlasToken)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "elena@example.com")
	elenasToken := assignmentTokenFrom(t, assignmentMailFor(t, env, "elena@example.com"))
	acceptAssignmentOK(t, env, elenasToken)

	// Cause one, then cause two.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "diego@example.com")
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	for name, token := range map[string]string{
		"the Holder whose Ticket was reassigned": carlasToken,
		"the Holder whose Sale was reversed":     elenasToken,
	} {
		// A CODE THE STOREFRONT HAS COPY FOR, and the SAME code for both, so the
		// two causes are indistinguishable to the reader.
		resp, body, raw := acceptAssignment(t, env, token)
		assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")

		// NOT BLANK. A refusal with no code is a page with nothing to render, and
		// #327 asks for an explanation rather than an empty screen.
		if body.Error == nil || body.Error.Code == "" {
			t.Fatalf("%s pressed their old link and got a refusal with no code to render", name)
		}

		// AND IT NAMES NOBODY AND NOTHING. Asserted on the RAW BYTES, because a
		// widening would arrive as a field somebody added rather than as a change
		// to the message.
		for _, forbidden := range []string{
			"Ana", "Lopez", "ana@example.com", f.anaRef, "diego@example.com",
			"revers", "reassign", "cancel",
		} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("the refusal shown to %s contains %q.\n"+
					"The disclosure rule holds in the error state too: a Ticket that has moved on says\n"+
					"so without naming who bought it or what they did (CONTEXT.md).", name, forbidden)
			}
		}
	}
}

// A SELF-HELD IMPORT HOLDER FOLLOWS THE BUYER'S NOTIFICATION POLICY (#392,
// parent #391, ADR 0055).
//
// EVERYTHING ABOVE THIS LINE IS ABOUT A HOLDER WHO CAME HERE AND ACCEPTED, and
// stays true. What ADR 0055 adds is a SECOND KIND of Holder: the buyer of an
// imported Ticket Sale, seated on Ticket 1 by TRANSCRIPTION rather than by a
// click. They proved nothing, clicked nothing and asked for nothing, and they
// are exactly the person the Sale Import undo's notify toggle and the Sale
// Correction's off-by-default Sale Confirmation checkbox exist to protect. So
// for THEM the notice follows the buyer's policy; for a Holder who accepted by
// Assignment Link it stays unconditional on every path, unchanged.
//
// THE TWO KINDS ARE ALWAYS STAGED SIDE BY SIDE ON THE SAME SALE, so a build that
// silenced the whole `import` channel — the option ADR 0055 rejected by name —
// fails here rather than in the inbox of somebody who really did click a link.

// seatSelfHeldImportHolder makes the buyer of an imported Ticket Sale the
// accepted Holder of one of its Tickets, by hand.
//
// STAGED IN SQL BECAUSE THE FLOW THAT WILL WRITE IT DOES NOT EXIST YET. #392
// lands this policy BEFORE #393's forward rule and #394's backfill, precisely so
// that no window opens in which a batch undo becomes a mailshot — which means
// the row the policy is about cannot yet be produced by any request. These are
// the columns those two tickets go on to write, in the shape migration 084
// already writes them on an Online Sale: the Sale's own Customer, the Sale's own
// timestamps, and no mail of any kind.
func seatSelfHeldImportHolder(t *testing.T, env *testEnv, saleID, ticketID string) {
	t.Helper()
	res, err := env.db.Exec(`
		UPDATE tickets tk
		SET holder_email = lower(btrim(ts.customer_email)),
		    holder_customer_id = ts.customer_id,
		    assigned_at = ts.created_at,
		    accepted_at = ts.created_at
		FROM ticket_sale_lines l
		JOIN ticket_sales ts ON ts.id = l.ticket_sale_id
		WHERE l.id = tk.ticket_sale_line_id AND ts.id = $1 AND tk.id = $2
	`, saleID, ticketID)
	if err != nil {
		t.Fatalf("seat the self-held import Holder: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("seated %d Tickets, want exactly 1 — the fixture proves nothing otherwise", n)
	}
}

// selfHeldAndLinkAccepted stages ADR 0055's two kinds of Holder on ONE imported
// Sale: Ana bought it and is presumed to hold Ticket 1, Carla was handed Ticket
// 2 and really did click. The inbox is emptied afterwards, so every mail counted
// by a test below was caused by the reversal under test.
func selfHeldAndLinkAccepted(t *testing.T, env *testEnv, f assignmentFixture) {
	t.Helper()
	seatSelfHeldImportHolder(t, env, f.anaSaleID, f.anaTicketIDs[0])
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	env.email.Reset()
}

// assertToldExactly insists on the whole population of the notice: who was
// mailed, how many times each, and that nobody outside the map was mailed at
// all. Silence has no words in it, so it is asserted by counting.
func assertToldExactly(t *testing.T, env *testEnv, want map[string]int) {
	t.Helper()
	total := 0
	for address, count := range want {
		if got := len(noLongerHoldingMailsTo(env, address)); got != count {
			t.Errorf("No Longer Holding mails to %s = %d, want %d", address, got, count)
		}
		total += count
	}
	if got := len(env.email.NoLongerHoldingsSent()); got != total {
		t.Errorf("No Longer Holding mails in total = %d, want %d; somebody outside %v was written to",
			got, total, want)
	}
}

// A BATCH UNDO WITH THE TOGGLE OFF TELLS THE HOLDER WHO CLICKED AND SPARES THE
// BUYER WHO WAS PRESUMED.
//
// THE SAFETY GATE FOR THE WHOLE OF #391, and the reason this ticket lands before
// any presumed Holder can exist. Undoing a 185-row import would otherwise mail
// 184 of them "you no longer hold a ticket", bypassing the very toggle built to
// stop an import writing to its buyers.
func TestABatchUndoWithTheToggleOffSparesTheSelfHeldImportBuyerAndTellsTheHolderWhoClicked(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	selfHeldAndLinkAccepted(t, env, f)

	// undoBatch sends notify_buyers=false: the Organization is undoing a
	// transcription and writing to nobody who bought through it.
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	assertToldExactly(t, env, map[string]int{
		"ana@example.com":   0,
		"carla@example.com": 1,
	})
	// And Carla's is the same message it always was: the Event, no cause, no
	// buyer. Silencing one reader must not change what the other reads.
	assertMailGivesNoCauseAndNamesNoBuyer(t, theOneNoLongerHoldingMailTo(t, env, "carla@example.com"), f.anaRef)
}

// A BATCH UNDO WITH THE TOGGLE ON TELLS THE SELF-HELD IMPORT BUYER EXACTLY ONCE.
//
// The other half of "follows the buyer's policy", and the half that keeps this
// from being a suppression: an Organization that HAS chosen to write to its
// imported buyers tells them both things — their purchase was undone, and the
// ticket they were holding is no longer theirs. Once each, never twice.
func TestABatchUndoWithTheToggleOnTellsTheSelfHeldImportBuyerOnce(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	selfHeldAndLinkAccepted(t, env, f)

	resp, body := env.post(t, "/api/v1/staff/events/"+f.eventID+"/sale-imports/"+f.anaBatchID+"/undo",
		map[string]any{"notify_buyers": true}, authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo batch status=%d error=%+v", resp.StatusCode, body.Error)
	}

	assertToldExactly(t, env, map[string]int{
		"ana@example.com":   1,
		"carla@example.com": 1,
	})
	// The notice to the buyer is the SAME notice, composed once for everybody:
	// it names the Event, gives no cause and names no buyer — not even when the
	// buyer is the reader.
	assertMailGivesNoCauseAndNamesNoBuyer(t, theOneNoLongerHoldingMailTo(t, env, "ana@example.com"), f.anaRef)
}

// A SALE CORRECTION TELLS THE SELF-HELD IMPORT BUYER ONLY WHEN THE MEMBER SENDS
// THE NEW SALE CONFIRMATION.
//
// The correction's checkbox is the buyer's notification policy on this path, and
// it is off by default. Without it the buyer would be told they had lost a
// ticket that the same act re-seats them on (#393) — a sentence that is not even
// true by the time they read it.
func TestACorrectionSparesTheSelfHeldImportBuyerUnlessTheConfirmationIsSent(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	selfHeldAndLinkAccepted(t, env, f)

	// Bruno bought his own imported Sale's single Ticket, presumed exactly as Ana
	// did, on a Sale nobody else holds anything on.
	seatSelfHeldImportHolder(t, env, f.brunoSaleID, f.brunoTicketIDs[0])

	// THE DEFAULT: no Sale Confirmation, so no notice to the buyer — and Carla,
	// who clicked, is told all the same.
	correctImportedSaleOK(t, env, f.staffSession, f.eventID, f.anaSaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", f.ticketTypeID, 2, "cash", "2026-07-02T10:00:00Z"))
	assertToldExactly(t, env, map[string]int{
		"ana@example.com":   0,
		"carla@example.com": 1,
	})

	// THE MEMBER CHOOSES TO WRITE: Bruno is being sent a Sale Confirmation for
	// the replacement, so he is told about the Ticket too.
	env.email.Reset()
	brunoCorrection := correctionBody("bruno@example.com", "Bruno", "Diaz", f.ticketTypeID, 1, "cash", "2026-07-01T10:00:00Z")
	brunoCorrection["send_confirmation"] = true
	correctImportedSaleOK(t, env, f.staffSession, f.eventID, f.brunoSaleID, brunoCorrection)
	assertToldExactly(t, env, map[string]int{"bruno@example.com": 1})
}

// A SINGLE-SALE REVERSAL TELLS THE HOLDER WHO CLICKED AND SPARES THE BUYER.
//
// #350's reversal mails the buyer NOTHING and offers no toggle for it — an
// imported buyer dealt with the Organization's sales rep — so that is the
// buyer's notification policy on this path, and the presumed Holder follows it.
// The Holder who clicked an Assignment Link is told, as they are everywhere.
func TestASingleSaleReversalSparesTheSelfHeldImportBuyerAndTellsTheHolderWhoClicked(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	selfHeldAndLinkAccepted(t, env, f)

	reverseImportedSaleOK(t, env, f.staffSession, f.eventID, f.anaSaleID)

	assertToldExactly(t, env, map[string]int{
		"ana@example.com":   0,
		"carla@example.com": 1,
	})
}

// AN OPERATOR REVERSAL TELLS EVERYBODY: THE BUYER HOLDING THEIR OWN TICKET AND
// THE HOLDER WHO CLICKED.
//
// AN OPERATOR REVERSAL ONLY EVER REACHES AN ONLINE SALE — it records money that
// went back through the platform, and it refuses an imported Sale outright with
// SALE_NOT_REVERSIBLE — so this path is `online` by construction and is here as
// the fourth of ADR 0055's unconditional cases. It mails the buyer the Sale
// Voided notice always and offers no toggle, so the buyer's notification policy
// on it is "they are being written to" and BOTH kinds of Holder are told.
func TestAnOperatorReversalTellsTheOnlineBuyersOwnTicketHolderAndTheHolderWhoClicked(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Operator Fest", "operator-fest", 2000, 20)

	// Two Tickets: Ana holds the first by paying (ADR 0048), and hands the second
	// to Carla, who clicks.
	begun := beginCheckoutOK(t, env, testOrgSlug, "operator-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	ticketIDs := ticketIDsOfSale(t, env, saleID)
	if len(ticketIDs) != 2 {
		t.Fatalf("the Sale has %d Tickets, want 2", len(ticketIDs))
	}
	ana := customerSignIn(t, env, "ana@example.com")
	assignTicketOK(t, env, ana, saleID, ticketIDs[1], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	// The money memo the endpoint insists on for a Sale that collected money. What
	// it records is #348's subject, not this test's.
	operatorReverseOK(t, env, operatorSessionID, settled.ConfirmationRef, operatorReversalBody{
		RefundedAmountCents: intPtr(4460),
		PlatformFeeKept:     boolPtr(false),
	})

	assertToldExactly(t, env, map[string]int{
		"ana@example.com":   1,
		"carla@example.com": 1,
	})
}

// AN ONLINE SALE'S BUYER HOLDING THEIR OWN TICKET IS TOLD WHEN THEY UNDO IT,
// EXACTLY AS BEFORE.
//
// ADR 0055 changes nothing on `online`, and this is the test that says so. An
// Online Sale's buyer holds Ticket 1 by PAYING (ADR 0048) and every route that
// reverses one writes to them anyway, so the policy this ticket introduces
// resolves to "tell them" on that channel and the behaviour is untouched. A
// build that read the rule as "an accepted Holder who is the buyer is never
// told" would fail here.
func TestAnOnlineBuyerHoldingTheirOwnTicketIsStillToldWhenTheyUndoTheSale(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Online Fest", "online-fest", 2000, 20)
	ref := buyOnline(t, env, "online-fest", gaID, "ana@example.com")
	env.email.Reset()

	undoOwnSale(t, env, "ana@example.com", ref)

	assertToldExactly(t, env, map[string]int{"ana@example.com": 1})
}
