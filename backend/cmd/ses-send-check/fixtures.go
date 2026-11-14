package main

import (
	"context"
	"time"

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

// messages returns one fixture of every message type the platform sends,
// all addressed to to: every method of the transactional EmailSender on the
// transactional identity's sender, and the Follow Digest on the Digest
// identity's. The two Mail Locales alternate, so the operator reads both.
//
// A message type added to platform.EmailSender belongs here too; the test
// beside this file counts the interface's methods against this list.
func messages(to string, now time.Time, transactional platform.EmailSender, digest platform.DigestEmailSender) ([]message, error) {
	es, en := platform.LocaleES, platform.LocaleEN
	guayaquil := "America/Guayaquil"
	eventStartsAt := now.AddDate(0, 0, 9).Truncate(24 * time.Hour).Add(25 * time.Hour) // 20:00 in Guayaquil
	const event = "Noche de Jazz en el Sucre"
	const organization = "Jazz Club Quito"

	attachments, err := taxDocumentAttachments(now)
	if err != nil {
		return nil, err
	}

	t := func(name string, locale platform.Locale, send func(ctx context.Context) error) message {
		return message{name: name, identity: transactionalIdentity, locale: locale, send: send}
	}
	return []message{
		t("otp", es, func(ctx context.Context) error {
			return transactional.SendOTP(ctx, to, "482913", es)
		}),
		t("sale confirmation", en, func(ctx context.Context) error {
			return transactional.SendSaleConfirmation(ctx, platform.SaleConfirmation{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0042",
				AmountCents: 5000, Currency: "USD",
				ConfirmationLink:   fixtureStorefront + "/c/fixture-confirmation",
				SaleInvoiceFollows: true, Locale: en,
			})
		}),
		t("sale voided", es, func(ctx context.Context) error {
			return transactional.SendSaleVoided(ctx, platform.SaleVoided{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0043", Locale: es,
			})
		}),
		t("sale reversal refused", en, func(ctx context.Context) error {
			return transactional.SendSaleReversalRefused(ctx, platform.SaleReversalRefused{
				To: to, CustomerName: "Ana", EventName: event, Reference: "TS-2026-0044", Locale: en,
			})
		}),
		t("tax document delivery", es, func(ctx context.Context) error {
			return transactional.SendTaxDocumentDelivery(ctx, platform.TaxDocumentDelivery{
				To: to, Kind: "sale", CustomerName: "Ana López", EventName: event, Reference: "TS-2026-0042",
				CustomerAreaURL: fixtureStorefront + "/tickets#sale-fixture",
				Attachments:     attachments, Locale: es,
			})
		}),
		t("consent withdrawal confirmation", en, func(ctx context.Context) error {
			return transactional.SendConsentWithdrawalConfirmation(ctx, platform.ConsentWithdrawalConfirmation{
				To: to, Locale: en, MarketingConsent: true, NetworkingConsent: true,
			})
		}),
		t("holder answer reminder", es, func(ctx context.Context) error {
			return transactional.SendHolderAnswerReminder(ctx, platform.HolderAnswerReminder{
				To:              to,
				Tickets:         []platform.HolderAnswerReminderTicket{{EventName: event, TicketTypeName: "General"}},
				CustomerAreaURL: fixtureStorefront + "/tickets", Locale: es,
			})
		}),
		t("assignment reminder", en, func(ctx context.Context) error {
			return transactional.SendAssignmentReminder(ctx, platform.AssignmentReminder{
				To: to, FirstName: "Ana", EventName: event,
				EventStartsAt: eventStartsAt, EventTimezone: guayaquil,
				UnassignedTickets: 2, TotalTickets: 3,
				ConfirmationLink: fixtureStorefront + "/c/fixture-confirmation",
				SaleCreatedAt:    now.AddDate(0, 0, -3), Locale: en,
			})
		}),
		t("ticket assignment", es, func(ctx context.Context) error {
			return transactional.SendTicketAssignment(ctx, platform.TicketAssignment{
				To: to, EventName: event, TicketTypeName: "General",
				AcceptURL: fixtureStorefront + "/a/fixture-assignment", Locale: es,
			})
		}),
		t("no longer holding", en, func(ctx context.Context) error {
			return transactional.SendNoLongerHolding(ctx, platform.NoLongerHolding{To: to, EventName: event, Locale: en})
		}),
		t("sale re-addressing", es, func(ctx context.Context) error {
			return transactional.SendSaleReAddressing(ctx, platform.SaleReAddressing{
				To: to, EventName: event, Reference: "TS-2026-0045",
				AcceptURL: fixtureStorefront + "/r/fixture-readdressing", Locale: es,
			})
		}),
		t("ticket question revoked", en, func(ctx context.Context) error {
			return transactional.SendTicketQuestionRevoked(ctx, platform.TicketQuestionRevoked{
				To: to, Locale: en, OrganizationName: organization, EventName: event,
				QuestionLabel: "T-shirt size", Reason: "Collecting sizes is no longer needed for this Event.",
			})
		}),
		t("payout request submitted", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestSubmitted(ctx, platform.PayoutRequestSubmitted{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD", RequestedBy: "Luis Andrade",
			})
		}),
		t("question review submitted", en, func(ctx context.Context) error {
			return transactional.SendQuestionReviewSubmitted(ctx, platform.QuestionReviewSubmitted{
				To: to, Locale: en, OrganizationName: organization, EventName: event, SubmittedBy: "Luis Andrade", QuestionCount: 2,
			})
		}),
		t("question review answered", es, func(ctx context.Context) error {
			return transactional.SendQuestionReviewAnswered(ctx, platform.QuestionReviewAnswered{
				To: to, Locale: es, OrganizationName: organization, EventName: event, AnsweredBy: "Marta Vera",
				Items: []platform.QuestionReviewAnsweredItem{
					{QuestionLabel: "Talla de camiseta", Verdict: "approved"},
					{QuestionLabel: "Número de cédula", Verdict: "refused", Reason: "Pide un dato que el Evento no necesita."},
				},
			})
		}),
		t("payout request paid", en, func(ctx context.Context) error {
			return transactional.SendPayoutRequestPaid(ctx, platform.PayoutRequestPaid{
				To: to, Locale: en, OrganizationName: organization, AmountCents: 120000, RequestedCents: 120000, Currency: "USD",
			})
		}),
		t("payout request declined", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestDeclined(ctx, platform.PayoutRequestDeclined{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD",
				Reason: "Los datos bancarios no coinciden con el RUC de la Organización.",
			})
		}),
		t("payout request transfer sent", en, func(ctx context.Context) error {
			return transactional.SendPayoutRequestTransferSent(ctx, platform.PayoutRequestTransferSent{
				To: to, Locale: en, OrganizationName: organization, AmountCents: 120000, Currency: "USD", SubmittedAt: now,
			})
		}),
		t("payout request transfer failed", es, func(ctx context.Context) error {
			return transactional.SendPayoutRequestTransferFailed(ctx, platform.PayoutRequestTransferFailed{
				To: to, Locale: es, OrganizationName: organization, AmountCents: 120000, Currency: "USD",
				Reason: "La cuenta de destino está cerrada.",
			})
		}),
		t("certificate expiry warning", en, func(ctx context.Context) error {
			return transactional.SendCertificateExpiryWarning(ctx, platform.CertificateExpiryWarning{
				To: to, Locale: en, Threshold: 7, NotAfter: now.AddDate(0, 0, 7), RUC: "1790012345001",
				IssuerURL: fixtureStaff + "/operator/issuer",
			})
		}),
		{name: "follow digest", identity: digestIdentity, locale: es, send: func(ctx context.Context) error {
			return digest.SendFollowDigest(ctx, platform.FollowDigest{
				To: to, CustomerName: "Ana", Locale: es,
				New: []platform.FollowDigestEvent{{
					Name: event, StartsAt: eventStartsAt, Timezone: guayaquil,
					OrganizationName: organization, Venue: "Teatro Nacional Sucre",
					URL: fixtureStorefront + "/e/noche-de-jazz", MatchedOrganization: true,
				}},
				Happening: []platform.FollowDigestEvent{{
					Name: "Feria del Libro de Quito", StartsAt: now.AddDate(0, 0, 3), Timezone: guayaquil,
					OrganizationName: "Libros EC", Venue: "Centro de Convenciones Bicentenario",
					URL: fixtureStorefront + "/e/feria-del-libro", MatchedTagNames: []string{"Literatura"},
					Attending: true, TicketSaleURL: fixtureStorefront + "/tickets#sale-fixture",
				}},
				UnsubscribeURL: fixtureStorefront + "/u/fixture-unsubscribe",
			})
		}},
	}, nil
}
