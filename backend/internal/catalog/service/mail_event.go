package service

import (
	"context"
	"database/sql"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// MailEvent is what mail about an Event shows of it beside its name (spec
// #870, #878): its Organization's name, the Logo and Event email renditions
// as public URLs (an empty URL for one not rendered yet, so the mail shows no
// image there), and when and where it is. The zero value, and no error, for
// an Event that does not exist: the mail goes out without an Event row's
// extras rather than not at all.
//
// It reads across every Organization, since the mail is composed by sweeps
// and drains with no Organization in scope, and it is the one place the
// rendition keys become URLs for mail: the sales module, which sends most of
// these mails, holds no object storage.
func (s *Service) MailEvent(ctx context.Context, eventID string) (platform.MailEvent, error) {
	row, err := s.repo.GetMailEvent(ctx, eventID)
	if err != nil || row == nil {
		return platform.MailEvent{}, err
	}
	event := platform.MailEvent{
		OrganizationName: row.OrganizationName,
		LogoURL:          s.mailImageURL(row.EmailLogoKey),
		WideURL:          s.mailImageURL(row.EmailWideKey),
		SquareURL:        s.mailImageURL(row.EmailSquareKey),
		Timezone:         row.Timezone.String,
		VenueName:        row.VenueName.String,
	}
	if row.StartsAt.Valid {
		event.StartsAt = row.StartsAt.Time
	}
	return event, nil
}

// mailImageURL is a rendition's public URL, built like every other image the
// platform serves, or "" when it is not rendered yet.
func (s *Service) mailImageURL(key sql.NullString) string {
	if !key.Valid || key.String == "" || s.storage == nil {
		return ""
	}
	return s.storage.PublicURL(key.String)
}

// mailEvent is MailEvent for a mail this module sends itself. A failed read is
// logged and the mail goes out without the Event row's extras: imagery is
// never on a send's critical path. The log names the Event and nobody.
func (s *Service) mailEvent(ctx context.Context, eventID string) platform.MailEvent {
	if eventID == "" {
		return platform.MailEvent{}
	}
	event, err := s.MailEvent(ctx, eventID)
	if err != nil {
		s.logger.Warn("could not read what a mail shows of its Event; sending it without",
			"event_id", eventID, "error", err)
	}
	return event
}
