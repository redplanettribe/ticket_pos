package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Customer Dossier (#638, spec #635; CONTEXT.md "Customer Dossier"): the
// one read of everything an Event knows about one Customer.
//
// ONE CALL, ONE ASSEMBLY. GetCustomerDossier is the whole interface: the
// handler asks for a Dossier and gets a finished one. Each later part of the
// Dossier — a Sale's phone, Affiliate Link, invoices and re-addressing (#639),
// its Tickets and the Tickets held on other Sales (#640), their Answers (#641)
// — is a field on CustomerDossier or DossierSale, filled here, so no caller
// ever composes a Dossier out of pieces.
//
// SCOPED TO THE EVENT, NEVER TO THE PLATFORM-GLOBAL CUSTOMER. From the Customer
// record it takes the id and the email identity; every name and Tax ID comes
// off this Event's own Sales, beside the Sale it was given on. A later purchase
// at another Organization rewrites nothing shown here.
//
// NOTHING AT THIS EVENT IS NOT FOUND. A Customer with no Ticket Sale on this
// Event is answered exactly as an id that names nobody, so the Dossier cannot
// be used to learn whether somebody bought elsewhere. #640 widens "something at
// this Event" to a Ticket held on another buyer's Sale.

// Dossier Sale statuses. `corrected` is a reversed Sale that a Sale Correction
// replaced — the same reading the Sales list gives a reversed row naming its
// replacement — and `upgraded` is a reversed Sale its own buyer surrendered for
// a paid one.
//
// THE LAST TWO ARE BOTH "REVERSED AND REPLACED" AND THEY ARE STILL TWO WORDS
// (#651, ADR 0074). `corrected` is ADR 0050's, and it says somebody recorded a
// sale wrongly and staff put it right; an Upgrade is a Sale recorded perfectly
// by a buyer who changed their mind, and no member of staff ever touched it.
// One word for both would tell an Organization its people erred on a Sale nobody
// there has ever seen.
const (
	DossierSaleActive    = "active"
	DossierSaleReversed  = "reversed"
	DossierSaleCorrected = "corrected"
	DossierSaleUpgraded  = "upgraded"
)

// CustomerDossier is the Customer Dossier response.
type CustomerDossier struct {
	Customer DossierCustomerView `json:"customer"`
	// Sales are this Event's Ticket Sales to the Customer, reversed and
	// corrected ones included, newest first.
	Sales []DossierSaleView `json:"sales"`
	// HeldTickets are the Tickets of this Event the Customer ACCEPTED on
	// somebody else's Sale, reversed Sales included, newest Sale first (#640).
	// Absent while TICKET_ASSIGNMENT_ENABLED is closed (ADR 0045); `[]` when
	// they hold none.
	HeldTickets *[]DossierHeldTicketView `json:"held_tickets,omitempty"`
}

// DossierCustomerView is who the Customer is, platform-wide: an id and an
// email, and deliberately nothing more.
type DossierCustomerView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// DossierSaleView is one of this Event's Ticket Sales on the Dossier, with the
// name and Tax ID given on it.
type DossierSaleView struct {
	ID              string `json:"id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is `active`, `reversed`, `corrected` or `upgraded`.
	Status string `json:"status" enums:"active,reversed,corrected,upgraded"`
	// ReversedAt is when a reversed, corrected or upgraded Sale was reversed;
	// null on an active one.
	ReversedAt *time.Time `json:"reversed_at"`
	// ReplacedByConfirmationRef names the replacement of a corrected Sale, or
	// the paid Sale an upgraded one was surrendered for.
	ReplacedByConfirmationRef *string `json:"replaced_by_confirmation_ref"`
	// ReplacesConfirmationRef is the other half of that pair, stated on the
	// REPLACEMENT: the mistaken Sale it corrects, or the free Sale its buyer
	// gave up for it. Both halves are published so an Organization reading
	// either row can explain to a buyer where a Ticket went (#651).
	ReplacesConfirmationRef *string `json:"replaces_confirmation_ref"`
	// ReplacementReason is WHY the two Sales are linked: `correction` or
	// `upgrade`, null when there is no link.
	//
	// IT IS FOR THE REPLACEMENT'S HALF, which `status` cannot speak for. The
	// replaced Sale's word arrives in `status` — `corrected` or `upgraded`,
	// decided here so the page never infers it — but the Sale standing in its
	// place is simply `active`, and it still has to say whether it corrects a
	// mistake or stands in for a Ticket its buyer traded up from.
	ReplacementReason *string   `json:"replacement_reason" enums:"correction,upgrade"`
	SoldAt            time.Time `json:"sold_at"`
	RecordedAt        time.Time `json:"recorded_at"`
	// Channel is the Sales Channel: `online`, `in_person` or `import`.
	Channel string `json:"channel"`
	// Source is the Sales list's `source` for the Sale.
	Source *string `json:"source"`
	// Origin is how the Sale reached the platform, derived exactly as the Sales
	// list derives it (ADR 0052).
	Origin            string                `json:"origin"`
	TicketTypes       []DossierSaleLineView `json:"ticket_types"`
	AmountCents       int                   `json:"amount_cents"`
	Currency          string                `json:"currency"`
	PaymentMethod     *string               `json:"payment_method"`
	CustomerFirstName string                `json:"customer_first_name"`
	CustomerLastName  string                `json:"customer_last_name"`
	TaxIDType         *string               `json:"tax_id_type"`
	TaxIDNumber       *string               `json:"tax_id_number"`

	// What surrounds the Sale (#639) — see customer_dossier_surroundings.go.

	// Phone is the number given on the checkout this Sale came from, read off
	// its Payment (ADR 0073) and never the Customer record's current phone.
	// Null for a Sale with no checkout behind it — In-Person, imported or
	// manually recorded — or a checkout that gave none.
	Phone *string `json:"phone"`
	// AffiliateLinkName is the display name of the Affiliate Link that
	// attributed the Sale, or null.
	AffiliateLinkName *string `json:"affiliate_link_name"`
	// TaxInvoices are the Tax Invoices about the Sale — its Sale Invoices and
	// any Credit Notes — as the operator Sale lookup
	// reads them, in the Sale's chain order; empty, never null.
	TaxInvoices []DossierTaxInvoiceView `json:"tax_invoices"`
	// ReAddressedAt is when the Sale was re-addressed (ADR 0058) — the
	// acceptance of its latest accepted Sale Re-addressing — or null. Neither
	// address and no token is ever carried.
	ReAddressedAt *time.Time `json:"re_addressed_at"`
	// Tickets and assignment reminders (#640).

	// Tickets are the Sale's Tickets in the catalog's order. A reversed or
	// corrected Sale's Tickets are void and carry their Ticket Type, ordinal
	// and Answers only; `status` is what says so.
	Tickets []DossierTicketView `json:"tickets"`
	// AssignmentReminderSentAt is when an Assignment Reminder was sent about
	// this Sale, oldest first. Absent while TICKET_ASSIGNMENT_ENABLED is closed.
	AssignmentReminderSentAt *[]time.Time `json:"assignment_reminder_sent_at,omitempty"`
}

// DossierTicketView is one Ticket of a Dossier Sale.
//
// EVERY ASSIGNMENT FIELD COMES FROM catalog.DiscloseHolder (ADR 0047) and is
// `omitempty`: absent while TICKET_ASSIGNMENT_ENABLED is closed and on a
// reversed Sale. An unaccepted assignment is a state and never a person.
type DossierTicketView struct {
	// TicketID is what the Ticket's Answers are read and given by.
	TicketID       string `json:"ticket_id"`
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	// Ordinal is which of its Ticket Sale Line's units this Ticket is.
	Ordinal int `json:"ordinal"`
	// AssignmentState is `unassigned`, `assigned` or `accepted`.
	AssignmentState string `json:"assignment_state,omitempty" enums:"unassigned,assigned,accepted"`
	// NeverAccepted marks an assignment whose unaccepted address the retention
	// purge took; it reads `assigned` beside it (#334).
	NeverAccepted bool `json:"never_accepted,omitempty"`
	// SelfHeld marks a Ticket this Customer accepted on their own Sale — their
	// Self-held Ticket (ADR 0048) — which names nobody else.
	SelfHeld bool `json:"self_held,omitempty"`
	// The accepted Holder when that is somebody else: their Customer id, which
	// links their Dossier, and the name they gave as Holder.
	HolderCustomerID string `json:"holder_customer_id,omitempty"`
	HolderFirstName  string `json:"holder_first_name,omitempty"`
	HolderLastName   string `json:"holder_last_name,omitempty"`
	// Answers, Outstanding Answers and the last Answer Reminder (#641); a
	// Ticket on a reversed Sale carries its Answers only.
	DossierTicketAnswers
}

// DossierHeldTicketView is a Ticket the Customer holds on somebody else's Sale.
type DossierHeldTicketView struct {
	TicketID       string `json:"ticket_id"`
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Ordinal        int    `json:"ordinal"`
	// TicketSaleID and ConfirmationRef name the Sale it is on.
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// SaleStatus is `reversed` when that Sale was reversed (a Sale Correction
	// included): the Ticket is void, and this Customer held it.
	SaleStatus string `json:"sale_status" enums:"active,reversed"`
	// The buyer's name as given on that Sale.
	BuyerFirstName string `json:"buyer_first_name"`
	BuyerLastName  string `json:"buyer_last_name"`
	// AcceptedAt is when this Customer accepted the Ticket.
	AcceptedAt *time.Time `json:"accepted_at"`
	// The name this Customer gave as its Holder.
	HolderFirstName string `json:"holder_first_name,omitempty"`
	HolderLastName  string `json:"holder_last_name,omitempty"`
	// Answers, Outstanding Answers and the last Answer Reminder (#641); a
	// Ticket on a reversed Sale carries its Answers only.
	DossierTicketAnswers
}

// DossierSaleLineView is one Ticket Type bought on a Dossier Sale.
type DossierSaleLineView struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

// GetCustomerDossier assembles the Customer Dossier of one Customer at one of
// the caller's Organization's Events.
func (s *Service) GetCustomerDossier(
	ctx context.Context, actor ActorContext, eventID, customerID string,
) (*CustomerDossier, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	saleRows, err := s.repo.ListDossierSales(ctx, actor.OrganizationID, eventID, customerID)
	if err != nil {
		return nil, err
	}
	// #640: a Customer who holds a Ticket of this Event — accepted, on somebody
	// else's Sale, reversed or not — is found too; never while assignment is dark.
	var heldTickets []DossierHeldTicketView
	if s.ticketAssignmentEnabled {
		if heldTickets, err = s.dossierHeldTickets(ctx, actor, eventID, customerID); err != nil {
			return nil, err
		}
	}
	if len(saleRows) == 0 && len(heldTickets) == 0 {
		return nil, catalog.ErrCustomerNotFoundAtEvent()
	}
	customer, err := s.repo.GetDossierCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, catalog.ErrCustomerNotFoundAtEvent()
	}

	dossier := &CustomerDossier{
		Customer: DossierCustomerView{ID: customer.ID, Email: customer.Email},
		Sales:    make([]DossierSaleView, 0, len(saleRows)),
	}
	for _, row := range saleRows {
		dossier.Sales = append(dossier.Sales, dossierSaleView(row))
	}
	if err := s.fillDossierSaleSurroundings(ctx, actor, eventID, dossier.Sales); err != nil {
		return nil, err
	}
	if err := s.fillDossierTickets(ctx, actor, eventID, customerID, dossier.Sales); err != nil {
		return nil, err
	}
	if err := s.fillDossierAnswers(ctx, actor, eventID, dossier.Sales, heldTickets); err != nil {
		return nil, err
	}
	if s.ticketAssignmentEnabled {
		if heldTickets == nil {
			heldTickets = []DossierHeldTicketView{}
		}
		dossier.HeldTickets = &heldTickets
	}
	return dossier, nil
}

// fillDossierTickets puts each Sale's Tickets and, while assignment is open, its
// assignment reminder send times on the Sales (#640).
func (s *Service) fillDossierTickets(
	ctx context.Context, actor ActorContext, eventID, customerID string, sales []DossierSaleView,
) error {
	saleIDs := make([]string, 0, len(sales))
	for _, sale := range sales {
		saleIDs = append(saleIDs, sale.ID)
	}
	tickets, err := s.repo.ListDossierSaleTickets(ctx, actor.OrganizationID, eventID, saleIDs)
	if err != nil {
		return err
	}
	ticketsBySale := make(map[string][]repository.DossierTicket, len(sales))
	for _, ticket := range tickets {
		ticketsBySale[ticket.TicketSaleID] = append(ticketsBySale[ticket.TicketSaleID], ticket)
	}

	remindersBySale := map[string][]time.Time{}
	if s.ticketAssignmentEnabled {
		reminders, err := s.repo.ListDossierAssignmentReminders(ctx, actor.OrganizationID, eventID, saleIDs)
		if err != nil {
			return err
		}
		for _, reminder := range reminders {
			remindersBySale[reminder.TicketSaleID] = append(remindersBySale[reminder.TicketSaleID], reminder.SentAt)
		}
	}

	for i := range sales {
		sale := &sales[i]
		live := sale.Status == DossierSaleActive
		sale.Tickets = make([]DossierTicketView, 0, len(ticketsBySale[sale.ID]))
		for _, ticket := range ticketsBySale[sale.ID] {
			view := DossierTicketView{
				TicketID:       ticket.ID,
				TicketTypeID:   ticket.TicketTypeID,
				TicketTypeName: ticket.TicketTypeName,
				Ordinal:        ticket.Ordinal,
			}
			// A reversed Sale's Tickets are void: no live assignment state.
			if live {
				fillDossierTicketHolder(&view, s.discloseDossierHolder(ticket), customerID)
			}
			sale.Tickets = append(sale.Tickets, view)
		}
		if s.ticketAssignmentEnabled {
			sent := remindersBySale[sale.ID]
			if sent == nil {
				sent = []time.Time{}
			}
			sale.AssignmentReminderSentAt = &sent
		}
	}
	return nil
}

// fillDossierTicketHolder shapes one live Ticket's disclosure. The buyer's own
// acceptance is their Self-held Ticket and names nobody; anybody else's is
// named and linked. A closed flag discloses nothing, so nothing is filled.
func fillDossierTicketHolder(view *DossierTicketView, disclosure catalog.HolderDisclosure, customerID string) {
	view.AssignmentState = string(disclosure.State)
	view.NeverAccepted = disclosure.NeverAccepted
	if disclosure.HolderCustomerID == "" {
		return
	}
	if disclosure.HolderCustomerID == customerID {
		view.SelfHeld = true
		return
	}
	view.HolderCustomerID = disclosure.HolderCustomerID
	view.HolderFirstName = disclosure.HolderFirstName
	view.HolderLastName = disclosure.HolderLastName
}

// dossierHeldTickets returns the Tickets the Customer holds on other buyers'
// Sales of this Event. The query selects accepted Tickets naming the Customer,
// and each is kept only if DiscloseHolder discloses that very Customer, so the
// filter can never find somebody the rule would not name.
func (s *Service) dossierHeldTickets(
	ctx context.Context, actor ActorContext, eventID, customerID string,
) ([]DossierHeldTicketView, error) {
	rows, err := s.repo.ListDossierHeldTickets(ctx, actor.OrganizationID, eventID, customerID)
	if err != nil {
		return nil, err
	}
	views := make([]DossierHeldTicketView, 0, len(rows))
	for _, row := range rows {
		disclosure := s.discloseDossierHolder(row.DossierTicket)
		if disclosure.HolderCustomerID != customerID {
			continue
		}
		saleStatus := DossierSaleActive
		if row.SaleStatus == DossierSaleReversed {
			saleStatus = DossierSaleReversed
		}
		views = append(views, DossierHeldTicketView{
			TicketID:        row.ID,
			TicketTypeID:    row.TicketTypeID,
			TicketTypeName:  row.TicketTypeName,
			Ordinal:         row.Ordinal,
			TicketSaleID:    row.TicketSaleID,
			ConfirmationRef: row.ConfirmationRef,
			SaleStatus:      saleStatus,
			BuyerFirstName:  row.BuyerFirstName,
			BuyerLastName:   row.BuyerLastName,
			AcceptedAt:      disclosure.AcceptedAt,
			HolderFirstName: disclosure.HolderFirstName,
			HolderLastName:  disclosure.HolderLastName,
		})
	}
	return views, nil
}

// discloseDossierHolder is the Dossier's adapter onto catalog.DiscloseHolder,
// as fillHolderListEntry is the Holder List's.
func (s *Service) discloseDossierHolder(ticket repository.DossierTicket) catalog.HolderDisclosure {
	return catalog.DiscloseHolder(s.ticketAssignmentEnabled, catalog.HolderAssignment{
		HolderEmail:           ticket.HolderEmail.String,
		HolderFirstName:       ticket.HolderFirstName.String,
		HolderLastName:        ticket.HolderLastName.String,
		HolderCustomerID:      ticket.HolderCustomerID.String,
		AssignedAt:            nullTimeOrNil(ticket.AssignedAt),
		AcceptedAt:            nullTimeOrNil(ticket.AcceptedAt),
		HolderAddressPurgedAt: nullTimeOrNil(ticket.HolderAddressPurgedAt),
	})
}

// dossierSaleView shapes one Sale row for the Dossier.
func dossierSaleView(row repository.DossierSale) DossierSaleView {
	lines := make([]DossierSaleLineView, 0, len(row.TicketTypes))
	for _, line := range row.TicketTypes {
		lines = append(lines, DossierSaleLineView(line))
	}
	return DossierSaleView{
		ID:                        row.ID,
		ConfirmationRef:           row.ConfirmationRef,
		Status:                    dossierSaleStatus(row),
		ReversedAt:                row.ReversedAt,
		ReplacedByConfirmationRef: row.ReplacedByConfirmationRef,
		ReplacesConfirmationRef:   row.ReplacesConfirmationRef,
		ReplacementReason:         row.ReplacementReason,
		SoldAt:                    row.SoldAt,
		RecordedAt:                row.RecordedAt,
		Channel:                   row.Channel,
		Source:                    row.Source,
		Origin:                    sales.DeriveSaleOrigin(row.Channel, row.ImportBatchID, row.ReplacesSaleID),
		TicketTypes:               lines,
		AmountCents:               row.AmountCents,
		Currency:                  row.Currency,
		PaymentMethod:             row.PaymentMethod,
		CustomerFirstName:         row.CustomerFirstName,
		CustomerLastName:          row.CustomerLastName,
		TaxIDType:                 row.TaxIDType,
		TaxIDNumber:               row.TaxIDNumber,
	}
}

// dossierSaleStatus reads a Sale's status for the Dossier: a reversed Sale a
// correction replaced is `corrected`, one its buyer upgraded out of is
// `upgraded`, and a reversal with no replacement behind it is just `reversed`.
//
// THE REASON IS READ AND THE LINK IS NOT ENOUGH (#651, ADR 0074). Until the
// Upgrade there was one way to replace a Ticket Sale, so the link alone could
// stand for the reason; there are two now, and `replacement_reason` is the
// column that tells them apart (migration 123, written on both halves so no join
// is needed here).
//
// ONLY THE UPGRADE'S OWN WORD MOVES THIS OFF `corrected`. The fallthrough is
// deliberate rather than defensive: a link whose reason this build cannot read —
// a third reason added later, or a row written by an older binary — keeps the
// older, narrower sentence instead of being promoted to a claim about a buyer's
// election that nobody made.
func dossierSaleStatus(row repository.DossierSale) string {
	if row.Status != DossierSaleReversed {
		return DossierSaleActive
	}
	if row.ReplacedBySaleID == nil {
		return DossierSaleReversed
	}
	if sales.IsUpgradeReplacement(row.ReplacementReason) {
		return DossierSaleUpgraded
	}
	return DossierSaleCorrected
}
