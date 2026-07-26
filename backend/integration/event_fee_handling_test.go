package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Fee Handling on the Event (issue #90): the per-Event switch that decides
// whether the buyer price is raised to cover the Platform Fee and its Fee IVA
// (ADR 0014). This ticket lands the switch and the rates it is read with;
// nothing a Customer sees moves yet.

type eventFeeView struct {
	FeeHandling       string `json:"fee_handling"`
	FeeBasisPoints    int    `json:"fee_basis_points"`
	FeeIVABasisPoints int    `json:"fee_iva_basis_points"`
}

func getEventFees(t *testing.T, env *testEnv, sessionID, eventID string) eventFeeView {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var view eventFeeView
	if err := json.Unmarshal(body.Data, &view); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return view
}

// patchEventFeeHandling sends the event form's payload with a fee_handling
// value (nil omits the field, as a caller that does not know it would).
func patchEventFeeHandling(t *testing.T, env *testEnv, sessionID, eventID string, feeHandling *string) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{
		"name":      "Fee Event",
		"slug":      "fee-event",
		"starts_at": env.fixedClock.Add(72 * time.Hour).Format(time.RFC3339),
		"timezone":  "America/Guayaquil",
	}
	if feeHandling != nil {
		body["fee_handling"] = *feeHandling
	}
	return env.patch(t, "/api/v1/staff/events/"+eventID, body, authHeader(sessionID))
}

func TestEventFeeHandlingDefaultsToPassOnAndCarriesTheConfiguredRates(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Fee Event", "fee-event")

	fees := getEventFees(t, env, sessionID, eventID)
	if fees.FeeHandling != "pass_on" {
		t.Fatalf("new event fee_handling = %q; want pass_on", fees.FeeHandling)
	}
	// The rates in force travel with the Event so the staff forms can derive
	// buyer and take-home figures with the same arithmetic checkout uses.
	if fees.FeeBasisPoints != 1000 || fees.FeeIVABasisPoints != 1500 {
		t.Fatalf("event rates = %d/%d bps; want the launch 1000/1500", fees.FeeBasisPoints, fees.FeeIVABasisPoints)
	}
}

func TestEventFeeHandlingIsEditableAndSurvivesFormsThatOmitIt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Fee Event", "fee-event")

	absorb := "absorb"
	resp, body := patchEventFeeHandling(t, env, sessionID, eventID, &absorb)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch fee_handling status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var updated eventFeeView
	if err := json.Unmarshal(body.Data, &updated); err != nil {
		t.Fatalf("decode updated event: %v", err)
	}
	if updated.FeeHandling != "absorb" {
		t.Fatalf("patched fee_handling = %q; want absorb", updated.FeeHandling)
	}
	if got := getEventFees(t, env, sessionID, eventID).FeeHandling; got != "absorb" {
		t.Fatalf("persisted fee_handling = %q; want absorb", got)
	}

	// An update that says nothing about Fee Handling leaves it alone rather than
	// silently reverting the Event to the default.
	resp, body = patchEventFeeHandling(t, env, sessionID, eventID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch without fee_handling status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := getEventFees(t, env, sessionID, eventID).FeeHandling; got != "absorb" {
		t.Fatalf("fee_handling after an omitting update = %q; want absorb", got)
	}

	// And back again: the switch is flippable at any time.
	passOn := "pass_on"
	if resp, body = patchEventFeeHandling(t, env, sessionID, eventID, &passOn); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch back to pass_on status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := getEventFees(t, env, sessionID, eventID).FeeHandling; got != "pass_on" {
		t.Fatalf("persisted fee_handling = %q; want pass_on", got)
	}
}

func TestEventFeeHandlingRejectsUnknownMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Fee Event", "fee-event")

	nonsense := "customer_pays_everything"
	resp, body := patchEventFeeHandling(t, env, sessionID, eventID, &nonsense)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch invalid fee_handling status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := getEventFees(t, env, sessionID, eventID).FeeHandling; got != "pass_on" {
		t.Fatalf("fee_handling after a rejected update = %q; want the untouched pass_on", got)
	}
}
