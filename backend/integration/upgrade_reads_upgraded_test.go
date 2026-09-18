package integration

import (
	"net/http"
	"testing"
)

// AN UPGRADED SALE READS "upgraded", NEVER "corrected" (#651, parent #645,
// ADR 0074).
//
// #650 wrote the marker and these tests read it. The two acts share one pair of
// database columns — "this Ticket Sale stands in the place of that one" is one
// relation — and until the Upgrade there was one reason to write that pair, so
// every staff surface took the LINK to mean a Sale Correction. ADR 0050's word
// says an Organization's own staff recorded a sale wrongly and put it right. An
// Upgrade is a Sale recorded perfectly by a buyer who changed their mind, so
// reading `corrected` off one tells an Organization its people erred on a Sale
// no human there ever touched.
//
// THREE SURFACES AND THREE DIFFERENT SHAPES, which is why they are asserted
// separately rather than through one helper:
//
//   - the Customer Dossier derives the word in the BACKEND and publishes a
//     finished `status`;
//   - the Sales list publishes the linkage and lets the page derive its own
//     badge, so what has to travel is the REASON;
//   - the Sales Export writes a route word into a spreadsheet that leaves the
//     platform, where a column heading is a claim the reader cannot argue with.
//
// AND THE REGRESSION IS ASSERTED BESIDE THE FEATURE. A genuine Sale Correction
// must still read `corrected` on all three, because the fallthrough that keeps
// it doing so is the only thing standing between ADR 0050 and this change.

// upgradedPair is one performed Upgrade: the free Sale that was surrendered and
// the paid Sale that took its place, with the Customer they both belong to.
type upgradedPair struct {
	crossSaleUpgrade
	paidSaleID string
	paidRef    string
	customerID string
}

// performUpgrade buys the paid Ticket with the Upgrade elected and confirms it,
// which is the only moment the cross-Sale reversal happens.
func performUpgrade(t *testing.T) upgradedPair {
	t.Helper()
	f := newCrossSaleUpgrade(t)
	paid := confirmCheckoutOK(t, f.env, f.buyPaid(t, true), "approved")
	if paid.Status != "approved" {
		t.Fatalf("confirm = %+v, want approved", paid)
	}
	paidSaleID := saleIDOfPayment(t, f.env, paid.ClientTransactionID)
	// Read off the reversed side: both Sales belong to the one buyer, and the
	// free one is the half this ticket is really about.
	customerID := customerIDOnSalesList(t, f.env, f.staff, f.eventID, f.buyer, "reversed")
	return upgradedPair{
		crossSaleUpgrade: f,
		paidSaleID:       paidSaleID,
		paidRef:          paid.ConfirmationRef,
		customerID:       customerID,
	}
}

// TestAnUpgradedSaleReadsUpgradedOnTheCustomerDossier: the Dossier is handed a
// finished status, so the whole distinction is made in the backend — and the
// staff app renders whatever word arrives, through a badge table where
// `corrected` is the destructive one.
//
// BOTH HALVES NAME EACH OTHER. An Organization asked "where did my free ticket
// go" must be able to answer it from this one screen: the surrendered Sale names
// the paid Sale that replaced it, and the paid Sale names the free Sale its
// buyer gave up for it.
func TestAnUpgradedSaleReadsUpgradedOnTheCustomerDossier(t *testing.T) {
	f := performUpgrade(t)

	dossier, _ := readDossier(t, f.env, f.staff, f.eventID, f.customerID)
	surrendered := dossierSaleByRef(t, dossier, f.freeRef)
	if surrendered.Status != "upgraded" {
		t.Errorf("the surrendered free Sale's status = %q, want upgraded — `corrected` is ADR 0050's word for a mistake somebody made", surrendered.Status)
	}
	if surrendered.ReversedAt == nil {
		t.Error("the surrendered free Sale has no reversed_at; it was reversed, whatever the word for why")
	}
	if strOf(surrendered.ReplacedByConfirmationRef) != f.paidRef {
		t.Errorf("replaced_by_confirmation_ref = %s, want the paid Sale %s", strOf(surrendered.ReplacedByConfirmationRef), f.paidRef)
	}
	// It is still an Online Sale. `replaces_sale_id` is half of how the origin
	// derivation spots a Sale Correction's replacement, and the channel is the
	// only thing keeping this pair out of that branch.
	if surrendered.Origin != "channel_sale" {
		t.Errorf("the surrendered free Sale's origin = %q, want channel_sale", surrendered.Origin)
	}

	paid := dossierSaleByRef(t, dossier, f.paidRef)
	if paid.Status != "active" {
		t.Errorf("the paid Sale's status = %q, want active — it is the Sale that stands", paid.Status)
	}
	if strOf(paid.ReplacesConfirmationRef) != f.freeRef {
		t.Errorf("the paid Sale's replaces_confirmation_ref = %s, want the free Sale %s", strOf(paid.ReplacesConfirmationRef), f.freeRef)
	}
	if paid.Origin != "channel_sale" {
		t.Errorf("the paid Sale's origin = %q, want channel_sale", paid.Origin)
	}
}

