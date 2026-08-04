package service

import (
	"context"
	"strings"
)

// RecordRegistrationClick counts one hand-off from an Event page to its
// Registration Link, resolved by the two Storefront slugs.
//
// It counts CLICKS, and the name is the whole contract: the platform hands the
// Customer to somebody else's site and never learns what happened there, so this
// number is neither registrations nor people. It is not deduplicated either, and
// that is deliberate — a Customer who comes back three times clicked three
// times, and a deduplicated figure would be just as wrong (cleared cookies,
// several devices, a shared machine) while licensing the misreading that the
// number counts people.
//
// An address that resolves to nothing — unknown Organization, unknown Event, or
// an Event that sells Ticket Types here — is a no-op rather than an error. The
// caller is somebody in the middle of navigating, and the Storefront route that
// calls this is about to redirect them whatever this answers.
//
// A storage failure is returned so it can be logged here, never shown: the count
// is display-only stats for an organizer, and a tracking problem must never stop
// someone registering.
func (s *Service) RecordRegistrationClick(ctx context.Context, organizationSlug, eventSlug string) error {
	organizationSlug = strings.TrimSpace(organizationSlug)
	eventSlug = strings.TrimSpace(eventSlug)
	if organizationSlug == "" || eventSlug == "" {
		return nil
	}
	if err := s.repo.RecordRegistrationClick(ctx, organizationSlug, eventSlug); err != nil {
		if s.logger != nil {
			s.logger.Error("registration link click not recorded",
				"organization_slug", organizationSlug, "event_slug", eventSlug, "error", err)
		}
		return err
	}
	return nil
}
