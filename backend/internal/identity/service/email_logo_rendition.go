package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// The Logo email rendition (spec #870, #877): an Organization's Logo as mail
// carries it, produced on the server from the original (see
// storage.RenderEmailLogo), on the terms the Event renditions keep in the
// catalog module.
//
// BEST-EFFORT, ALWAYS. The rendition is produced after the save that changed
// the Logo has committed, and a failure is logged and leaves it null, which
// mail reads as "no Logo". Nothing here can fail an Org Admin's save. The
// rendition fill picks up whatever is still null.

// emailLogoRenditionTimeout bounds the render after one save. It runs inside
// the save's request, after the write and before the answer, detached from the
// request's cancellation so a client hanging up does not abandon it.
const emailLogoRenditionTimeout = time.Minute

// emailLogoFillPage is how many Organizations the fill reads at a time.
const emailLogoFillPage = 50

// refreshEmailLogoRendition follows a committed Logo save: it deletes the
// rendition the save nulled (UpdateOrganizationLogoKey nulls it whenever the
// Logo changes) and renders the new Logo when one was set or replaced.
func (s *Service) refreshEmailLogoRendition(ctx context.Context, organizationID string, before repository.LogoKeys, after sql.NullString) {
	if s.storage == nil || before.Logo == after {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emailLogoRenditionTimeout)
	defer cancel()
	if before.EmailLogo.Valid && before.EmailLogo.String != "" {
		s.deleteEmailLogoObject(ctx, organizationID, before.EmailLogo.String)
	}
	if after.Valid && after.String != "" {
		_, _ = s.renderEmailLogo(ctx, organizationID, after.String)
	}
}

// renderEmailLogo reads the Logo, renders it, stores it under a new key and
// records the key on the Organization. It reports whether the row took the
// key: false with no error means the Organization moved on while it rendered.
// Any failure is logged and returned, and leaves the rendition null.
func (s *Service) renderEmailLogo(ctx context.Context, organizationID, source string) (bool, error) {
	fail := func(step string, err error) (bool, error) {
		if s.logger != nil {
			s.logger.Error(ctx, "email rendition: the rendition could not be produced and stays empty",
				"slot", "logo", "step", step, "organization_id", organizationID, "source_key", source, "error", err)
		}
		return false, fmt.Errorf("logo rendition %s: %w", step, err)
	}

	body, err := s.storage.Get(ctx, source)
	if err != nil {
		return fail("read original", err)
	}
	data, err := storage.RenderEmailLogo(body)
	_ = body.Close()
	if err != nil {
		return fail("render", err)
	}

	key := storage.BuildEmailLogoObjectKey(organizationID)
	if err := s.storage.Put(ctx, key, storage.EmailLogoContentType, bytes.NewReader(data)); err != nil {
		return fail("store", err)
	}
	recorded, err := s.repo.RecordEmailLogoRendition(ctx, organizationID, source, key)
	if err != nil || !recorded {
		// The Organization has moved on (a later save changed the Logo, or a
		// concurrent render recorded first), or the write failed: the object
		// just stored is referenced by nothing.
		s.deleteEmailLogoObject(ctx, organizationID, key)
		if err != nil {
			return fail("record", err)
		}
	}
	return recorded, nil
}

// deleteEmailLogoObject deletes a Logo rendition nothing references any more,
// best-effort: a failure is logged and leaves an orphan, as ADR 0020 accepts.
func (s *Service) deleteEmailLogoObject(ctx context.Context, organizationID, key string) {
	if err := s.storage.Delete(ctx, key); err != nil && s.logger != nil {
		s.logger.Error(ctx, "media cleanup: the replaced object could not be deleted and is now an orphan",
			"kind", "email logo rendition", "organization_id", organizationID, "object_key", key, "error", err)
	}
}

// FillEmailLogoRenditions renders every Logo whose rendition is null, across
// every Organization, until deadline: the Logo share of the rendition fill the
// catalog serves. Idempotent: what is rendered is skipped. It visits each
// Organization once per run, in id order, so a Logo that cannot be rendered is
// counted as failed and passed over rather than retried in a loop.
func (s *Service) FillEmailLogoRenditions(ctx context.Context, deadline time.Time) (storage.EmailRenditionCounts, error) {
	counts := storage.EmailRenditionCounts{Complete: true}
	if s.storage == nil {
		return counts, nil
	}
	after := ""
	for counts.Complete {
		logos, err := s.repo.ListOrganizationsMissingEmailLogo(ctx, after, emailLogoFillPage)
		if err != nil {
			return counts, err
		}
		if len(logos) == 0 {
			break
		}
		for _, logo := range logos {
			// Wall-clock, not s.now: the deadline is the catalog's wall-clock
			// budget inside the request timeout, not a domain instant.
			if time.Now().After(deadline) {
				counts.Complete = false
				break
			}
			after = logo.OrganizationID
			recorded, err := s.renderEmailLogo(ctx, logo.OrganizationID, logo.LogoKey)
			switch {
			case err != nil:
				counts.Failed++
			case recorded:
				counts.Rendered++
			}
		}
	}
	remaining, err := s.repo.CountOrganizationsMissingEmailLogo(ctx)
	if err != nil {
		return counts, err
	}
	counts.Remaining = remaining
	return counts, nil
}
