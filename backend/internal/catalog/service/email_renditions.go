package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// Email renditions (spec #870, #876): each Event's imagery as mail carries it,
// produced on the server from the originals so no mail ships a WebP or a
// multi-megabyte photo. See storage.RenderEmailWide and RenderEmailSquare for
// what each one is.
//
// BEST-EFFORT, ALWAYS. A rendition is produced after the save that changed its
// original has committed, and a failure is logged and leaves it null, which
// mail reads as "no image". Nothing here can fail an Organizer's save: mail
// imagery is never on the critical path. The rendition fill picks up whatever
// is still null.

// emailRenditionTimeout bounds the renders after one save. They run inside the
// save's request, after its commit and before its answer, so the Organizer
// waits for them (a read, a decode and an encode per rendition, well under a
// second for a Cover Image the staff form produces). They are detached from
// the request's cancellation, so a client hanging up does not abandon a
// half-made rendition, and this bound is what stops a slow bucket holding the
// request open.
const emailRenditionTimeout = time.Minute

// emailRenditionFillBudget is how long one fill run keeps starting Events,
// inside Cloud Run's 300s request timeout (api_request_timeout_seconds) with
// room for the Event in hand to finish.
const emailRenditionFillBudget = 4 * time.Minute

// emailRenditionFillPage is how many Events the fill reads at a time.
const emailRenditionFillPage = 50

// emailRenditionSlot is one of an Event's two renditions.
type emailRenditionSlot struct {
	name string
	// source is the key of the original the rendition derives from, or "" for
	// none.
	source func(e *repository.Event) string
	// current is the rendition key on the row.
	current func(e *repository.Event) sql.NullString
	render  func(io.Reader) ([]byte, error)
	key     func(organizationID, eventID string) string
	record  func(r *repository.Repository, ctx context.Context, eventID, source, key string) (bool, error)
}

// label names the slot in a media cleanup log line.
func (slot emailRenditionSlot) label() string {
	return "email " + slot.name + " rendition"
}

var emailRenditionSlots = []emailRenditionSlot{
	{
		name:    "wide",
		source:  func(e *repository.Event) string { return validKey(e.CoverImageKey) },
		current: func(e *repository.Event) sql.NullString { return e.EmailWideKey },
		render:  storage.RenderEmailWide,
		key:     storage.BuildEmailWideObjectKey,
		record:  (*repository.Repository).RecordEmailWideRendition,
	},
	{
		// The Thumbnail, else the Cover Image cropped to its centre: the
		// Storefront's own fallback.
		name: "square",
		source: func(e *repository.Event) string {
			if k := validKey(e.ThumbnailKey); k != "" {
				return k
			}
			return validKey(e.CoverImageKey)
		},
		current: func(e *repository.Event) sql.NullString { return e.EmailSquareKey },
		render:  storage.RenderEmailSquare,
		key:     storage.BuildEmailSquareObjectKey,
		record:  (*repository.Repository).RecordEmailSquareRendition,
	},
}

func validKey(k sql.NullString) string {
	if !k.Valid {
		return ""
	}
	return k.String
}

// refreshEmailRenditions follows a committed Event save: it deletes every
// rendition the save nulled (UpdateEvent nulls a rendition whose original
// changed) and renders each one whose original was set or replaced.
func (s *Service) refreshEmailRenditions(ctx context.Context, before, after *repository.Event) {
	if s.storage == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emailRenditionTimeout)
	defer cancel()
	for _, slot := range emailRenditionSlots {
		s.deleteReplacedObject(ctx, slot.current(before), slot.current(after), slot.label(), after.ID)
		if source := slot.source(after); source != "" && source != slot.source(before) {
			_, _ = s.renderEmailRendition(ctx, after, slot)
		}
	}
}

