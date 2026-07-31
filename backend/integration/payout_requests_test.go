package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Payout Request: an Org Admin asking to be paid (#175, ADR 0026).
//
// A request moves nothing and counts for nothing. It is a queue OVER the ledger,
// never a second one, so the assertions below are about a record and its
// lifecycle — and one of them is about the money figures NOT changing, which is
// the property the whole design rests on.
//
// Everything here goes over real HTTP against a real Postgres, because the two
// rules that matter most are enforced in two different places and only this seam
// sees both: the cap lives in the service, and "one outstanding request per
// Organization" lives in a partial unique index the service cannot be trusted to
// be the only writer of. TestPayoutRequestOnePendingIsEnforcedByTheDatabase
// therefore goes around the API entirely and writes the second row itself.

const payoutRequestsPath = "/api/v1/staff/organization/payout-requests"

// payoutRequestProfile is the snapshot the request carries: the six Payout
// Profile fields as they stood when the ask was made. It is a copy and not a
// pointer at the profile, which is what lets a later edit leave it alone.
type payoutRequestProfile struct {
	BankName          string `json:"bank_name"`
	AccountType       string `json:"account_type"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
	TaxIDType         string `json:"tax_id_type"`
	TaxIDNumber       string `json:"tax_id_number"`
}

type payoutRequest struct {
	ID          string  `json:"id"`
	AmountCents int     `json:"amount_cents"`
	Note        *string `json:"note"`
	Status      string  `json:"status"`
	// RequestedBy is an email, exactly as a Payout's recorded_by is, so the
	// record outlives the Member who made it.
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
	// PayableBalanceCents is what the Organization could have asked for at the
	// moment it asked — the figure that later tells "asked for all of it" from
	// "asked for four times what they have".
	PayableBalanceCents int                  `json:"payable_balance_cents"`
	PayoutProfile       payoutRequestProfile `json:"payout_profile"`
	ResolutionReason    *string              `json:"resolution_reason"`
	ResolvedAt          *string              `json:"resolved_at"`
	ResolvedBy          *string              `json:"resolved_by"`
	PayoutID            *string              `json:"payout_id"`
	// TransferSubmittedAt is when the transfer was sent, and it is the date the
	// organizer's "up to 48 hours" sentence is built on (#187).
	TransferSubmittedAt *string `json:"transfer_submitted_at"`
}

// rawPayoutRequestFields decodes one request from the Organization's own history
// as a bare map, which is the only way to assert a field is ABSENT: a typed
// struct silently tolerates whatever it was not taught about, so the fields this
// surface deliberately does not carry can only be tested for by looking at the
// JSON itself.
func rawPayoutRequestFields(t *testing.T, env *testEnv, sessionID, requestID string) map[string]any {
	t.Helper()
	resp, body := env.get(t, payoutRequestsPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list payout requests status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var requests []map[string]any
	if err := json.Unmarshal(body.Data, &requests); err != nil {
		t.Fatalf("decode payout requests as raw objects: %v", err)
	}
	for _, request := range requests {
		if id, _ := request["id"].(string); id == requestID {
			return request
		}
	}
	t.Fatalf("request %q is not in the organization's own history", requestID)
	return nil
}

// requestBody is a Payout Request as the form submits one: an amount, an
// optional note, and the bank details the organizer sees pre-filled and may
// correct in place.
func requestBody(amountCents int, note string) map[string]any {
	body := map[string]any{
		"amount_cents":   amountCents,
		"payout_profile": completeProfile(),
	}
	if note != "" {
		body["note"] = note
	}
	return body
}

func submitPayoutRequest(t *testing.T, env *testEnv, sessionID string, body map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, payoutRequestsPath, body, authHeader(sessionID))
}

// submitPayoutRequestOK submits and insists the platform accepted the ask,
// returning the request and whether it was newly created (201) or the
// outstanding one handed back (200).
func submitPayoutRequestOK(t *testing.T, env *testEnv, sessionID string, body map[string]any) (payoutRequest, bool) {
	t.Helper()
	resp, envBody := submitPayoutRequest(t, env, sessionID, body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("submit payout request status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	var request payoutRequest
	if err := json.Unmarshal(envBody.Data, &request); err != nil {
		t.Fatalf("decode payout request: %v", err)
	}
	return request, resp.StatusCode == http.StatusCreated
}

func listPayoutRequests(t *testing.T, env *testEnv, sessionID string) []payoutRequest {
	t.Helper()
	resp, body := env.get(t, payoutRequestsPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list payout requests status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var requests []payoutRequest
	if err := json.Unmarshal(body.Data, &requests); err != nil {
		t.Fatalf("decode payout requests: %v", err)
	}
	return requests
}

func cancelPayoutRequest(t *testing.T, env *testEnv, sessionID, requestID string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, payoutRequestsPath+"/"+requestID+"/cancel", nil, authHeader(sessionID))
}

// clearedSale sells `quantity` tickets and moves the sales clock past Ecuadorian
// midnight, so the Net Proceeds have cleared and are payable. It returns what
// the Payable Balance becomes.
//
// The clock move is the whole of the fixture: the Payable Balance counts sales
// recorded BEFORE today in Ecuador, so there is no way to have a payable balance
// without changing what today is (ADR 0026).
func clearedSale(t *testing.T, env *testEnv, sessionID, eventName, eventSlug string, quantity int) int {
	t.Helper()
	_, gaID := publishCheckoutEvent(t, env, sessionID, eventName, eventSlug, feeTestBaseCents, quantity+10)
	begin := beginCheckoutOK(t, env, "test-org", eventSlug,
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": quantity}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	salesClockAt(ecuadorMidnight(t, 1))
	return quantity * feeTestBaseCents
}

// TestPayoutRequestSubmitsAndAppearsInHistory is the ordinary life of the
// record: an Org Admin states an amount and a note, and finds the ask waiting in
// their own history with everything the platform recorded about it.
func TestPayoutRequestSubmitsAndAppearsInHistory(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Request Fest", "request-fest", 5)

	// Nothing asked for yet: an empty history, which is not an error.
	if existing := listPayoutRequests(t, env, sessionID); len(existing) != 0 {
		t.Fatalf("history on a brand-new Organization = %+v; want none", existing)
	}

	request, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable-100, "for the June shows"))
	if !created {
		t.Fatalf("the first request was answered as an existing one")
	}
	if request.Status != "pending" {
		t.Fatalf("status = %q; want pending", request.Status)
	}
	if request.AmountCents != payable-100 {
		t.Fatalf("amount = %d; want %d", request.AmountCents, payable-100)
	}
	if request.Note == nil || *request.Note != "for the June shows" {
		t.Fatalf("note = %v; want the one that was typed", request.Note)
	}
	// The asker is an email and not a member id, so the record outlives the
	// Membership exactly as payouts.recorded_by does (ADR 0026).
	if request.RequestedBy != "admin@example.com" {
		t.Fatalf("requested_by = %q; want the acting Member's email", request.RequestedBy)
	}
	// The Payable Balance as it stood, snapshotted so an operator can later tell
	// "asked for all of it" from "asked for four times what they have".
	if request.PayableBalanceCents != payable {
		t.Fatalf("payable_balance_cents = %d; want the balance at the moment of asking, %d",
			request.PayableBalanceCents, payable)
	}
	// Nothing has been answered, so every resolution field is empty.
	if request.ResolvedAt != nil || request.ResolvedBy != nil || request.ResolutionReason != nil || request.PayoutID != nil {
		t.Fatalf("a pending request carries a resolution: %+v", request)
	}

	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 1 || history[0].ID != request.ID {
		t.Fatalf("history = %+v; want exactly the request just made", history)
	}

	// A note is optional, which the column has to allow rather than store as "".
	cancelled, _ := cancelPayoutRequest(t, env, sessionID, request.ID)
	if cancelled.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", cancelled.StatusCode)
	}
	noteless, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(1, ""))
	if noteless.Note != nil {
		t.Fatalf("note = %v on a request that carried none; want null", *noteless.Note)
	}
}

// TestPayoutRequestIsCappedByThePayableBalance: exactly the Payable Balance is
// accepted, one cent above is refused, and the cap is never looked at again.
//
// The last part is the one an implementation drifts on. The Payable Balance
// moves after the ask — a later Payout takes it straight down — and the request
// must sit there unchanged, because the operator standing at the bank is the
// party who decides what to do about that (ADR 0026).
func TestPayoutRequestIsCappedByThePayableBalance(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Capped Fest", "capped-fest", 4)

	// One cent above what has cleared, refused before anything is written.
	resp, body := submitPayoutRequest(t, env, sessionID, requestBody(payable+1, ""))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("over-balance status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE" {
		t.Fatalf("over-balance error = %+v; want PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE", body.Error)
	}
	if requests := listPayoutRequests(t, env, sessionID); len(requests) != 0 {
		t.Fatalf("a refused request was recorded: %+v", requests)
	}

	// Exactly the Payable Balance is a legitimate ask: the whole of what has
	// cleared is the whole of what may be asked for.
	request, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))
	if !created || request.AmountCents != payable {
		t.Fatalf("request for the exact balance = %+v (created=%v)", request, created)
	}

	// The operator settles somewhere else entirely, and the Payable Balance
	// collapses. The request is untouched: request-time validation is a
	// guardrail, not an invariant (ADR 0026).
	recordPayout(t, env, "test-org", payable, "2026-07-08", "settled another way")
	salesClockAt(ecuadorMidnight(t, 1))
	if summary := getPayouts(t, env, sessionID); summary.PayableBalanceCents != 0 {
		t.Fatalf("payable balance after the settlement = %d; want 0", summary.PayableBalanceCents)
	}
	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 1 || history[0].Status != "pending" || history[0].AmountCents != payable {
		t.Fatalf("history after the balance moved = %+v; want the request exactly as it was", history)
	}
	if history[0].PayableBalanceCents != payable {
		t.Fatalf("snapshotted payable balance = %d; want the figure at the moment of asking, %d",
			history[0].PayableBalanceCents, payable)
	}
}

// TestPayoutRequestRefusesAZeroOrNegativeAmount: an ask has to be for something.
func TestPayoutRequestRefusesAZeroOrNegativeAmount(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	clearedSale(t, env, sessionID, "Zero Fest", "zero-fest", 4)

	for _, amount := range []int{0, -1} {
		resp, body := submitPayoutRequest(t, env, sessionID, requestBody(amount, ""))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("amount %d status=%d; want 400", amount, resp.StatusCode)
		}
		fields := fieldErrorsByName(t, body)
		if _, ok := fields["amount_cents"]; !ok {
			t.Fatalf("amount %d: fields=%+v; want an error on amount_cents", amount, fields)
		}
	}
}

// TestPayoutRequestNeedsACompletePayoutProfile: a request an operator cannot
// action is the support thread this feature exists to remove, so the endpoint
// refuses without somewhere to pay — naming every field that is missing rather
// than saying "no profile" and leaving the organizer to guess.
//
// The refusal reuses the profile's own validator: the field names and codes here
// are exactly the ones PUT /payout-profile returns, because they are the same
// fields and the form shows them in the same place.
func TestPayoutRequestNeedsACompletePayoutProfile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Profileless Fest", "profileless-fest", 4)

	// No profile stored and none supplied: every field is named.
	resp, body := submitPayoutRequest(t, env, sessionID, map[string]any{"amount_cents": payable})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("profileless status=%d error=%+v; want 400", resp.StatusCode, body.Error)
	}
	fields := fieldErrorsByName(t, body)
	// Every field the profile's own validator names when handed nothing. The Tax
	// ID is named once, on its type: an empty type is what
	// platform.BeneficiaryTaxIDFieldErrors complains about, and it says nothing
	// further about a number it cannot yet know the rules for. Restating that
	// judgement here would be a second definition of what a complete profile is.
	for _, field := range []string{"bank_name", "account_type", "account_number", "account_holder_name", "tax_id_type"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("fields=%+v; want %s named as missing", fields, field)
		}
	}

	// A profile supplied on the request but wrong is refused the same way, and
	// the refusal never echoes what was typed (ADR 0026).
	broken := requestBody(payable, "")
	profile := completeProfile()
	profile["account_number"] = "22012A4821"
	broken["payout_profile"] = profile
	resp, body = submitPayoutRequest(t, env, sessionID, broken)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad account number status=%d; want 400", resp.StatusCode)
	}
	got := fieldErrorsByName(t, body)["account_number"]
	if got.Code != "INVALID_ACCOUNT_NUMBER" {
		t.Fatalf("account_number error = %+v; want INVALID_ACCOUNT_NUMBER", got)
	}
	if got.Message == "" || strings.Contains(got.Message, "22012A4821") {
		t.Fatalf("message %q echoes the submitted account number", got.Message)
	}

	// Nothing was written by either refusal — no request, and no half-profile.
	if requests := listPayoutRequests(t, env, sessionID); len(requests) != 0 {
		t.Fatalf("a refused request was recorded: %+v", requests)
	}
	if _, exists := getPayoutProfileOK(t, env, sessionID); exists {
		t.Fatalf("a refused request stored a Payout Profile")
	}

	// With a profile already on file, the request needs no bank details at all:
	// the stored profile is what it snapshots.
	if resp, envBody := putPayoutProfile(t, env, sessionID, completeProfile()); resp.StatusCode != http.StatusOK {
		t.Fatalf("save profile status=%d error=%+v", resp.StatusCode, envBody.Error)
	}
	request, created := submitPayoutRequestOK(t, env, sessionID, map[string]any{"amount_cents": payable})
	if !created || request.PayoutProfile.BankName != "Banco Pichincha" {
		t.Fatalf("request against the stored profile = %+v (created=%v)", request, created)
	}
}

// TestPayoutRequestFormIsTheProfileEditor: correcting an account number on a
// request corrects the profile too, because an organizer who fixes it here means
// their account number changed, and having to fix the same typo in two places is
// worse than the alternative (ADR 0026).
func TestPayoutRequestFormIsTheProfileEditor(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Editor Fest", "editor-fest", 4)

	if resp, body := putPayoutProfile(t, env, sessionID, completeProfile()); resp.StatusCode != http.StatusOK {
		t.Fatalf("save profile status=%d error=%+v", resp.StatusCode, body.Error)
	}

	corrected := requestBody(payable, "")
	profile := completeProfile()
	profile["bank_name"] = "Cooperativa JEP"
	profile["account_number"] = " 0004-821 "
	corrected["payout_profile"] = profile

	request, created := submitPayoutRequestOK(t, env, sessionID, corrected)
	if !created {
		t.Fatalf("the corrected request was not created")
	}
	// The correction is normalised on the way in, exactly as the profile editor
	// normalises it, so the request snapshots what will be typed into a bank app.
	if request.PayoutProfile.BankName != "Cooperativa JEP" || request.PayoutProfile.AccountNumber != "0004821" {
		t.Fatalf("snapshot = %+v; want the corrected, normalised details", request.PayoutProfile)
	}

	stored, exists := getPayoutProfileOK(t, env, sessionID)
	if !exists || stored.BankName != "Cooperativa JEP" || stored.AccountNumber != "0004821" {
		t.Fatalf("profile after the request = %+v; want the correction saved there too", stored)
	}
}

// TestPayoutRequestSnapshotSurvivesALaterProfileEdit is the reason the snapshot
// exists. The profile says where to pay today; the snapshot says where an
// operator was told to pay, and no later edit may rewrite it (ADR 0026).
func TestPayoutRequestSnapshotSurvivesALaterProfileEdit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Snapshot Fest", "snapshot-fest", 4)

	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))
	asked := request.PayoutProfile

	// The Organization changes banks entirely.
	moved := completeProfile()
	moved["bank_name"] = "Banco Guayaquil"
	moved["account_type"] = "corriente"
	moved["account_number"] = "9998887776"
	moved["account_holder_name"] = "Ritmo Producciones"
	moved["tax_id_type"] = "ruc"
	moved["tax_id_number"] = naturalRUC
	if resp, body := putPayoutProfile(t, env, sessionID, moved); resp.StatusCode != http.StatusOK {
		t.Fatalf("change banks status=%d error=%+v", resp.StatusCode, body.Error)
	}

	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 1 {
		t.Fatalf("history = %+v; want the one request", history)
	}
	if history[0].PayoutProfile != asked {
		t.Fatalf("snapshot after the profile moved = %+v; want the account it was made against, %+v",
			history[0].PayoutProfile, asked)
	}

	// And the profile really did change, so the assertion above is not passing
	// because nothing happened.
	stored, _ := getPayoutProfileOK(t, env, sessionID)
	if stored.BankName != "Banco Guayaquil" {
		t.Fatalf("profile = %+v; want the new bank", stored)
	}
}

// TestPayoutRequestSecondSubmissionReturnsTheOutstandingOne: an organizer who
// asks twice learns where their earlier ask went, rather than getting a bare
// conflict — the courtesy ADR 0024 established for the Reversal Request.
func TestPayoutRequestSecondSubmissionReturnsTheOutstandingOne(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Twice Fest", "twice-fest", 6)

	first, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "the first ask"))
	if !created {
		t.Fatalf("the first request was not created")
	}

	// A second ask, for a different amount and with a different note. It writes
	// nothing: a pending request cannot be edited, only cancelled and re-asked,
	// which is what keeps "outstanding" genuinely singular.
	resp, body := submitPayoutRequest(t, env, sessionID, requestBody(1, "the second ask"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second submission status=%d error=%+v; want 200 with the existing request", resp.StatusCode, body.Error)
	}
	var second payoutRequest
	if err := json.Unmarshal(body.Data, &second); err != nil {
		t.Fatalf("decode second submission: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second submission returned a different request %q; want the outstanding %q", second.ID, first.ID)
	}
	if second.AmountCents != first.AmountCents || second.Note == nil || *second.Note != "the first ask" {
		t.Fatalf("the outstanding request was edited by the second ask: %+v", second)
	}
	if history := listPayoutRequests(t, env, sessionID); len(history) != 1 {
		t.Fatalf("history = %+v; want exactly one request", history)
	}
}

// TestPayoutRequestSecondSubmissionChangesNoBankDetails: the repeat submission
// writes nothing at all, and "nothing" has to include where the Organization is
// paid.
//
// The ordering rule the request service opens with — a refused ask never quietly
// changes an Organization's bank details — has to hold for every way an ask can
// fail to be recorded, not just for an amount over the Payable Balance. An
// organizer who corrects their account number, presses submit, and is handed
// back the outstanding request would otherwise be looking at a request showing
// one account while their profile had silently become another. The route for
// changing banks mid-ask is the profile editor, which says so.
func TestPayoutRequestSecondSubmissionChangesNoBankDetails(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Rebank Fest", "rebank-fest", 6)

	first, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "the first ask"))
	if !created {
		t.Fatalf("the first request was not created")
	}

	// The same ask again, this time naming a different account entirely.
	elsewhere := completeProfile()
	elsewhere["bank_name"] = "Banco del Pacífico"
	elsewhere["account_number"] = "0099887766"
	second := requestBody(payable, "the second ask")
	second["payout_profile"] = elsewhere

	handedBack, createdAgain := submitPayoutRequestOK(t, env, sessionID, second)
	if createdAgain {
		t.Fatalf("the second submission recorded a new request; want the outstanding one handed back")
	}
	if handedBack.ID != first.ID {
		t.Fatalf("handed back request %q; want the outstanding %q", handedBack.ID, first.ID)
	}
	if handedBack.PayoutProfile.AccountNumber != first.PayoutProfile.AccountNumber {
		t.Fatalf("the outstanding request's snapshot changed to %q; want %q",
			handedBack.PayoutProfile.AccountNumber, first.PayoutProfile.AccountNumber)
	}

	// And the profile itself is untouched: the Organization is still paid where
	// it was before the second press.
	profile, exists := getPayoutProfileOK(t, env, sessionID)
	if !exists {
		t.Fatalf("the Payout Profile vanished")
	}
	if profile.AccountNumber != first.PayoutProfile.AccountNumber || profile.BankName != first.PayoutProfile.BankName {
		t.Fatalf("a repeat submission rewrote the Payout Profile to %s/%s; want %s/%s",
			profile.BankName, profile.AccountNumber,
			first.PayoutProfile.BankName, first.PayoutProfile.AccountNumber)
	}
}

// TestPayoutRequestOnePendingIsEnforcedByTheDatabase goes around the API to
// prove the rule is structural. A service check the application could race past
// is not the rule; the partial unique index is (ADR 0026, in the shape ADR 0024
// already uses).
func TestPayoutRequestOnePendingIsEnforcedByTheDatabase(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Indexed Fest", "indexed-fest", 6)
	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))

	// A second pending row, written directly. The database must refuse it.
	insertSecond := func() error {
		_, err := env.db.Exec(`
			INSERT INTO payout_requests
				(organization_id, amount_cents, status, requested_by, payable_balance_cents,
				 bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
			SELECT organization_id, 100, $1, 'sneaky@example.com', payable_balance_cents,
			       bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number
			FROM payout_requests WHERE id = $2
		`, "pending", request.ID)
		return err
	}
	if err := insertSecond(); err == nil {
		t.Fatal("the database accepted a second pending Payout Request; the partial unique index is missing")
	}

	// The index is PARTIAL, and that is what makes the rule usable: once the
	// outstanding request is out of `pending`, a new one may be written.
	if resp, _ := cancelPayoutRequest(t, env, sessionID, request.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	if err := insertSecond(); err != nil {
		t.Fatalf("the database refused a pending request with none outstanding: %v", err)
	}

	// And the resolved states really are excluded rather than the index simply
	// being absent: two cancelled rows coexist happily.
	if _, err := env.db.Exec(`
		UPDATE payout_requests SET status = 'cancelled', resolved_at = NOW(), resolved_by = 'sneaky@example.com'
		WHERE requested_by = 'sneaky@example.com'
	`); err != nil {
		t.Fatalf("resolve the second request: %v", err)
	}
	var cancelled int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payout_requests WHERE status = 'cancelled'`).Scan(&cancelled); err != nil {
		t.Fatalf("count cancelled: %v", err)
	}
	if cancelled != 2 {
		t.Fatalf("cancelled rows = %d; want both, since the index only binds pending ones", cancelled)
	}
}

// TestPayoutRequestCancelFreesTheOutstandingSlot: a pending request cannot be
// edited, only cancelled and re-asked. Cancelling is final, and cancelling twice
// is refused rather than silently repeated.
func TestPayoutRequestCancelFreesTheOutstandingSlot(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Cancel Fest", "cancel-fest", 6)

	first, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "too much"))

	resp, body := cancelPayoutRequest(t, env, sessionID, first.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var cancelled payoutRequest
	if err := json.Unmarshal(body.Data, &cancelled); err != nil {
		t.Fatalf("decode cancelled request: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.ResolvedAt == nil || cancelled.ResolvedBy == nil {
		t.Fatalf("cancelled request = %+v; want a resolved cancellation", cancelled)
	}
	if *cancelled.ResolvedBy != "admin@example.com" {
		t.Fatalf("resolved_by = %q; want the cancelling Member's email", *cancelled.ResolvedBy)
	}

	// All three end states are final, so a second cancellation is refused rather
	// than treated as a no-op.
	resp, body = cancelPayoutRequest(t, env, sessionID, first.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "PAYOUT_REQUEST_NOT_PENDING" {
		t.Fatalf("second cancel status=%d error=%+v; want 409 PAYOUT_REQUEST_NOT_PENDING", resp.StatusCode, body.Error)
	}

	// And the Organization may ask again, which is the point of cancelling.
	second, created := submitPayoutRequestOK(t, env, sessionID, requestBody(payable-1, "the right amount"))
	if !created || second.ID == first.ID {
		t.Fatalf("re-ask after cancelling = %+v (created=%v)", second, created)
	}
	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 2 {
		t.Fatalf("history = %+v; want both the cancelled ask and the new one", history)
	}
	// Newest first, the house convention for a history (the operator's queue is
	// the one place that inverts it, and it is not this surface).
	if history[0].ID != second.ID {
		t.Fatalf("history order = %+v; want newest first", history)
	}

	// A request belonging to nobody, and a request belonging to somebody else,
	// are both simply not found.
	if resp, _ := cancelPayoutRequest(t, env, sessionID, "a0000000-0000-4000-8000-00000000dead"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cancel of an unknown request status=%d; want 404", resp.StatusCode)
	}
}

// TestPayoutRequestCannotBeEdited: there is no way to change a pending request.
// That is what keeps "outstanding" singular and stops an operator looking at a
// figure that changed under them (ADR 0026).
func TestPayoutRequestCannotBeEdited(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Immutable Fest", "immutable-fest", 6)
	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "as asked"))

	// Read raw rather than through env.doJSON: there is no route here at all, so
	// the answer is the router's own and carries no envelope to decode.
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		req, err := http.NewRequest(method, env.server.URL+payoutRequestsPath+"/"+request.ID,
			strings.NewReader(`{"amount_cents":1}`))
		if err != nil {
			t.Fatalf("new %s request: %v", method, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do %s request: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s on a pending request status=%d; want no edit route at all", method, resp.StatusCode)
		}
	}

	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 1 || history[0].AmountCents != payable || *history[0].Note != "as asked" {
		t.Fatalf("request after the edit attempts = %+v; want it untouched", history)
	}
}

