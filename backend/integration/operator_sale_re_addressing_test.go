package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Sale Re-addressing an Operator records against a stranded Online Sale
// (issue #420, parent #419, ADR 0058).
//
// WHAT THIS SLICE IS. A buyer who typed their address wrong before the sign-in
// wall (ADR 0054) holds Tickets they cannot reach. The Platform Operator finds
// the Sale by the reference the buyer quoted and records the address the buyer
// meant; the platform mails THAT address a Re-addressing Link. Nothing moves
// yet — the Sale still belongs to whoever it belonged to — and the lookup shows
// the pending record. The Operator can also withdraw the pending record, or
// replace it by recording again — the same address to resend a lost mail, a
// different one to fix their own typo — and each move kills the earlier link
// (#423). The click itself is #421; the reversal/expiry interplay is #424.
//
// THE PROPERTY EVERYTHING ELSE RESTS ON is the Assignment Link's, carried over:
// the Re-addressing Link is a token delivered to the corrected address alone
// and present in NO Operator response, so the Operator cannot complete an
// acceptance on the buyer's behalf and the click stays a proof (ADR 0058). The
// captured mail sender is the ONLY place a token can be obtained here, exactly
// as an inbox is the only place a person can.
//
// THE PAYMENT PROVIDER IS NEVER CALLED. Every recording below is made against
// the app wired to the fake PayPhone server so the stub can be asked, not
// assumed, to have done nothing: no money moves when a Sale is re-addressed.

// reAddressBody is the Sale Re-addressing as the Operator states it: the
// address the buyer meant, and an optional note for whoever reads the record
// later. Who recorded it is never in the body — the API takes it from the
// Staff Session.
type reAddressBody struct {
	Email string  `json:"email"`
	Note  *string `json:"note,omitempty"`
}

// saleReAddressing is one Sale Re-addressing record as every operator surface
// shows it. Its status is derived, never stored: `pending`, `accepted`,
// `withdrawn` or `expired`.
type saleReAddressing struct {
	ID              string  `json:"id"`
	TicketSaleID    string  `json:"ticket_sale_id"`
	ConfirmationRef string  `json:"confirmation_ref"`
	Status          string  `json:"status"`
	PreviousEmail   string  `json:"previous_email"`
	CorrectedEmail  *string `json:"corrected_email"`
	Operator        string  `json:"operator"`
	Note            *string `json:"note"`
	RequestedAt     string  `json:"requested_at"`
	AcceptedAt      *string `json:"accepted_at"`
	WithdrawnAt     *string `json:"withdrawn_at"`
}

// saleReAddressingBlock is the `re_addressing` block on the Operator lookup:
// the one pending record (or null) and the history of accepted ones.
type saleReAddressingBlock struct {
	Pending  *saleReAddressing  `json:"pending"`
	Accepted []saleReAddressing `json:"accepted"`
}

// operatorSaleLookupWithReAddressing decodes the lookup with the block this
// feature adds. A separate type from operatorSaleLookup so the lookup test's
// own decoder stays the narrow shape it was written for.
type operatorSaleLookupWithReAddressing struct {
	Sale         operatorSale             `json:"sale"`
	Organization operatorSaleOrganization `json:"organization"`
	ReAddressing saleReAddressingBlock    `json:"re_addressing"`
}

func reAddressPath(confirmationRef string) string {
	return "/api/v1/operator/sales/" + confirmationRef + "/re-address"
}

func reAddressRequest(t *testing.T, env *testEnv, sessionID, confirmationRef string, body reAddressBody) (*http.Response, envelope, []byte) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	resp, envBody := env.post(t, reAddressPath(confirmationRef), body, headers)
	raw, err := json.Marshal(envBody)
	if err != nil {
		t.Fatalf("re-marshal the envelope: %v", err)
	}
	return resp, envBody, raw
}