// TestAnUpgradedSaleCarriesItsReasonToTheSalesList: this page derives its own
// badge from the linkage, so the API has to say WHY the two Sales are linked or
// the page has no way to tell an Upgrade from an error its staff made.
//
// The reason is asserted on BOTH rows, because it is written on both halves for
// exactly this: the replacement's row says what it stands in for without
// fetching the other one.
func TestAnUpgradedSaleCarriesItsReasonToTheSalesList(t *testing.T) {
	f := performUpgrade(t)

	surrendered := saleRowByID(t, f.env, f.staff, f.eventID, f.freeSaleID, "reversed")
	if strOf(surrendered.ReplacementReason) != "upgrade" {
		t.Errorf("the surrendered free Sale's replacement_reason = %s, want upgrade", strOf(surrendered.ReplacementReason))
	}
	if strOf(surrendered.ReplacedByConfirmationRef) != f.paidRef {
		t.Errorf("replaced_by_confirmation_ref = %s, want the paid Sale %s", strOf(surrendered.ReplacedByConfirmationRef), f.paidRef)
	}
	if surrendered.Origin != "channel_sale" {
		t.Errorf("the surrendered free Sale's origin = %q, want channel_sale", surrendered.Origin)
	}

	paid := saleRowByID(t, f.env, f.staff, f.eventID, f.paidSaleID, "active")
	if strOf(paid.ReplacementReason) != "upgrade" {
		t.Errorf("the paid Sale's replacement_reason = %s, want upgrade", strOf(paid.ReplacementReason))
	}
	if strOf(paid.ReplacesConfirmationRef) != f.freeRef {
		t.Errorf("the paid Sale's replaces_confirmation_ref = %s, want the free Sale %s", strOf(paid.ReplacesConfirmationRef), f.freeRef)
	}
	if paid.Origin != "channel_sale" {
		t.Errorf("the paid Sale's origin = %q, want channel_sale", paid.Origin)
	}
}

