package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// staffLocale is the language one staff mail is written in, a Payout Request
// notice or an Event Recap: the Staff Locale stored against the address it is
// going to, and English underneath (ADR 0041).
//
// EVERY FAILURE IS ENGLISH: an unconfigured reader, a read that failed and an
// address nobody has stated a language for are one outcome, and none of them
// is worth withholding a mail over. The floor itself lives in
// platform.ResolveStaffLocale rather than here: this module asks the question
// and does not decide what a missing answer means.
func (s *Service) staffLocale(ctx context.Context, email string) platform.Locale {
	if s.staffLocales == nil {
		return platform.DefaultLocale
	}
	stored, err := s.staffLocales.StaffLocale(ctx, email)
	if err != nil {
		s.logger.Error(ctx, "staff mail: read staff locale", "error", err)
		return platform.DefaultLocale
	}
	return platform.ResolveStaffLocale(stored)
}