func reAddressOK(t *testing.T, env *testEnv, sessionID, confirmationRef string, body reAddressBody) saleReAddressing {
	t.Helper()
	resp, envBody, _ := reAddressRequest(t, env, sessionID, confirmationRef, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("re-address status=%d error=%+v, want 201", resp.StatusCode, envBody.Error)
	}
	var out saleReAddressing
	if err := json.Unmarshal(envBody.Data, &out); err != nil {
		t.Fatalf("decode re-addressing: %v", err)
	}
	return out
}

func lookUpSaleWithReAddressing(t *testing.T, env *testEnv, sessionID, confirmationRef string) (operatorSaleLookupWithReAddressing, []byte) {
	t.Helper()
	resp, body := env.get(t, operatorSaleLookupPath(confirmationRef), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK || body.Error != nil {
		t.Fatalf("lookup status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out operatorSaleLookupWithReAddressing
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode lookup: %v", err)
	}
	return out, body.Data
}

// reAddressingMailFor finds the one Re-addressing mail sent to an address,
// failing if there is not exactly one. THE CAPTURED MAIL IS THE ONLY SOURCE OF
// A TOKEN IN THIS PACKAGE: no minting helper exists, deliberately.
func reAddressingMailFor(t *testing.T, env *testEnv, address string) platform.SaleReAddressing {
	t.Helper()
	var found []platform.SaleReAddressing
	for _, mail := range env.email.SaleReAddressingsSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Re-addressing mails to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// reAddressingTokenFrom pulls the token out of the Re-addressing Link, the
// way the reader's browser does when they press it.
func reAddressingTokenFrom(t *testing.T, mail platform.SaleReAddressing) string {
	t.Helper()
	base, token, found := strings.Cut(mail.AcceptURL, "?token=")
	if !found || token == "" {
		t.Fatalf("the Re-addressing mail carries no token: %q", mail.AcceptURL)
	}
	// The link lands on the Storefront and never on this API (ADR 0008), at its
	// own page — not the Assignment Link's /accept, which opens a different
	// token with a different power.
	if !strings.HasSuffix(base, "/re-addressing") {
		t.Fatalf("the Re-addressing mail points at %q, want the Storefront's /re-addressing page", base)
	}
	return token
}

// assertNoReAddressingMail fails if any Re-addressing mail went out at all —
// the assertion every refusal below makes, since a refused recording that
// still wrote to a stranger would be the worse half of the failure.
func assertNoReAddressingMail(t *testing.T, env *testEnv) {
	t.Helper()
	if sent := env.email.SaleReAddressingsSent(); len(sent) != 0 {
		t.Fatalf("captured %d Re-addressing mails from a refused recording, want none", len(sent))
	}
}

// TestOperatorReAddressesAnOnlineSaleAndTheCorrectedAddressIsMailedALink is
// the tracer bullet: the Operator records the address the buyer meant, the
// corrected address gets the one mail, the wrong address gets nothing, the
// Sale does not move, and the provider is never asked to do anything.
func TestOperatorReAddressesAnOnlineSaleAndTheCorrectedAddressIsMailedALink(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Readdress Fest", "readdress-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	// A Sale bought under a mistyped address — the case the feature exists for.
	ref, _ := buyOnlineThroughPayPhone(t, "readdress-fest", gaID, "ana.lopes@example.com", 2)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	recorded := reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{
		Email: " Ana.Lopez@Example.com ",
		Note:  strPtr("buyer wrote in from the support address"),
	})

	// THE assertion of the feature's money rule: nothing about a re-addressing
	// touches the Payment Provider.
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times; a Sale Re-addressing moves no money and must never call the provider", got)
	}

	if recorded.Status != "pending" || recorded.ConfirmationRef != ref || recorded.ID == "" || recorded.TicketSaleID == "" {
		t.Fatalf("recorded = %+v, want a pending record under the Sale's own reference %q", recorded, ref)
	}
	// The address is normalised exactly as a Customer's is, so a second typo of
	// case or whitespace cannot fragment the corrected Customer later.
	if recorded.CorrectedEmail == nil || *recorded.CorrectedEmail != "ana.lopez@example.com" {
		t.Fatalf("corrected_email = %v, want the normalised address", recorded.CorrectedEmail)
	}
	// The Sale's address AT REQUEST TIME is kept beside the correction: it is
	// the evidence of what was wrong.
	if recorded.PreviousEmail != "ana.lopes@example.com" {
		t.Fatalf("previous_email = %q, want the Sale's address as it stood", recorded.PreviousEmail)
	}
	if recorded.Operator != "operator@example.com" {
		t.Fatalf("operator = %q, want the acting operator's own session email", recorded.Operator)
	}
	if recorded.Note == nil || *recorded.Note != "buyer wrote in from the support address" {
		t.Fatalf("note = %v, want the note as stated", recorded.Note)
	}
	if recorded.RequestedAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("requested_at = %q, want the server's own clock %q", recorded.RequestedAt, env.fixedClock.UTC().Format(time.RFC3339))
	}
	if recorded.AcceptedAt != nil || recorded.WithdrawnAt != nil {
		t.Fatalf("accepted_at/withdrawn_at = %v/%v on a fresh record, want both null", recorded.AcceptedAt, recorded.WithdrawnAt)
	}

	// THE MAIL: to the corrected address only, naming the Event and the
	// reference, carrying the link. The wrong address hears nothing, and no
	// other message goes out — the Sale has not moved, so there is no receipt
	// and no void notice.
	mail := reAddressingMailFor(t, env, "ana.lopez@example.com")
	token := reAddressingTokenFrom(t, mail)
	if mail.EventName != "Readdress Fest" || mail.Reference != ref {
		t.Fatalf("mail names %q / %q, want the Event and the reference", mail.EventName, mail.Reference)
	}
	for _, other := range env.email.SaleReAddressingsSent() {
		if other.To == "ana.lopes@example.com" {
			t.Fatal("the wrong address was mailed; it is nobody or a stranger and is told nothing (ADR 0058)")
		}
	}
	if len(env.email.SaleReAddressingsSent()) != 1 {
		t.Fatalf("captured %d Re-addressing mails, want exactly one", len(env.email.SaleReAddressingsSent()))
	}
	if got := len(env.email.Voided()) + len(env.email.Confirmations()); got != 0 {
		t.Fatalf("captured %d other Customer mails, want none — nothing has moved yet", got)
	}
	text := mail.Text()
	for _, want := range []string{"Readdress Fest", ref, mail.AcceptURL} {
		if !strings.Contains(text, want) {
			t.Fatalf("mail text = %q, want it to contain %q", text, want)
		}
	}

	// THE SALE HAS NOT MOVED. The lookup still shows the wrong address as the
	// buyer, and now carries the pending record — without the token.
	found, raw := lookUpSaleWithReAddressing(t, env, operatorSessionID, ref)
	if found.Sale.Customer.Email != "ana.lopes@example.com" || found.Sale.Status != "active" {
		t.Fatalf("sale after recording = %s / %s, want still addressed to the wrong address and active",
			found.Sale.Customer.Email, found.Sale.Status)
	}
	pending := found.ReAddressing.Pending
	if pending == nil {
		t.Fatalf("re_addressing.pending is null after recording; block = %+v", found.ReAddressing)
	}
	if pending.ID != recorded.ID || pending.Status != "pending" ||
		pending.CorrectedEmail == nil || *pending.CorrectedEmail != "ana.lopez@example.com" ||
		pending.PreviousEmail != "ana.lopes@example.com" ||
		pending.Operator != "operator@example.com" ||
		pending.Note == nil || *pending.Note != "buyer wrote in from the support address" ||
		pending.RequestedAt != recorded.RequestedAt {
		t.Fatalf("pending = %+v, want the record as recorded", pending)
	}
	if found.ReAddressing.Accepted == nil || len(found.ReAddressing.Accepted) != 0 {
		t.Fatalf("accepted = %v, want an empty list (never null) — nobody has accepted anything", found.ReAddressing.Accepted)
	}

	// THE TOKEN IS IN NO OPERATOR RESPONSE. The Operator recorded the address
	// and is shown the record; the link is the corrected address's alone, so
	// the Operator cannot complete the acceptance themself.
	_, _, postRaw := reAddressRequest(t, env, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	for name, payload := range map[string]string{"the lookup": string(raw), "the second POST": string(postRaw)} {
		if strings.Contains(payload, token) {
			t.Errorf("%s contains the Re-addressing Link token; ADR 0058 delivers it to the corrected address alone", name)
		}
		if strings.Contains(payload, "?token=") || strings.Contains(payload, "/re-addressing?") {
			t.Errorf("%s contains a Re-addressing Link URL", name)
		}
	}
}

// withdrawReAddressRequest is the Operator's DELETE on the same path the
// recording POSTs to: the one control that ends a pending re-addressing
// (#423). No body — there is nothing to say beyond "not this one".
func withdrawReAddressRequest(t *testing.T, env *testEnv, sessionID, confirmationRef string) (*http.Response, envelope) {
	t.Helper()
	var headers map[string]string
	if sessionID != "" {
		headers = authHeader(sessionID)
	}
	return env.deleteJSON(t, reAddressPath(confirmationRef), nil, headers)
}

func withdrawReAddressOK(t *testing.T, env *testEnv, sessionID, confirmationRef string) saleReAddressing {
	t.Helper()
	resp, envBody := withdrawReAddressRequest(t, env, sessionID, confirmationRef)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw status=%d error=%+v, want 200", resp.StatusCode, envBody.Error)
	}
	var out saleReAddressing
	if err := json.Unmarshal(envBody.Data, &out); err != nil {
		t.Fatalf("decode withdrawn re-addressing: %v", err)
	}
	return out
}