// TestTheSalesExportCallsAnUpgradeAnUpgrade: the file states the ROUTE a Sale
// Reversal arrived by, and an Upgrade is the sixth.
//
// THE ACTOR CANNOT TELL IT APART, which is the point of the assertion. The buyer
// caused it, so the stored actor is `customer` — the same value a Reversal
// Window undo writes, and that lever leaves the buyer holding nothing where this
// one hands them the Ticket they paid for.
//
// AND corrected_by/corrects STAY BLANK. They are ADR 0050's columns; filling
// them here would tell an accountant, under a heading they cannot argue with,
// that somebody at the Organization recorded a sale wrongly.
func TestTheSalesExportCallsAnUpgradeAnUpgrade(t *testing.T) {
	f := performUpgrade(t)

	reversed := reversedSalesExport(t, f.env, f.staff, f.eventID)
	if reversed.dataRows != 1 {
		t.Fatalf("reversed rows = %d, want only the surrendered free Sale", reversed.dataRows)
	}
	free := reversed.rowOf(t, f.buyer)
	if got := reversed.value(t, free, "reversed_by"); got != "upgrade" {
		t.Errorf("the surrendered free Sale's reversed_by = %q, want upgrade", got)
	}
	reversed.blank(t, free, "corrected_by")
	reversed.blank(t, free, "corrects")
	if got := reversed.value(t, free, "origin"); got != "channel_sale" {
		t.Errorf("origin = %q, want channel_sale", got)
	}

	resp, data := downloadSalesExport(t, f.env, f.staff, f.eventID, "status=active")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active export status=%d body=%s", resp.StatusCode, string(data))
	}
	active := openSalesExport(t, data)
	paid := active.rowOf(t, f.buyer)
	active.blank(t, paid, "reversed_by")
	active.blank(t, paid, "corrects")
	active.blank(t, paid, "corrected_by")
}

// TestASaleCorrectionStillReadsCorrectedOnEverySurface is the regression half,
// and it is the reason the reading code asks "is this an Upgrade" rather than
// "is this a correction": everything that is not the one word keeps ADR 0050's
// older, narrower sentence, so a correction reads exactly as it did before the
// Upgrade existed.
func TestASaleCorrectionStillReadsCorrectedOnEverySurface(t *testing.T) {
	env := setupTest(t)
	staff := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, staff, "Still Corrected", "still-corrected")
	gaID := createTicketTypeWithCapacity(t, env, staff, eventID, "GA", 1000, 50)

	mistaken := recordManualSaleOK(t, env, staff, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	correction := correctImportedSaleOK(t, env, staff, eventID, mistaken.SaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z"))

	// The Customer Dossier: still `corrected`, and never `upgraded`.
	customerID := customerIDOnSalesList(t, env, staff, eventID, "ana@example.com", "reversed")
	dossier, _ := readDossier(t, env, staff, eventID, customerID)
	fixed := dossierSaleByRef(t, dossier, mistaken.ConfirmationRef)
	if fixed.Status != "corrected" {
		t.Errorf("the corrected Sale's status = %q, want corrected", fixed.Status)
	}
	if strOf(fixed.ReplacedByConfirmationRef) != correction.ReplacementConfirmationRef {
		t.Errorf("replaced_by_confirmation_ref = %s, want %s", strOf(fixed.ReplacedByConfirmationRef), correction.ReplacementConfirmationRef)
	}
	replacement := dossierSaleByRef(t, dossier, correction.ReplacementConfirmationRef)
	if strOf(replacement.ReplacesConfirmationRef) != mistaken.ConfirmationRef {
		t.Errorf("the replacement's replaces_confirmation_ref = %s, want %s", strOf(replacement.ReplacesConfirmationRef), mistaken.ConfirmationRef)
	}

	// The Sales list: the reason travels there too, and it is the correction's.
	listed := saleRowByID(t, env, staff, eventID, mistaken.SaleID, "reversed")
	if strOf(listed.ReplacementReason) != "correction" {
		t.Errorf("the corrected Sale's replacement_reason = %s, want correction", strOf(listed.ReplacementReason))
	}

	// The Sales Export: the route and both linkage columns, exactly as before.
	reversed := reversedSalesExport(t, env, staff, eventID)
	row := reversed.rowOf(t, "ana@example.com")
	if got := reversed.value(t, row, "reversed_by"); got != "correction" {
		t.Errorf("the corrected Sale's reversed_by = %q, want correction", got)
	}
	if got := reversed.value(t, row, "corrected_by"); got != correction.ReplacementConfirmationRef {
		t.Errorf("corrected_by = %q, want the replacement's reference %s", got, correction.ReplacementConfirmationRef)
	}
	if got := reversed.value(t, row, "corrects"); got != "" {
		t.Errorf("the corrected Sale's corrects = %q, want blank — it corrects nothing", got)
	}
}
