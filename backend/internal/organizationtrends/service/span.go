package service

import "github.com/peter/ticket_pos/backend/internal/platform/trendspan"

// ResolveSpan resolves a span query against today on the service clock, in
// month mode: a span of up to 90 days is drawn in days, a longer one and all
// time in calendar months (see trendspan.Resolve). The caller fills
// query.Since for all time with the earlier of the Organization's creation
// and its earliest Ticket Sale's sale day. It reports false for a custom
// range with no closed day in it.
func (s *Service) ResolveSpan(query trendspan.Query) (trendspan.Span, bool) {
	return trendspan.Resolve(query, trendspan.GranularityMonth, s.now())
}
