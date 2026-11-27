package main

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// identity names which sending identity a message leaves from (ADR 0030).
type identity string

const (
	transactionalIdentity identity = "transactional"
	digestIdentity        identity = "digest"
)

// message is one fixture of one message type, bound to the sender of the
// identity it leaves from. name is the message type as the SES sender names
// it in its errors and logs.
type message struct {
	name     string
	identity identity
	locale   platform.Locale
	send     func(ctx context.Context) error
}

// The links the fixtures carry. They are fixtures too: they look like the
// links the platform sends but lead nowhere, so the operator reads the mail
// without anything in it acting on a real Ticket Sale.
const (
	fixtureStorefront = "https://tickets.example.com"
	fixtureStaff      = "https://staff.example.com"
)

// The images the fixtures' Event rows and Cover Image blocks show. The check
// reads no database, so it has no Event's renditions to point at: these stand
// in for them with the production Storefront's own icons, which load in any
// inbox, so the operator sees the square Thumbnail cell, the Logo tile (#878,
// and the Follow Digest's first entry, #880) and the full-bleed Cover Image
// (#879) filled. The Storefront publishes no
// wide image, so the 512px icon stands in for the wide rendition too; the
// block sets its width, so it fills the card as a real one does.
const (
	fixtureSquareURL = "https://discover.multiticketing.com/icon-512.png"
	fixtureWideURL   = "https://discover.multiticketing.com/icon-512.png"
	fixtureLogoURL   = "https://discover.multiticketing.com/icon-192.png"
)