// assertReAddressingLinkDead is the assertion every move below makes through
// the public view: a link whose record has ended refuses both the view and the
// click, and says why. The link died because the OPERATOR ended the record, so
// the reason is `withdrawn` whether the record was withdrawn outright or
// replaced — from the reader's side those are the same fact.
func assertReAddressingLinkDead(t *testing.T, token string) {
	t.Helper()
	resp, body := viewReAddressingLink(t, payphoneEnv, token)
	assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
	if got := answerDetail(body, "reason"); got != "withdrawn" {
		t.Errorf("view details.reason = %q, want withdrawn", got)
	}
	resp, body = acceptReAddressingLink(t, payphoneEnv, token)
	assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
	if got := answerDetail(body, "reason"); got != "withdrawn" {
		t.Errorf("accept details.reason = %q, want withdrawn", got)
	}
}

// TestOperatorWithdrawsAPendingReAddressing: a mistake of the Operator's never
// completes (ADR 0058). The withdrawal stamps the record, kills the link on
// both its routes, mails nobody, and leaves the Sale where it was — with the
// lever offered again, since nothing is pending any more.
func TestOperatorWithdrawsAPendingReAddressing(t *testing.T) {
	env := setupTest(t)
	s := strandSale(t, env, "Withdraw Fest", "readdress-withdraw-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)
	viewReAddressingLinkOK(t, payphoneEnv, s.token)

	withdrawn := withdrawReAddressOK(t, payphoneEnv, s.operator, s.ref)
	if withdrawn.Status != "withdrawn" || withdrawn.ConfirmationRef != s.ref ||
		withdrawn.CorrectedEmail == nil || *withdrawn.CorrectedEmail != "ana.lopez@example.com" {
		t.Fatalf("withdrawn = %+v, want the pending record read back as withdrawn", withdrawn)
	}
	if withdrawn.WithdrawnAt == nil || *withdrawn.WithdrawnAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("withdrawn_at = %v, want the server's own clock", withdrawn.WithdrawnAt)
	}
	if withdrawn.AcceptedAt != nil {
		t.Fatalf("accepted_at = %v on a withdrawn record, want null", withdrawn.AcceptedAt)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times; a withdrawal moves no money", got)
	}

	// NOBODY IS MAILED: not the corrected address, whose link simply stops
	// working, and not the wrong one, which is told nothing (ADR 0058).
	assertNoReAddressingMail(t, env)
	if got := len(env.email.Voided()) + len(env.email.Confirmations()); got != 0 {
		t.Fatalf("captured %d Customer mails from a withdrawal, want none", got)
	}

	// THE LINK IS DEAD on both routes, and the click minted nobody.
	assertReAddressingLinkDead(t, s.token)
	if _, exists := readHolderCustomer(t, env, "ana.lopez@example.com"); exists {
		t.Error("a withdrawn link minted a Customer")
	}

	// THE SALE HAS NOT MOVED, and the lookup shows nothing pending and nothing
	// accepted: a withdrawn record is kept as evidence but is not an
	// outstanding correction.
	found, raw := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.Sale.Customer.Email != "ana.lopes@example.com" || found.Sale.Status != "active" {
		t.Fatalf("sale after withdrawal = %s / %s, want untouched", found.Sale.Customer.Email, found.Sale.Status)
	}
	if found.ReAddressing.Pending != nil || len(found.ReAddressing.Accepted) != 0 {
		t.Fatalf("re_addressing after withdrawal = %s, want nothing pending and nothing accepted", raw)
	}

	// A second withdrawal has nothing to withdraw.
	resp, body := withdrawReAddressRequest(t, payphoneEnv, s.operator, s.ref)
	assertRefused(t, resp, body, http.StatusConflict, "RE_ADDRESSING_NOTHING_PENDING")

	// And the lever is offered again: a fresh recording opens a fresh link,
	// which is the form reappearing on the Sale page.
	again := reAddressOK(t, payphoneEnv, s.operator, s.ref, reAddressBody{Email: "ana.lopez@example.com"})
	if again.ID == withdrawn.ID || again.Status != "pending" {
		t.Fatalf("recording after withdrawal = %+v, want a new pending record, not the withdrawn one reopened", again)
	}
	viewReAddressingLinkOK(t, payphoneEnv, reAddressingTokenFrom(t, reAddressingMailFor(t, env, "ana.lopez@example.com")))
	assertReAddressingLinkDead(t, s.token)
}