// TestPayoutRequestMovesNoMoney is the load-bearing assertion of the whole
// feature. A request moves nothing and counts for nothing: no balance, no
// aggregate and no platform revenue figure may learn that requests exist
// (ADR 0026). A system with two places money can be said to have moved has no
// answer to which one is true.
func TestPayoutRequestMovesNoMoney(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Inert Fest", "inert-fest", 5)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	orgID := operatorOrgIDBySlug(t, env, "test-org")

	readMoney := func() (payoutsSummary, operatorSummary, operatorOrganizationDetail) {
		t.Helper()
		organization := getPayouts(t, env, sessionID)
		var summary operatorSummary
		operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/summary", &summary)
		var detail operatorOrganizationDetail
		operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/organizations/"+orgID, &detail)
		return organization, summary, detail
	}

	beforeOrg, beforeSummary, beforeDetail := readMoney()

	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, "settle me"))

	afterOrg, afterSummary, afterDetail := readMoney()
	if !sameBalances(beforeOrg, afterOrg) {
		t.Fatalf("the Organization's balances moved: before=%+v after=%+v", beforeOrg, afterOrg)
	}
	if !equalTotals(beforeSummary, afterSummary) {
		t.Fatalf("platform revenue moved: before=%+v after=%+v", beforeSummary.Totals, afterSummary.Totals)
	}
	if afterDetail.WithdrawableBalanceCents != beforeDetail.WithdrawableBalanceCents ||
		afterDetail.PayableBalanceCents != beforeDetail.PayableBalanceCents ||
		len(afterDetail.Payouts) != len(beforeDetail.Payouts) {
		t.Fatalf("the operator's view of the Organization moved: before=%+v after=%+v", beforeDetail, afterDetail)
	}

	// Cancelling moves nothing either — a request that was never money cannot
	// stop being it.
	if resp, _ := cancelPayoutRequest(t, env, sessionID, request.ID); resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", resp.StatusCode)
	}
	cancelledOrg, cancelledSummary, _ := readMoney()
	if !sameBalances(beforeOrg, cancelledOrg) || !equalTotals(beforeSummary, cancelledSummary) {
		t.Fatalf("cancelling a request moved money: %+v / %+v", cancelledOrg, cancelledSummary.Totals)
	}

	// And no Payout was conjured: the payouts table is the ledger, and a request
	// is not an entry in it.
	var payouts int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payouts`).Scan(&payouts); err != nil {
		t.Fatalf("count payouts: %v", err)
	}
	if payouts != 0 {
		t.Fatalf("payout rows = %d; want none — a request records no settlement", payouts)
	}
}

// sameBalances compares the Organization's two money figures and the length of
// its payout history — everything on the summary that a request must not be able
// to move.
func sameBalances(a, b payoutsSummary) bool {
	return a.WithdrawableBalanceCents == b.WithdrawableBalanceCents &&
		a.PayableBalanceCents == b.PayableBalanceCents &&
		a.Currency == b.Currency &&
		len(a.Payouts) == len(b.Payouts)
}

// equalTotals compares the Operator Dashboard's per-currency revenue totals.
func equalTotals(a, b operatorSummary) bool {
	if len(a.Totals) != len(b.Totals) {
		return false
	}
	for i := range a.Totals {
		if a.Totals[i] != b.Totals[i] {
			return false
		}
	}
	return true
}

// TestPayoutRequestIsOrgAdminOnly: asking for the Organization's money to be
// moved to a bank account is the Org Admin's authority and nobody else's.
//
// Integration Partners are excluded too (ADR 0026, #175). There is no partner
// credential in the system yet — V8 — so what this test can pin is the gate that
// will decide it: every one of these routes is behind RequireOrgAdmin, which
// admits the `org_admin` MEMBER ROLE and nothing else. When the partner API
// lands it must not be widened to include these three routes; routes.go says so
// beside them.
func TestPayoutRequestIsOrgAdminOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Gated Fest", "gated-fest", 6)
	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	for _, actor := range []struct {
		role      string
		sessionID string
	}{
		{role: "event_owner", sessionID: addMember("owner@example.com", "event_owner")},
		{role: "event_staff", sessionID: addMember("doorstaff@example.com", "event_staff")},
	} {
		resp, body := env.get(t, payoutRequestsPath, authHeader(actor.sessionID))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s list status=%d error=%+v; want 403 FORBIDDEN", actor.role, resp.StatusCode, body.Error)
		}
		resp, body = submitPayoutRequest(t, env, actor.sessionID, requestBody(1, ""))
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s submit status=%d error=%+v; want 403 FORBIDDEN", actor.role, resp.StatusCode, body.Error)
		}
		resp, body = cancelPayoutRequest(t, env, actor.sessionID, request.ID)
		if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
			t.Fatalf("%s cancel status=%d error=%+v; want 403 FORBIDDEN", actor.role, resp.StatusCode, body.Error)
		}
	}

	// A stranger with no Membership at all, and nobody at all.
	strangerSessionID := verifyOTP(t, env, "stranger@example.com")
	if resp, _ := env.get(t, payoutRequestsPath, authHeader(strangerSessionID)); resp.StatusCode == http.StatusOK {
		t.Fatalf("a non-member read another Organization's Payout Requests")
	}
	if resp, _ := env.get(t, payoutRequestsPath, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list status=%d; want 401", resp.StatusCode)
	}
	if resp, _ := env.post(t, payoutRequestsPath, requestBody(1, ""), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous submit status=%d; want 401", resp.StatusCode)
	}
}

// TestPayoutRequestIsScopedToTheActingOrganization: one Organization's ask is
// never another's, and neither is its bank account.
func TestPayoutRequestIsScopedToTheActingOrganization(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Scoped Fest", "scoped-fest", 6)
	mine, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))

	// The pre-seeded demo Organization has an outstanding ask of its own, which
	// must neither appear here nor block this one.
	if _, err := env.db.Exec(`
		INSERT INTO payout_requests
			(organization_id, amount_cents, status, requested_by, payable_balance_cents,
			 bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		SELECT id, 5000, 'pending', 'preseeded@example.com', 5000,
		       'Banco Guayaquil', 'corriente', '0009999', 'Demo Venue', 'cedula', $1
		FROM organizations WHERE slug = 'demo-venue'
	`, otherCedula); err != nil {
		t.Fatalf("seed the other Organization's request: %v", err)
	}

	history := listPayoutRequests(t, env, sessionID)
	if len(history) != 1 || history[0].ID != mine.ID {
		t.Fatalf("history = %+v; want only this Organization's own request", history)
	}

	// And cancelling by id cannot reach across Organizations.
	var othersID string
	if err := env.db.QueryRow(`
		SELECT r.id FROM payout_requests r
		JOIN organizations o ON o.id = r.organization_id WHERE o.slug = 'demo-venue'
	`).Scan(&othersID); err != nil {
		t.Fatalf("read the other request: %v", err)
	}
	if resp, _ := cancelPayoutRequest(t, env, sessionID, othersID); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cancelling another Organization's request status=%d; want 404", resp.StatusCode)
	}
	var othersStatus string
	if err := env.db.QueryRow(`SELECT status FROM payout_requests WHERE id = $1`, othersID).Scan(&othersStatus); err != nil {
		t.Fatalf("re-read the other request: %v", err)
	}
	if othersStatus != "pending" {
		t.Fatalf("the other Organization's request = %q; want it untouched", othersStatus)
	}
}

// TestPayoutRequestRecordsTheAskingInstant: `requested_at` is when the ask was
// made, and the history is ordered by it.
func TestPayoutRequestRecordsTheAskingInstant(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	payable := clearedSale(t, env, sessionID, "Timed Fest", "timed-fest", 6)

	request, _ := submitPayoutRequestOK(t, env, sessionID, requestBody(payable, ""))
	at, err := time.Parse(time.RFC3339, request.RequestedAt)
	if err != nil {
		t.Fatalf("requested_at %q is not an instant: %v", request.RequestedAt, err)
	}
	if at.IsZero() {
		t.Fatalf("requested_at = %v; want the moment of asking", at)
	}
}

// A processing request holds the Organization's single slot, and neither party
// may take it back (#184, ADR 0026 amendment).
//
// These two tests are the organizer's half of the widened partial unique index.
// Without it an Organization whose transfer has been submitted could ask again for the
// same money — the first request is no longer `pending`, and NO BALANCE HAS
// MOVED to stop them, because a request moves nothing and counts for nothing.
// The index is the only thing standing there.

// TestPayoutRequestProcessingStillOccupiesTheOutstandingSlot: an Organization
// whose transfer is submitted but unconfirmed cannot ask again, and is handed its existing request
// back exactly as a pending one does.
//
// The same courtesy, deliberately: an organizer pressing submit while their
// money is on its way has not asked twice, and telling them "conflict" would
// leave them wondering where their earlier ask went. The raw INSERT at the end
// is what proves the rule is the DATABASE's rather than a service check the
// application could race past.
func TestPayoutRequestProcessingStillOccupiesTheOutstandingSlot(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Slot Fest", "slot-fest", 6, 1)
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{"transfer_reference": "PP-SLOT"})

	// The second ask, for a different amount and a different note. It writes
	// nothing and edits nothing.
	resp, body := submitPayoutRequest(t, env, adminSessionID, requestBody(1, "asking again while the transfer is unconfirmed"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second submission while processing status=%d error=%+v; want 200 with the existing request",
			resp.StatusCode, body.Error)
	}
	var second payoutRequest
	if err := json.Unmarshal(body.Data, &second); err != nil {
		t.Fatalf("decode second submission: %v", err)
	}
	if second.ID != request.ID || second.Status != "processing" {
		t.Fatalf("second submission returned %+v; want the outstanding processing request %q", second, request.ID)
	}
	if second.AmountCents != payable {
		t.Fatalf("the outstanding request was edited by the second ask: %+v", second)
	}
	if history := listPayoutRequests(t, env, adminSessionID); len(history) != 1 {
		t.Fatalf("history = %+v; want exactly one request", history)
	}

	// And structurally: the partial unique index covers `processing`, so even a
	// writer going around the API cannot open a second slot.
	_, err := env.db.Exec(`
		INSERT INTO payout_requests
			(organization_id, amount_cents, status, requested_by, payable_balance_cents,
			 bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		SELECT organization_id, 100, 'pending', 'sneaky@example.com', payable_balance_cents,
		       bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number
		FROM payout_requests WHERE id = $1
	`, request.ID)
	if err == nil {
		t.Fatal("the database accepted a pending request beside a processing one; the partial unique index was not widened")
	}
}

// TestPayoutRequestCannotBeCancelledOnceTheTransferIsSubmitted: cancelling would
// withdraw an ask that is thirty seconds from landing, leaving a confirmed
// transfer with nothing to attach it to and the Organization free to ask again
// for money already on its way to them (ADR 0026 amendment).
//
// The refusal must say THAT, not restate a status. "Already resolved" is false —
// nothing has been resolved — and an organizer who reads it will believe their
// money is not coming.
func TestPayoutRequestCannotBeCancelledOnceTheTransferIsSubmitted(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Nocancel Fest", "nocancel-fest", 6, 1)
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	markProcessingOK(t, env, operatorSessionID, request.ID, nil)

	resp, body := cancelPayoutRequest(t, env, adminSessionID, request.ID)
	if resp.StatusCode != http.StatusConflict || body.Error == nil ||
		body.Error.Code != "PAYOUT_REQUEST_NOT_PENDING" {
		t.Fatalf("cancelling a processing request status=%d error=%+v; want 409 PAYOUT_REQUEST_NOT_PENDING",
			resp.StatusCode, body.Error)
	}
	message := strings.ToLower(body.Error.Message)
	if !strings.Contains(message, "being processed") {
		t.Fatalf("refusal message = %q; want it to say the transfer is already being processed", body.Error.Message)
	}
	if strings.Contains(message, "already been resolved") {
		t.Fatalf("refusal message = %q; a request awaiting the bank has not been resolved", body.Error.Message)
	}

	// The ask is exactly where it was, and no money has moved either way.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "processing" {
		t.Fatalf("request after a refused cancellation = %+v; want it still processing", history)
	}
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows = %d; want none — nothing has been confirmed", count)
	}
}

// TestFailedPayoutRequestFreesTheOrganizationToAskAgain is the organizer's whole
// route out of a failure, and it is a FRESH ASK rather than a retry (#185,
// ADR 0026 amendment).
//
// A failed request is terminal and cannot be reopened: the bank details it
// carries are a frozen snapshot, so retrying it would aim at the same rejected
// account forever. The organizer corrects their Payout Profile and submits a new
// request — which they may do IMMEDIATELY, because the partial unique index
// counts only `('pending', 'processing')` and a failed request is neither.
//
// This is the widened index read from the other side. A future "simplification"
// that added `failed` to the outstanding predicate — reasoning that a request
// with no Payout behind it is somehow still open — would lock an organizer out
// of the money they are owed permanently, and this is the test that would fail.
//
// The 201 is load-bearing: it says a NEW request was created. Handing back the
// failed one, the way a still-outstanding request is handed back, would tell an
// organizer their correction had done nothing.
func TestFailedPayoutRequestFreesTheOrganizationToAskAgain(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	first, payable := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Again Fest", "again-fest", 6, 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	markProcessingOK(t, env, operatorSessionID, first.ID, map[string]any{"transfer_reference": "PP-AGAIN"})
	markFailedOK(t, env, operatorSessionID, first.ID, map[string]any{"reason": "the account number was rejected"})

	// The same amount, because nothing moved: a rejected transfer left the
	// Payable Balance exactly where it was, so the organizer may ask for all of
	// it again.
	second, created := submitPayoutRequestOK(t, env, adminSessionID, requestBody(payable, "corrected the account number"))
	if !created {
		t.Fatalf("the second ask was answered as an existing request %+v; want a new one", second)
	}
	if second.ID == first.ID {
		t.Fatalf("the second ask reopened the failed request %q; failed is terminal", first.ID)
	}
	if second.Status != "pending" || second.AmountCents != payable {
		t.Fatalf("second request = %+v; want a fresh pending ask for the full payable balance", second)
	}

	// Both are on the record, and they read as two different things. The failure
	// does not vanish when it is superseded — the history of what happened to
	// each ask is complete — and it must never be rendered as a decline.
	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 2 {
		t.Fatalf("history = %+v; want the failed ask and the fresh one", history)
	}
	byID := map[string]payoutRequest{}
	for _, request := range history {
		byID[request.ID] = request
	}
	if byID[first.ID].Status != "failed" {
		t.Fatalf("the earlier ask reads %q; want failed", byID[first.ID].Status)
	}
	if byID[second.ID].Status != "pending" {
		t.Fatalf("the fresh ask reads %q; want pending", byID[second.ID].Status)
	}

	// Still no money anywhere: two asks, one rejected transfer, zero Payouts.
	if count := payoutRowCount(t, env, "test-org"); count != 0 {
		t.Fatalf("payout rows = %d; want none — nothing has been paid", count)
	}

	// And the fresh ask holds the slot on its own account, so the failure has not
	// left the index permissive either.
	resp, body := submitPayoutRequest(t, env, adminSessionID, requestBody(1, "a third ask"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("third submission status=%d error=%+v; want 200 with the outstanding request", resp.StatusCode, body.Error)
	}
	var third payoutRequest
	if err := json.Unmarshal(body.Data, &third); err != nil {
		t.Fatalf("decode third submission: %v", err)
	}
	if third.ID != second.ID {
		t.Fatalf("third submission returned %q; want the outstanding pending request %q", third.ID, second.ID)
	}
}

// TestPayoutRequestCarriesTheDateTheTransferWasSent is the organizer's half of
// the `processing` state (#187, ADR 0026 amendment).
//
// "Your money is on its way" is not the thing the organizer needs; it is the
// thing they need PLUS a date, because the promise attached to it is "up to 48
// hours" and a promise nobody can check the age of is boilerplate. The wait
// having outrun what they were told is exactly the moment an organizer should be
// writing to support, and this field is the only way they can know it.
//
// The two fields NOT here are asserted too. They are the operator's — which
// colleague submitted the transfer, and what PayPhone called it — and neither
// answers the organizer's question. This is not masking, which this surface does
// none of: it is a smaller answer to a different question.
func TestPayoutRequestCarriesTheDateTheTransferWasSent(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	request, _ := operatorPendingRequestFor(t, env, adminSessionID, "test-org", "Sent Fest", "sent-fest", 6, 1)
	operatorSessionID := operatorSession(t, env, "sender@example.com")

	// Nobody has looked at it yet, and there is nothing to say about a transfer.
	if request.TransferSubmittedAt != nil {
		t.Fatalf("transfer_submitted_at on a pending request = %v; want null — no transfer exists", request.TransferSubmittedAt)
	}

	markProcessingOK(t, env, operatorSessionID, request.ID, map[string]any{"transfer_reference": "PP-SENT-0007"})

	history := listPayoutRequests(t, env, adminSessionID)
	if len(history) != 1 || history[0].Status != "processing" {
		t.Fatalf("organization history = %+v; want the ask, processing", history)
	}
	processing := history[0]
	if processing.TransferSubmittedAt == nil {
		t.Fatal("transfer_submitted_at on a processing request is null; the organizer's 48-hour sentence has no date to stand on")
	}
	readBack, err := time.Parse(time.RFC3339Nano, *processing.TransferSubmittedAt)
	if err != nil {
		t.Fatalf("parse transfer_submitted_at %q: %v", *processing.TransferSubmittedAt, err)
	}
	_, _, stored := transferStamp(t, env, request.ID)
	if stored == nil || !readBack.Equal(*stored) {
		t.Fatalf("transfer_submitted_at read back as %v; want the stored instant %v", readBack, stored)
	}

	// The operator's two facts stay on the operator's surface.
	raw := rawPayoutRequestFields(t, env, adminSessionID, request.ID)
	for _, field := range []string{"transfer_submitted_by", "transfer_reference"} {
		if _, present := raw[field]; present {
			t.Fatalf("%q is serialised on the organization's own surface; it answers an operator's question, not theirs", field)
		}
	}

	// And when the bank sends it back, the date it was sent survives beside the
	// reason it failed: both halves of "sent on the 12th, rejected on the 14th".
	markFailedOK(t, env, operatorSessionID, request.ID, map[string]any{"reason": "the account number was rejected"})
	failed := listPayoutRequests(t, env, adminSessionID)[0]
	if failed.Status != "failed" || failed.ResolutionReason == nil ||
		*failed.ResolutionReason != "the account number was rejected" {
		t.Fatalf("failed request = %+v; want the bank's reason on the record", failed)
	}
	if failed.TransferSubmittedAt == nil || *failed.TransferSubmittedAt != *processing.TransferSubmittedAt {
		t.Fatalf("transfer_submitted_at after a failure = %v; want the instant it was sent, unchanged", failed.TransferSubmittedAt)
	}
}