// messages returns one fixture of every message type the platform sends,
// all addressed to to: every method of the transactional EmailSender on the
// transactional identity's sender, and the Follow Digest on the Digest
// identity's. Spanish and English alternate, so the operator reads both
// languages across the set: a Customer's Mail Locale on Customer mail, a
// Member's or Platform Operator's Staff Locale on staff mail.
//
// A message type added to platform.EmailSender belongs here too; the test
// beside this file counts the interface's methods against this list.
func messages(to string, now time.Time, transactional platform.EmailSender, digest platform.DigestEmailSender) ([]message, error) {
	es, en := platform.LocaleES, platform.LocaleEN
	guayaquil := "America/Guayaquil"
	eventStartsAt := now.AddDate(0, 0, 9).Truncate(24 * time.Hour).Add(25 * time.Hour) // 20:00 in Guayaquil
	const event = "Noche de Jazz en el Sucre"
	const organization = "Jazz Club Quito"
	// What the Event rows show of the Event beside its name.
	eventRow := platform.MailEvent{
		OrganizationName: organization, LogoURL: fixtureLogoURL, SquareURL: fixtureSquareURL, WideURL: fixtureWideURL,
		StartsAt: eventStartsAt, Timezone: guayaquil, VenueName: "Teatro Nacional Sucre",
	}

	attachments, err := taxDocumentAttachments(now)
	if err != nil {
		return nil, err
	}

	transactionalMessage := func(name string, locale platform.Locale, send func(ctx context.Context) error) message {
		return message{name: name, identity: transactionalIdentity, locale: locale, send: send}
	}
	return []message{
		transactionalMessage("otp", es, func(ctx context.Context) error {
			return transactional.SendOTP(ctx, to, "482913", es)
		}),
		transactionalMessage("sale confirmation", en, func(ctx context.Context) error {
			return transactional.SendSaleConfirmation(ctx, platform.SaleConfirmation{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0042",
				AmountCents: 5000, Currency: "USD",
				ConfirmationLink:        fixtureStorefront + "/c/fixture-confirmation",
				ConsentConfirmationLink: fixtureStorefront + "/consent/fixture-confirmation",
				TaxID:                   platform.SaleTaxID{Type: platform.TaxIDTypeCedula, Number: "1712345675"},
				SaleInvoiceFollows:      true, Event: eventRow, Locale: en,
			})
		}),
		transactionalMessage("sale voided", es, func(ctx context.Context) error {
			return transactional.SendSaleVoided(ctx, platform.SaleVoided{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0043", Event: eventRow, Locale: es,
			})
		}),
		transactionalMessage("sale reversal refused", en, func(ctx context.Context) error {
			return transactional.SendSaleReversalRefused(ctx, platform.SaleReversalRefused{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0044", Event: eventRow, Locale: en,
			})
		}),
		transactionalMessage("tax document delivery", es, func(ctx context.Context) error {
			return transactional.SendTaxDocumentDelivery(ctx, platform.TaxDocumentDelivery{
				To: to, Kind: string(invoicing.DocumentKindSale), CustomerName: "Ana López", EventName: event, Reference: "TS-2026-0042",
				CustomerAreaURL: fixtureStorefront + "/tickets#sale-fixture",
				Attachments:     attachments, Locale: es,
			})
		}),
		transactionalMessage("annulment notice", en, func(ctx context.Context) error {
			return transactional.SendAnnulmentNotice(ctx, platform.AnnulmentNotice{
				To: to, CustomerName: "Ana López", EventName: event, Reference: "TS-2026-0042",
				Number: "001-001-000000042", CustomerAreaURL: fixtureStorefront + "/tickets#sale-fixture", Locale: en,
			})
		}),
		transactionalMessage("consent withdrawal confirmation", en, func(ctx context.Context) error {
			return transactional.SendConsentWithdrawalConfirmation(ctx, platform.ConsentWithdrawalConfirmation{
				To: to, Locale: en, MarketingConsent: true, NetworkingConsent: true,
			})
		}),
		transactionalMessage("holder answer reminder", es, func(ctx context.Context) error {
			return transactional.SendHolderAnswerReminder(ctx, platform.HolderAnswerReminder{
				To:              to,
				Tickets:         []platform.HolderAnswerReminderTicket{{EventName: event, TicketTypeName: "General", Event: eventRow}},
				CustomerAreaURL: fixtureStorefront + "/tickets", Locale: es,
			})
		}),
		transactionalMessage("assignment reminder", en, func(ctx context.Context) error {
			return transactional.SendAssignmentReminder(ctx, platform.AssignmentReminder{
				To: to, FirstName: "Ana", EventName: event,
				EventStartsAt: eventStartsAt, EventTimezone: guayaquil, Event: eventRow,
				UnassignedTickets: 2, TotalTickets: 3,
				ConfirmationLink: fixtureStorefront + "/c/fixture-confirmation",
				SaleCreatedAt:    now.AddDate(0, 0, -3), Locale: en,
			})
		}),
		transactionalMessage("ticket assignment", es, func(ctx context.Context) error {
			return transactional.SendTicketAssignment(ctx, platform.TicketAssignment{
				To: to, EventName: event, TicketTypeName: "General",
				AcceptURL: fixtureStorefront + "/a/fixture-assignment", Event: eventRow, Locale: es,
			})
		}),
		transactionalMessage("no longer holding", en, func(ctx context.Context) error {
			return transactional.SendNoLongerHolding(ctx, platform.NoLongerHolding{To: to, EventName: event, Event: eventRow, Locale: en})
		}),
		transactionalMessage("sale re-addressing", es, func(ctx context.Context) error {
			return transactional.SendSaleReAddressing(ctx, platform.SaleReAddressing{
				To: to, EventName: event, Reference: "TS-2026-0045",
				AcceptURL: fixtureStorefront + "/r/fixture-readdressing", Event: eventRow, Locale: es,
			})
		}),
		transactionalMessage("ticket question revoked", en, func(ctx context.Context) error {
			return transactional.SendTicketQuestionRevoked(ctx, platform.TicketQuestionRevoked{
				To: to, Locale: en, OrganizationName: organization, EventName: event,
				QuestionLabel: "T-shirt size", Reason: "Collecting sizes is no longer needed for this Event.",
			})
		}),
		transactionalMessage("payout request submitted", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestSubmitted(ctx, platform.PayoutRequestSubmitted{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD", RequestedBy: "Luis Andrade",
			})
		}),
		transactionalMessage("question review submitted", en, func(ctx context.Context) error {
			return transactional.SendQuestionReviewSubmitted(ctx, platform.QuestionReviewSubmitted{
				To: to, Locale: en, OrganizationName: organization, EventName: event, SubmittedBy: "Luis Andrade", QuestionCount: 2,
			})
		}),
		transactionalMessage("question review answered", es, func(ctx context.Context) error {
			return transactional.SendQuestionReviewAnswered(ctx, platform.QuestionReviewAnswered{
				To: to, Locale: es, OrganizationName: organization, EventName: event, AnsweredBy: "Marta Vera",
				Items: []platform.QuestionReviewAnsweredItem{
					{QuestionLabel: "Talla de camiseta", Verdict: "approved"},
					{QuestionLabel: "Número de cédula", Verdict: "refused", Reason: "Pide un dato que el Evento no necesita."},
				},
			})
		}),
		transactionalMessage("payout request paid", en, func(ctx context.Context) error {
			return transactional.SendPayoutRequestPaid(ctx, platform.PayoutRequestPaid{
				To: to, Locale: en, OrganizationName: organization, AmountCents: 120000, RequestedCents: 120000, Currency: "USD",
			})
		}),
		transactionalMessage("payout request declined", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestDeclined(ctx, platform.PayoutRequestDeclined{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD",
				Reason: "Los datos bancarios no coinciden con el RUC de la Organización.",
			})
		}),
		transactionalMessage("payout request transfer sent", en, func(ctx context.Context) error {
			return transactional.SendPayoutRequestTransferSent(ctx, platform.PayoutRequestTransferSent{
				To: to, Locale: en, OrganizationName: organization, AmountCents: 120000, Currency: "USD", SubmittedAt: now,
			})
		}),
		transactionalMessage("payout request transfer failed", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestTransferFailed(ctx, platform.PayoutRequestTransferFailed{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD",
				Reason: "La cuenta de destino está cerrada.",
			})
		}),
		transactionalMessage("certificate expiry warning", en, func(ctx context.Context) error {
			return transactional.SendCertificateExpiryWarning(ctx, platform.CertificateExpiryWarning{
				To: to, Locale: en, Threshold: 7, NotAfter: now.AddDate(0, 0, 7), RUC: "1790012345001",
				IssuerURL: fixtureStaff + "/operator/issuer",
			})
		}),
		transactionalMessage("event recap", es, func(ctx context.Context) error {
			const recapEvent = fixtureStaff + "/events/6f1c2a9e-0b7d-4c1e-9a53-2d8e4f7b1c60"
			return transactional.SendEventRecap(ctx, platform.EventRecap{
				To: to, Locale: es, EventName: event, SentAt: now,
				SalesTrendsURL: recapEvent + "/sales/trends", ReachURL: recapEvent + "/reach", SalesURL: recapEvent + "/sales",
				Sales: &platform.EventRecapSales{
					Currency:    "USD",
					TicketsSold: 1284, TicketSales: 713,
					TakingsCents: 2345050, NetProceedsCents: 1650000,
					ReversedSales: 2,
					Channels: []platform.EventRecapChannel{
						{Channel: "online", TicketsSold: 1100, TicketSales: 640},
						{Channel: "in_person", TicketsSold: 150, TicketSales: 60},
						{Channel: "import", TicketsSold: 34, TicketSales: 13},
					},
					TicketTypes: []platform.EventRecapTicketType{
						{Name: "General", TicketsSold: 1004},
						{Name: "Palco", TicketsSold: 280},
						{Name: "Cortesía", TicketsSold: 0},
					},
				},
			})
		}),
		{name: "follow digest", identity: digestIdentity, locale: es, send: func(ctx context.Context) error {
			return digest.SendFollowDigest(ctx, platform.FollowDigest{
				To: to, CustomerName: "Ana", Locale: es,
				New: []platform.FollowDigestEvent{{
					Name: event, StartsAt: eventStartsAt, Timezone: guayaquil,
					OrganizationName: organization, Venue: "Teatro Nacional Sucre",
					URL: fixtureStorefront + "/e/noche-de-jazz", MatchedOrganization: true,
					SquareURL: fixtureSquareURL, LogoURL: fixtureLogoURL,
				}},
				// The second entry has no renditions, so the inbox check sees an
				// Event row with its image cell dropped beside a full one.
				Happening: []platform.FollowDigestEvent{{
					Name: "Feria del Libro de Quito", StartsAt: now.AddDate(0, 0, 3), Timezone: guayaquil,
					OrganizationName: "Libros EC", Venue: "Centro de Convenciones Bicentenario",
					URL: fixtureStorefront + "/e/feria-del-libro", MatchedTagNames: []string{"Literatura"},
					Attending: true, TicketSaleURL: fixtureStorefront + "/tickets#sale-fixture",
				}},
				NewOverflow:    platform.FollowDigestOverflow{Count: 4, URL: fixtureStorefront + "/?tags=jazz"},
				UnsubscribeURL: fixtureStorefront + "/u/fixture-unsubscribe",
				ExploreURL:     fixtureStorefront + "/",
			})
		}},
	}, nil
}