// TestWithdrawingIsRefusedWhenNothingIsPending: with no recording against the
// Sale there is nothing to end, and the refusal writes nothing and mails
// nobody. The withdraw is also the operator's alone, and 404 on a reference
// nothing carries, exactly as the recording is.
func TestWithdrawingIsRefusedWhenNothingIsPending(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Nothing Fest", "readdress-nothing-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-nothing-fest", gaID, "ana.lopes@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	resp, body := withdrawReAddressRequest(t, env, operatorSessionID, ref)
	assertRefused(t, resp, body, http.StatusConflict, "RE_ADDRESSING_NOTHING_PENDING")
	assertNoReAddressingMail(t, env)

	resp, body = withdrawReAddressRequest(t, env, operatorSessionID, "TP-NOSUCHREF")
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Fatalf("unknown reference status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}

	reAddressOK(t, env, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	resp, body = withdrawReAddressRequest(t, env, "", ref)
	if resp.StatusCode != http.StatusUnauthorized || body.Error == nil || body.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated withdrawal status=%d error=%+v, want 401 UNAUTHORIZED", resp.StatusCode, body.Error)
	}
	resp, body = withdrawReAddressRequest(t, env, sessionID, ref)
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("org_admin withdrawal status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
	found, _ := lookUpSaleWithReAddressing(t, env, operatorSessionID, ref)
	if found.ReAddressing.Pending == nil {
		t.Fatal("a refused withdrawal ended the pending record")
	}
}

// TestRecordingWhileOneIsPendingReplacesItAndKillsItsLink: fixing the
// Operator's own typo is one act, not two. The new recording withdraws the old
// in the same transaction, the old link dies, the new address alone is mailed,
// and the lookup shows only the new record. The old one is kept, ended — the
// evidence of what was typed first.
func TestRecordingWhileOneIsPendingReplacesItAndKillsItsLink(t *testing.T) {
	env := setupTest(t)
	s := strandSale(t, env, "Replace Fest", "readdress-replace-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)
	first, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	firstID := first.ReAddressing.Pending.ID

	replaced := reAddressOK(t, payphoneEnv, s.operator, s.ref, reAddressBody{
		Email: "ana.lopez.real@example.com",
		Note:  strPtr("second typo was mine"),
	})
	if replaced.Status != "pending" || replaced.ID == firstID ||
		replaced.CorrectedEmail == nil || *replaced.CorrectedEmail != "ana.lopez.real@example.com" {
		t.Fatalf("replacement = %+v, want a new pending record for the new address", replaced)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times", got)
	}

	// ONE MAIL, TO THE NEW ADDRESS. The first corrected address is not told its
	// link died — it was a typo, and may be nobody.
	newMail := reAddressingMailFor(t, env, "ana.lopez.real@example.com")
	if len(env.email.SaleReAddressingsSent()) != 1 {
		t.Fatalf("captured %d Re-addressing mails from the replacement, want exactly one", len(env.email.SaleReAddressingsSent()))
	}
	newToken := reAddressingTokenFrom(t, newMail)
	if newToken == s.token {
		t.Fatal("the replacement reused the old token")
	}

	// THE OLD LINK IS DEAD, THE NEW ONE OPENS.
	assertReAddressingLinkDead(t, s.token)
	if view := viewReAddressingLinkOK(t, payphoneEnv, newToken); view.CorrectedEmail != "ana.lopez.real@example.com" {
		t.Fatalf("new link views %+v, want the new address", view)
	}

	// THE LOOKUP SHOWS ONLY THE NEW RECORD.
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	pending := found.ReAddressing.Pending
	if pending == nil || pending.ID != replaced.ID || *pending.CorrectedEmail != "ana.lopez.real@example.com" ||
		pending.Note == nil || *pending.Note != "second typo was mine" {
		t.Fatalf("pending = %+v, want the replacement alone", pending)
	}
	if len(found.ReAddressing.Accepted) != 0 || found.Sale.Customer.Email != "ana.lopes@example.com" {
		t.Fatalf("accepted=%v customer=%s, want nothing accepted and the sale untouched", found.ReAddressing.Accepted, found.Sale.Customer.Email)
	}

	// And the new address can accept: the replacement is a real recording.
	accepted := acceptReAddressingLinkOK(t, payphoneEnv, newToken)
	if accepted.ConfirmationRef != s.ref {
		t.Fatalf("accepted = %+v", accepted)
	}
	found, _ = lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.Sale.Customer.Email != "ana.lopez.real@example.com" || len(found.ReAddressing.Accepted) != 1 || found.ReAddressing.Pending != nil {
		t.Fatalf("after acceptance customer=%s accepted=%d pending=%v", found.Sale.Customer.Email, len(found.ReAddressing.Accepted), found.ReAddressing.Pending)
	}
	if _, exists := readHolderCustomer(t, env, "ana.lopez@example.com"); exists {
		t.Error("the replaced address was minted a Customer")
	}
}

// TestRecordingTheSameAddressAgainResendsTheLink: a lost mail is one click to
// recover. "Send again" is recording the same address again — a fresh record,
// a fresh mail, a fresh token bound to the new row, and the previous token
// refused, so a link that leaked with the lost mail cannot be used later.
func TestRecordingTheSameAddressAgainResendsTheLink(t *testing.T) {
	env := setupTest(t)
	s := strandSale(t, env, "Resend Fest", "readdress-resend-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)
	first, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	firstID := first.ReAddressing.Pending.ID

	resent := reAddressOK(t, payphoneEnv, s.operator, s.ref, reAddressBody{Email: "Ana.Lopez@example.com"})
	if resent.Status != "pending" || resent.ID == firstID || *resent.CorrectedEmail != "ana.lopez@example.com" {
		t.Fatalf("resend = %+v, want a fresh pending record for the same address", resent)
	}

	mail := reAddressingMailFor(t, env, "ana.lopez@example.com")
	if len(env.email.SaleReAddressingsSent()) != 1 {
		t.Fatalf("captured %d Re-addressing mails from the resend, want exactly one", len(env.email.SaleReAddressingsSent()))
	}
	newToken := reAddressingTokenFrom(t, mail)
	if newToken == s.token {
		t.Fatal("the resend reused the previous token; a resend mints a fresh one")
	}
	assertReAddressingLinkDead(t, s.token)
	viewReAddressingLinkOK(t, payphoneEnv, newToken)

	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.ReAddressing.Pending == nil || found.ReAddressing.Pending.ID != resent.ID {
		t.Fatalf("pending = %+v, want the resent record alone", found.ReAddressing.Pending)
	}
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times", got)
	}
}

// TestReAddressingIsRefusedForTheSaleCurrentAddress: a correction to what the
// Sale already says is a no-op and is never recorded — however the address is
// capitalised or padded, since both normalise the same way.
func TestReAddressingIsRefusedForTheSaleCurrentAddress(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Same Fest", "readdress-same-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-same-fest", gaID, "ana@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	resp, body, _ := reAddressRequest(t, env, operatorSessionID, ref, reAddressBody{Email: "  ANA@Example.com "})
	assertRefused(t, resp, body, http.StatusConflict, "RE_ADDRESSING_SAME_ADDRESS")
	assertNoReAddressingMail(t, env)

	found, _ := lookUpSaleWithReAddressing(t, env, operatorSessionID, ref)
	if found.ReAddressing.Pending != nil {
		t.Fatalf("pending = %+v after a refused recording, want null", found.ReAddressing.Pending)
	}
}

// TestReAddressingIsRefusedOnANonOnlineSale: an imported Sale has Sale
// Correction (ADR 0050) and a door sale has no buyer surface, so neither is
// re-addressed here — the Operator's two levers share one channel rule.
func TestReAddressingIsRefusedOnANonOnlineSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	ref := seedSaleForCustomer(t, env, sessionID, "Imported Fest", "readdress-imported-fest",
		env.fixedClock.Add(30*24*time.Hour), "readdress-import-1", "ana@example.com", "Ana", "Lopez")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	resp, body, _ := reAddressRequest(t, env, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	assertRefused(t, resp, body, http.StatusConflict, "SALE_NOT_RE_ADDRESSABLE")
	assertNoReAddressingMail(t, env)
}

// TestReAddressingIsRefusedOnAReversedSale: a reversed Sale has nothing left to
// give the buyer; only the money question remains, and that is answered.
func TestReAddressingIsRefusedOnAReversedSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Reversed Fest", "readdress-reversed-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-reversed-fest", gaID, "ana.lopes@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, env, operatorSessionID, ref, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
	})
	env.email.Reset()

	resp, body, _ := reAddressRequest(t, env, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	assertRefused(t, resp, body, http.StatusConflict, "SALE_ALREADY_REVERSED")
	assertNoReAddressingMail(t, env)
}

// TestReAddressingIsRefusedOnceTheEventHasStarted: past the doors, "give me my
// tickets" has no meaning, and the lever is not offered (ADR 0058). The start
// is read live from the Event as it stands now.
func TestReAddressingIsRefusedOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishEventStarting(t, env, sessionID, "Started Fest", "readdress-started-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-started-fest", gaID, "ana.lopes@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	setEventStart(t, env, eventID, env.fixedClock.Add(-time.Minute))
	env.email.Reset()

	resp, body, _ := reAddressRequest(t, env, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	assertRefused(t, resp, body, http.StatusConflict, "RE_ADDRESSING_EVENT_STARTED")
	assertNoReAddressingMail(t, env)
}

// TestReAddressingIsOperatorsOnly: moving whom a paid record belongs to is a
// larger trust grant than any Organization holds (ADR 0058). An Org Admin of
// the very Organization whose Sale it is cannot record one; the allowlist can.
func TestReAddressingIsOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Gated Fest", "readdress-gated-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-gated-fest", gaID, "ana.lopes@example.com")
	env.email.Reset()

	body := reAddressBody{Email: "ana.lopez@example.com"}

	resp, envBody, _ := reAddressRequest(t, env, "", ref, body)
	if resp.StatusCode != http.StatusUnauthorized || envBody.Error == nil || envBody.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthenticated recording status=%d error=%+v, want 401 UNAUTHORIZED", resp.StatusCode, envBody.Error)
	}
	resp, envBody, _ = reAddressRequest(t, env, sessionID, ref, body)
	if resp.StatusCode != http.StatusForbidden || envBody.Error == nil || envBody.Error.Code != "FORBIDDEN" {
		t.Fatalf("org_admin recording status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, envBody.Error)
	}
	assertNoReAddressingMail(t, env)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	if got := reAddressOK(t, env, operatorSessionID, ref, body); got.Status != "pending" {
		t.Fatalf("operator recording = %+v", got)
	}
}

// TestReAddressingOfAnUnknownReference is the lookup's own 404: a mistyped
// reference names no Sale.
func TestReAddressingOfAnUnknownReference(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	resp, body, _ := reAddressRequest(t, env, operatorSessionID, "TP-NOSUCHREF", reAddressBody{Email: "ana@example.com"})
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_SALE_NOT_FOUND" {
		t.Fatalf("unknown reference status=%d error=%+v, want 404 TICKET_SALE_NOT_FOUND", resp.StatusCode, body.Error)
	}
}

