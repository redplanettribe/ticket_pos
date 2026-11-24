package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// What the compact Event row kinds carry of their Event (#878, spec #870):
// the Answer Reminder, the Assignment Reminder, Sale Voided, Sale Reversal
// Refused and No Longer Holding. Each is driven through the path that really
// sends it, and the captured message is read for the Organization's name, the
// rendition URLs when the renditions exist, and empty URLs when they are null.

// mailRenditions is one Event's recorded renditions, written straight to the
// rows as the rendering or the fill leaves them.
type mailRenditions struct {
	wide, square, logo string
}

// recordMailRenditions records renditions for the Event and its Organization.
func recordMailRenditions(t *testing.T, eventID string) mailRenditions {
	t.Helper()
	ctx := context.Background()
	var orgID string
	if err := sharedDB.QueryRowContext(ctx, `SELECT organization_id FROM events WHERE id = $1`, eventID).Scan(&orgID); err != nil {
		t.Fatalf("read the Event's Organization: %v", err)
	}
	r := mailRenditions{
		wide:   "email-wide/" + orgID + "/" + eventID + "/wide.jpg",
		square: "email-square/" + orgID + "/" + eventID + "/square.jpg",
		logo:   "email-logo/" + orgID + "/logo.png",
	}
	if _, err := sharedDB.ExecContext(ctx, `UPDATE events SET email_wide_key = $2, email_square_key = $3 WHERE id = $1`,
		eventID, r.wide, r.square); err != nil {
		t.Fatalf("record the Event's renditions: %v", err)
	}
	if _, err := sharedDB.ExecContext(ctx, `UPDATE organizations SET email_logo_key = $2 WHERE id = $1`, orgID, r.logo); err != nil {
		t.Fatalf("record the Logo's rendition: %v", err)
	}
	return r
}

// eventOrganizationName is the name the mail must carry.
func eventOrganizationName(t *testing.T, eventID string) string {
	t.Helper()
	var name string
	if err := sharedDB.QueryRowContext(context.Background(), `
		SELECT o.name FROM events e JOIN organizations o ON o.id = e.organization_id WHERE e.id = $1
	`, eventID).Scan(&name); err != nil {
		t.Fatalf("read the Organization's name: %v", err)
	}
	return name
}

// assertMailEvent holds a captured message's MailEvent to the rows: the
// Organization's name always, and each URL the public URL of its rendition,
// or empty when none is recorded (rendered nil).
func assertMailEvent(t *testing.T, got platform.MailEvent, eventID string, rendered *mailRenditions) {
	t.Helper()
	if want := eventOrganizationName(t, eventID); got.OrganizationName != want {
		t.Errorf("OrganizationName = %q, want %q", got.OrganizationName, want)
	}
	want := map[string]string{"wide": "", "square": "", "logo": ""}
	if rendered != nil {
		want = map[string]string{
			"wide":   sharedStorage.PublicURL(rendered.wide),
			"square": sharedStorage.PublicURL(rendered.square),
			"logo":   sharedStorage.PublicURL(rendered.logo),
		}
	}
	for name, url := range map[string]string{"wide": got.WideURL, "square": got.SquareURL, "logo": got.LogoURL} {
		if url != want[name] {
			t.Errorf("%s URL = %q, want %q", name, url, want[name])
		}
	}
	if got.StartsAt.IsZero() || got.Timezone == "" {
		t.Errorf("the Event row has no date: starts %v in %q", got.StartsAt, got.Timezone)
	}
}

// renditionCases runs a test once with the renditions recorded and once with
// them null, as every Event saved before the fill has them.
func renditionCases(t *testing.T, run func(t *testing.T, env *testEnv, record func(eventID string) *mailRenditions)) {
	for _, tc := range []struct {
		name   string
		record bool
	}{
		{"rendered", true},
		{"not rendered", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			run(t, env, func(eventID string) *mailRenditions {
				if !tc.record {
					return nil
				}
				r := recordMailRenditions(t, eventID)
				return &r
			})
		})
	}
}

func TestTheAnswerReminderCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		f := newReminderFixture(t, env)
		rendered := record(f.eventID)
		if result := sweepAnswerReminders(t, env); result.Sent != 1 {
			t.Fatalf("sweep = %+v, want one mail sent", result)
		}
		sent := reminderFor(t, "ana@example.com")
		assertMailEvent(t, sent.Tickets[0].Event, f.eventID, rendered)
	})
}

func TestTheAssignmentReminderCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		f := newAssignmentReminderFixture(t, env)
		rendered := record(f.eventID)
		if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
			t.Fatalf("sweep = %+v, want one mail sent", result)
		}
		assertMailEvent(t, assignmentReminderTo(t, "ana@example.com").Event, f.eventID, rendered)
	})
}

// The buyer's own undo of a free Online Sale sends the void notice.
func TestSaleVoidedCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, freeID := publishFreeEvent(t, env, sessionID, "Row Fest", "row-fest", 10)
		rendered := record(eventID)
		ref := claimFree(t, env, "row-fest", freeID, "ana@example.com", 1)
		env.email.Reset()

		token := customerSignIn(t, env, "ana@example.com")
		reverseSaleOK(t, env, token, saleByRef(t, readCustomerArea(t, env, token, ""), ref).ID)

		voided := env.email.Voided()
		if len(voided) != 1 {
			t.Fatalf("captured %d void notices, want 1", len(voided))
		}
		assertMailEvent(t, voided[0].Event, eventID, rendered)
	})
}

// A Sale Import undo voids a whole batch, and tells the buyers about the one
// Event the batch belongs to; the Holders on it are told too.
func TestABatchUndoCarriesTheEventRowToBuyersAndHolders(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		f := newAssignmentFixture(t, env)
		rendered := record(f.eventID)
		assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
		acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

		resp, body := env.post(t, "/api/v1/staff/events/"+f.eventID+"/sale-imports/"+f.anaBatchID+"/undo",
			map[string]any{"notify_buyers": true}, authHeader(f.staffSession))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("undo batch status=%d error=%+v", resp.StatusCode, body.Error)
		}

		assertMailEvent(t, theOneNoLongerHoldingMailTo(t, env, "carla@example.com").Event, f.eventID, rendered)
		voided := env.email.Voided()
		if len(voided) == 0 {
			t.Fatal("the undo told no buyer")
		}
		for _, notice := range voided {
			assertMailEvent(t, notice.Event, f.eventID, rendered)
		}
	})
}

// A reassignment tells the Holder it displaced, through the catalog's own
// path.
func TestNoLongerHoldingAfterAReassignmentCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		f := newAssignmentFixture(t, env)
		rendered := record(f.eventID)
		ticketID := f.anaTicketIDs[0]
		assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
		acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

		assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "elena@example.com")

		assertMailEvent(t, theOneNoLongerHoldingMailTo(t, env, "carla@example.com").Event, f.eventID, rendered)
	})
}

// The refund the buyer was told was being processed is refused (the path of
// TestARefundThatWasPendingAndIsRefusedTellsTheCustomer).
func TestSaleReversalRefusedCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, gaID := publishEventStarting(t, env, sessionID, "Row Refused Fest", "row-refused-fest",
			env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
		rendered := record(eventID)

		token, _, saleID := buyThenReadOwnSale(t, "row-refused-fest", gaID, "ana@example.com", 1)
		env.email.Reset()
		payphoneStub.reset()
		t.Cleanup(payphoneStub.reset)

		payphoneStub.reverseHangsUp()
		reverseSalePending(t, payphoneEnv, token, saleID)
		atReversalClock(t, fixedClock.Add(time.Minute))
		payphoneStub.reverseRefuses(http.StatusBadRequest, "El reverso no se puede ejecutar", 42)
		readCustomerArea(t, payphoneEnv, token, "")

		assertMailEvent(t, refusedNotice(t, env).Event, eventID, rendered)
	})
}

// A Platform Operator's reversal sends the same void notice from its own path.
func TestAnOperatorReversalsVoidNoticeCarriesItsEventRow(t *testing.T) {
	renditionCases(t, func(t *testing.T, env *testEnv, record func(string) *mailRenditions) {
		sessionID := orgAdminSession(t, env)
		eventID, freeID := publishFreeEvent(t, env, sessionID, "Operator Row Fest", "operator-row-fest", 10)
		rendered := record(eventID)
		ref := claimFree(t, payphoneEnv, "operator-row-fest", freeID, "ana@example.com", 1)
		operatorSessionID := operatorSession(t, env, "operator@example.com")
		env.email.Reset()

		operatorReverseOK(t, payphoneEnv, operatorSessionID, ref, operatorReversalBody{Note: strPtr("row")})

		voided := env.email.Voided()
		if len(voided) != 1 {
			t.Fatalf("captured %d void notices, want 1", len(voided))
		}
		assertMailEvent(t, voided[0].Event, eventID, rendered)
	})
}
