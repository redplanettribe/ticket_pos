package platform

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// What places a report: who the request acted for and what it concerns, as
// opaque ids only (ADR 0079). Never a name, slug, email or identity number;
// an opaque id is not something an erasure has to reach.
//
// A report made inside a request is placed by the request it belongs to,
// whichever of its sources made it (a panic, the error mapping, an Error
// line, the request line of a 5xx):
//
//   - the route pattern it was routed by, under "route", never the raw path;
//   - the Organization, Event and Ticket Sale ids in its path, read from the
//     pattern: a wildcard whose literal segment in front is "organizations",
//     "events", "sales" or "ticket-sales", the same key the path-id guard
//     uses (server.pathIDs), and whose value is a UUID, so a slug or a
//     Confirmation reference in the same place is never sent;
//   - what the authentication middleware supplied with SetReportActor and
//     SetReportTag: the person as a Member or Customer id with the audience,
//     and the Organization or Ticket Sale their session is for;
//   - what a handler supplied with SetReportTag, for an id it resolved that is
//     not in its path, such as the Event of a checkout addressed by slug.

// Audience is who a request acted for.
type Audience string

const (
	AudienceStaff    Audience = "staff"
	AudienceCustomer Audience = "customer"
)

// ReportTag is the key of an id-only tag a request's reports carry. The set is
// closed, so a tag can only ever be an id.
type ReportTag string

const (
	ReportTagOrganization ReportTag = "organization_id"
	ReportTagEvent        ReportTag = "event_id"
	ReportTagSale         ReportTag = "sale_id"
)

// The tags placement sets beside the ReportTags.
const (
	reportTagAudience = "audience"
	reportTagRoute    = "route"
)

// pathReportTags maps the literal segment in front of a path wildcard to the
// tag its value is.
var pathReportTags = map[string]ReportTag{
	"organizations": ReportTagOrganization,
	"events":        ReportTagEvent,
	"sales":         ReportTagSale,
	"ticket-sales":  ReportTagSale,
}

// SetReportActor records who the request on ctx acts for, so its reports
// carry the person's opaque id and the audience: a staff Member id, or a
// Customer id. An id that is not a UUID is not recorded, the audience still
// is. Outside a request it does nothing.
func SetReportActor(ctx context.Context, audience Audience, id string) {
	reports := requestReportsOn(ctx)
	if reports == nil {
		return
	}
	reports.mu.Lock()
	defer reports.mu.Unlock()
	reports.tags[reportTagAudience] = string(audience)
	reports.userID = ""
	if isUUID(id) {
		reports.userID = id
	}
}

// SetReportTag records an id the request on ctx concerns, so its reports
// carry it. A value that is not a UUID is not recorded: tags are ids only.
// Outside a request it does nothing.
func SetReportTag(ctx context.Context, tag ReportTag, id string) {
	reports := requestReportsOn(ctx)
	if reports == nil || !isUUID(id) {
		return
	}
	reports.mu.Lock()
	defer reports.mu.Unlock()
	reports.tags[string(tag)] = id
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}

// requestReports is what one request has reported, so that one failed request
// produces one event, and what places its reports (ADR 0079). The request
// pipeline puts it on the request's context; everything that reports inside
// the request finds it there.
type requestReports struct {
	reported atomic.Bool

	// request is the request as the pipeline handed it to the router, which
	// sets its pattern and path values on it before any handler runs. That
	// holds only while nothing between the pipeline and the ServeMux replaces
	// the request (r.WithContext): a middleware there would leave every
	// report "unrouted" and without its path ids. Nil when detached.
	request *http.Request

	mu     sync.Mutex
	userID string
	tags   map[string]string
}

type requestReportsKey struct{}

// withRequestReports is r carrying a fresh record of what it reports.
func withRequestReports(r *http.Request) *http.Request {
	reports := &requestReports{tags: map[string]string{}}
	r = r.WithContext(context.WithValue(r.Context(), requestReportsKey{}, reports))
	reports.request = r
	return r
}

// requestReportsOn is the request's record on ctx, nil outside a request.
func requestReportsOn(ctx context.Context) *requestReports {
	reports, _ := ctx.Value(requestReportsKey{}).(*requestReports)
	return reports
}

// DetachFromRequest is ctx for work that outlives the request it was started
// by, such as a fire-and-forget goroutine: not cancelled with the request, and
// still carrying its id and what places it, so what the work reports names the
// request; but no longer counted as the request's report, so it can never
// stand in for the request's own 5xx (ADR 0079).
func DetachFromRequest(ctx context.Context) context.Context {
	detached := &requestReports{tags: map[string]string{}}
	if reports := requestReportsOn(ctx); reports != nil {
		placed := reports.place(ErrorReport{})
		detached.userID, detached.tags = placed.UserID, placed.Tags
	}
	return context.WithValue(context.WithoutCancel(ctx), requestReportsKey{}, detached)
}

// place is report placed by the request: its user, and its tags over the
// report's own. Outside a request, report as it is.
func (reports *requestReports) place(report ErrorReport) ErrorReport {
	if reports == nil {
		return report
	}
	// The report's own tags first: what places the request is not theirs to
	// overwrite.
	tags := maps.Clone(report.Tags)
	if tags == nil {
		tags = map[string]string{}
	}
	if r := reports.request; r != nil {
		tags[reportTagRoute] = routePattern(r)
		for tag, id := range pathIDTags(r) {
			tags[string(tag)] = id
		}
	}
	reports.mu.Lock()
	maps.Copy(tags, reports.tags)
	if report.UserID == "" {
		report.UserID = reports.userID
	}
	reports.mu.Unlock()
	report.Tags = tags
	return report
}

// pathIDTags are the Organization, Event and Ticket Sale ids in r's path, read from
// the pattern it was routed by.
func pathIDTags(r *http.Request) map[ReportTag]string {
	pattern := r.Pattern
	if _, path, ok := strings.Cut(pattern, " "); ok {
		pattern = path
	}
	tags := map[ReportTag]string{}
	segments := strings.Split(pattern, "/")
	for i := 1; i < len(segments); i++ {
		tag, ok := pathReportTags[segments[i-1]]
		wildcard := segments[i]
		if !ok || !strings.HasPrefix(wildcard, "{") || !strings.HasSuffix(wildcard, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.Trim(wildcard, "{}"), "...")
		if value := r.PathValue(name); isUUID(value) {
			tags[tag] = value
		}
	}
	return tags
}