// renderEmailRendition reads the slot's original, renders it, stores it under
// a new key and records the key on the Event. It reports whether the row took
// the key: false with no error means the Event moved on while it rendered.
// Any failure is logged and returned, and leaves the rendition null.
func (s *Service) renderEmailRendition(ctx context.Context, e *repository.Event, slot emailRenditionSlot) (bool, error) {
	source := slot.source(e)
	fail := func(step string, err error) (bool, error) {
		if s.logger != nil {
			s.logger.Error(ctx, "email rendition: the rendition could not be produced and stays empty",
				"slot", slot.name, "step", step, "event_id", e.ID, "source_key", source, "error", err)
		}
		return false, fmt.Errorf("%s rendition %s: %w", slot.name, step, err)
	}

	body, err := s.storage.Get(ctx, source)
	if err != nil {
		return fail("read original", err)
	}
	data, err := slot.render(body)
	_ = body.Close()
	if err != nil {
		return fail("render", err)
	}

	key := slot.key(e.OrganizationID, e.ID)
	if err := s.storage.Put(ctx, key, storage.EmailRenditionContentType, bytes.NewReader(data)); err != nil {
		return fail("store", err)
	}
	recorded, err := slot.record(s.repo, ctx, e.ID, source, key)
	if err != nil || !recorded {
		// The Event has moved on (a later save changed the original, or a
		// concurrent render recorded first), or the write failed: the object
		// just stored is referenced by nothing.
		s.deleteReplacedObject(ctx, sql.NullString{String: key, Valid: true}, sql.NullString{}, slot.label(), e.ID)
		if err != nil {
			return fail("record", err)
		}
	}
	return recorded, nil
}

// EmailRenditionFillResult reports one run of the rendition fill.
type EmailRenditionFillResult struct {
	// Rendered counts the renditions produced and recorded by this run.
	Rendered int `json:"rendered"`
	// Failed counts the renditions this run could not produce. Each stays
	// null, its cause is logged, and the next run tries it again.
	Failed int `json:"failed"`
	// Remaining counts the Events and the Organizations still holding an
	// original with a null rendition after this run: the failures, and
	// anything the run's time budget did not reach.
	Remaining int `json:"remaining"`
	// Complete is false when the run stopped at its time budget with Events
	// or Logos left unvisited; run it again.
	Complete bool `json:"complete"`
}

// EmailLogoRenditions fills the Organization Logo renditions (#877), which the
// identity module owns since it owns the Organization and its Logo. The fill
// the catalog serves runs it beside the Event renditions, inside the same
// time budget, so one command fills both.
type EmailLogoRenditions interface {
	FillEmailLogoRenditions(ctx context.Context, deadline time.Time) (storage.EmailRenditionCounts, error)
}

// WithEmailLogoRenditions sets who fills the Logo renditions. Identity is built
// before the catalog service but reads the catalog's repository, so this is set
// once both exist. Without it the fill covers Events alone.
func (s *Service) WithEmailLogoRenditions(logos EmailLogoRenditions) *Service {
	s.emailLogos = logos
	return s
}

// FillEmailRenditions renders every Event original and every Organization
// Logo whose rendition is null, across every Organization. Idempotent and safe to rerun: what is rendered is
// skipped, so a second run after a clean one renders nothing.
//
// It visits each Event once per run, in id order, so an original that cannot
// be rendered is counted as failed and passed over rather than retried in a
// loop; and it stops starting Events after emailRenditionFillBudget, saying
// so, so a large catalogue fills over several runs inside the request timeout.
func (s *Service) FillEmailRenditions(ctx context.Context) (*EmailRenditionFillResult, error) {
	result := &EmailRenditionFillResult{Complete: true}
	if s.storage == nil {
		return result, nil
	}
	deadline := time.Now().Add(emailRenditionFillBudget)
	after := ""
	for {
		events, err := s.repo.ListEventsMissingEmailRenditions(ctx, after, emailRenditionFillPage)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		for i := range events {
			if time.Now().After(deadline) {
				result.Complete = false
				break
			}
			e := &events[i]
			after = e.ID
			for _, slot := range emailRenditionSlots {
				if slot.source(e) == "" || slot.current(e).Valid {
					continue
				}
				recorded, err := s.renderEmailRendition(ctx, e, slot)
				if err != nil {
					result.Failed++
					continue
				}
				if recorded {
					result.Rendered++
				}
			}
		}
		if !result.Complete {
			break
		}
	}
	remaining, err := s.repo.CountEventsMissingEmailRenditions(ctx)
	if err != nil {
		return nil, err
	}
	result.Remaining = remaining
	if s.emailLogos != nil {
		// Past the deadline it starts no Logo, but still counts what remains.
		logos, err := s.emailLogos.FillEmailLogoRenditions(ctx, deadline)
		if err != nil {
			return nil, err
		}
		result.Rendered += logos.Rendered
		result.Failed += logos.Failed
		result.Remaining += logos.Remaining
		result.Complete = result.Complete && logos.Complete
	}
	if s.logger != nil {
		s.logger.Info("email rendition fill ran",
			"rendered", result.Rendered, "failed", result.Failed, "remaining", result.Remaining, "complete", result.Complete)
	}
	return result, nil
}