// TestReAddressingValidatesTheCorrectedAddress: a second typo is caught where it
// can be — the shape of the address and the bound on the note are the
// handler's, before any Sale is read.
func TestReAddressingValidatesTheCorrectedAddress(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Typo Fest", "readdress-typo-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref := buyOnline(t, env, "readdress-typo-fest", gaID, "ana.lopes@example.com")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	for name, body := range map[string]reAddressBody{
		"a blank address":       {Email: "   "},
		"no @ at all":           {Email: "ana.lopez.example.com"},
		"a display-name form":   {Email: "Ana <ana.lopez@example.com>"},
		"a note past the bound": {Email: "ana.lopez@example.com", Note: strPtr(strings.Repeat("n", 501))},
	} {
		resp, envBody, _ := reAddressRequest(t, env, operatorSessionID, ref, body)
		if resp.StatusCode != http.StatusBadRequest || envBody.Error == nil || envBody.Error.Code != "VALIDATION_FAILED" {
			t.Fatalf("%s: status=%d error=%+v, want 400 VALIDATION_FAILED", name, resp.StatusCode, envBody.Error)
		}
	}
	assertNoReAddressingMail(t, env)
}

// TestTheReAddressingMailIsWrittenInTheSalesLocale: the corrected address is
// written to in the language the Sale was made in, as the original Sale
// Confirmation would have been (ADR 0033, ADR 0058) — usted throughout.
func TestTheReAddressingMailIsWrittenInTheSalesLocale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Locale Fest", "readdress-locale-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref, _ := buyOnlineThroughPayPhoneInLocale(t, "readdress-locale-fest", gaID, "ana.lopes@example.com", 1, "es")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()

	reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	mail := reAddressingMailFor(t, env, "ana.lopez@example.com")
	if mail.Locale != platform.LocaleES {
		t.Fatalf("mail locale = %q, want es — the Sale's own language", mail.Locale)
	}
	if !strings.Contains(mail.Subject(), "Locale Fest") || !strings.Contains(mail.Text(), "usted") {
		t.Fatalf("subject/text = %q / %q, want Spanish in usted naming the Event", mail.Subject(), mail.Text())
	}

	// And a Sale made on no page at all falls to the English floor.
	env.email.Reset()
	ref2 := buyOnline(t, env, "readdress-locale-fest", gaID, "bea.lopes@example.com")
	reAddressOK(t, env, operatorSessionID, ref2, reAddressBody{Email: "bea.lopez@example.com"})
	if got := reAddressingMailFor(t, env, "bea.lopez@example.com").Locale; got != platform.LocaleEN {
		t.Fatalf("mail locale = %q, want the English floor", got)
	}
}
