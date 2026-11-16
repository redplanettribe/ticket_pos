package platform_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

const (
	memberID   = "0b6f4a52-3c1e-4c8e-9d43-2f1a7e5b9c10"
	customerID = "7d2e9c41-5a6b-4f30-8e1d-9c3b2a1f0e57"
	orgID      = "a3c5e7f9-1b2d-4e6f-8a0c-2e4f6a8c0e1b"
	eventID    = "5e8a1c3d-7f9b-4d2e-a6c8-0b1d3f5a7c9e"
	saleID     = "c1d3e5f7-a9b1-4c3d-8e5f-7a9b1c3d5e7f"
	ticketID   = "9f8e7d6c-5b4a-4321-8fed-cba987654321"
)

// failing is a handler that ends its request in an unexpected 500.
func failing(w http.ResponseWriter, r *http.Request) {
	_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errConnectionRefused)
}

// onlyReport is the one report the fake received.
func onlyReport(t *testing.T, fake *reportertest.Reporter) platform.ErrorReport {
	t.Helper()
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	return reports[0]
}

// reported is everything a report holds, as text.
func reported(report platform.ErrorReport) string {
	return fmt.Sprintf("%+v %s", report, report.Err)
}

func TestAStaffRequestsReportCarriesTheMemberIDAndTheStaffAudience(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
		mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
			platform.SetReportActor(r.Context(), platform.AudienceStaff, memberID)
			platform.SetReportTag(r.Context(), platform.ReportTagOrganization, orgID)
			failing(w, r)
		})
	})

	get(t, server, "/fail")

	report := onlyReport(t, fake)
	if report.UserID != memberID {
		t.Errorf("user = %q, want the Member id %q", report.UserID, memberID)
	}
	if report.Tags["audience"] != "staff" || report.Tags["organization_id"] != orgID {
		t.Errorf("tags = %v, want audience staff and the Organization id", report.Tags)
	}
}

func TestACustomerRequestsReportCarriesTheCustomerIDAndTheCustomerAudience(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, logger platform.Logger) {
		mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
			platform.SetReportActor(r.Context(), platform.AudienceCustomer, customerID)
			// An Error line is placed by its request like the 5xx is.
			logger.Error(r.Context(), "could not load the Customer Area", "error", errConnectionRefused)
			failing(w, r)
		})
	})

	get(t, server, "/fail")

	report := onlyReport(t, fake)
	if report.UserID != customerID || report.Tags["audience"] != "customer" {
		t.Errorf("user, audience = %q, %q; want %q, customer", report.UserID, report.Tags["audience"], customerID)
	}
}

func TestAReportCarriesTheRoutePatternAndTheIDsInItsPathButNeverThePath(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		wantTags map[string]string
		// absent must appear nowhere in the report.
		absent []string
	}{
		{
			name:     "an Event and its Ticket Sale",
			pattern:  "GET /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets",
			path:     "/api/v1/staff/events/" + eventID + "/ticket-sales/" + saleID + "/tickets",
			wantTags: map[string]string{"route": "GET /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets", "event_id": eventID, "sale_id": saleID},
		},
		{
			name:     "a Sale under an Event, and a Ticket that is not tagged",
			pattern:  "POST /api/v1/staff/events/{id}/sales/{saleId}/tickets/{ticketId}",
			path:     "/api/v1/staff/events/" + eventID + "/sales/" + saleID + "/tickets/" + ticketID,
			wantTags: map[string]string{"route": "POST /api/v1/staff/events/{id}/sales/{saleId}/tickets/{ticketId}", "event_id": eventID, "sale_id": saleID},
			absent:   []string{ticketID},
		},
		{
			name:     "an Organization",
			pattern:  "PUT /api/v1/operator/organizations/{orgID}/house",
			path:     "/api/v1/operator/organizations/" + orgID + "/house",
			wantTags: map[string]string{"route": "PUT /api/v1/operator/organizations/{orgID}/house", "organization_id": orgID},
		},
		{
			name:     "slugs are never tagged",
			pattern:  "POST /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout",
			path:     "/api/v1/customer/organizations/ana-perez-producciones/events/fiesta-de-ana/checkout",
			wantTags: map[string]string{"route": "POST /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout"},
			absent:   []string{"ana-perez-producciones", "fiesta-de-ana"},
		},
		{
			name:     "a Confirmation reference is never tagged",
			pattern:  "POST /api/v1/operator/sales/{confirmationRef}/reverse",
			path:     "/api/v1/operator/sales/TKT-7F3K9Q/reverse",
			wantTags: map[string]string{"route": "POST /api/v1/operator/sales/{confirmationRef}/reverse"},
			absent:   []string{"TKT-7F3K9Q"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
				mux.HandleFunc(tt.pattern, failing)
			})
			method, _, _ := strings.Cut(tt.pattern, " ")
			req, _ := http.NewRequest(method, server.URL+tt.path+"?email=ana@example.com", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			report := onlyReport(t, fake)
			if report.RequestID == "" || report.RequestID != resp.Header.Get(platform.RequestIDHeader) {
				t.Errorf("request id = %q, want the response's %q", report.RequestID, resp.Header.Get(platform.RequestIDHeader))
			}
			if fmt.Sprint(report.Tags) != fmt.Sprint(tt.wantTags) {
				t.Errorf("tags = %v, want %v", report.Tags, tt.wantTags)
			}
			for _, leaked := range append(tt.absent, tt.path, "ana@example.com") {
				if strings.Contains(reported(report), leaked) {
					t.Errorf("the report carries %q: %s", leaked, reported(report))
				}
			}
		})
	}
}

func TestTheRequestLineOfAHandWritten5xxIsPlacedToo(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
		mux.HandleFunc("GET /api/v1/staff/events/{id}", func(w http.ResponseWriter, r *http.Request) {
			platform.SetReportActor(r.Context(), platform.AudienceStaff, memberID)
			w.WriteHeader(http.StatusBadGateway)
		})
	})

	get(t, server, "/api/v1/staff/events/"+eventID)

	report := onlyReport(t, fake)
	if report.Err.Error() != "GET /api/v1/staff/events/{id} answered 502" {
		t.Errorf("reported %q", report.Err)
	}
	if report.UserID != memberID || report.Tags["event_id"] != eventID || report.Tags["route"] != "GET /api/v1/staff/events/{id}" {
		t.Errorf("user, tags = %q, %v", report.UserID, report.Tags)
	}
}

func TestOnlyIDsArePlaced(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
		mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
			platform.SetReportActor(r.Context(), platform.AudienceCustomer, "ana@example.com")
			platform.SetReportTag(r.Context(), platform.ReportTagOrganization, "ana-perez-producciones")
			platform.SetReportTag(r.Context(), platform.ReportTagSale, "1710034065")
			platform.SetReportTag(r.Context(), platform.ReportTagEvent, "{"+eventID+"}")
			failing(w, r)
		})
	})

	get(t, server, "/fail")

	report := onlyReport(t, fake)
	if report.UserID != "" {
		t.Errorf("user = %q, want none for a value that is not an id", report.UserID)
	}
	want := map[string]string{"route": "GET /fail", "audience": "customer"}
	if fmt.Sprint(report.Tags) != fmt.Sprint(want) {
		t.Errorf("tags = %v, want %v", report.Tags, want)
	}
}

func TestPlacingOutsideARequestDoesNothing(t *testing.T) {
	fake := reportertest.New()
	logger := platform.NewSlogLogger(discardLogger(), fake)
	ctx := context.Background()

	platform.SetReportActor(ctx, platform.AudienceStaff, memberID)
	platform.SetReportTag(ctx, platform.ReportTagEvent, eventID)
	logger.Error(ctx, "nightly sweep failed", "error", errors.New("boom"))

	report := onlyReport(t, fake)
	if report.UserID != "" || len(report.Tags) != 0 {
		t.Errorf("a report outside a request is placed: user %q, tags %v", report.UserID, report.Tags)
	}
}

func TestWorkDetachedFromARequestReportsUnderItsPlacement(t *testing.T) {
	detached := make(chan context.Context, 1)
	server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
		mux.HandleFunc("POST /api/v1/customer/ticket-sales/{ticketSaleId}/reverse", func(w http.ResponseWriter, r *http.Request) {
			platform.SetReportActor(r.Context(), platform.AudienceCustomer, customerID)
			detached <- platform.DetachFromRequest(r.Context())
			w.WriteHeader(http.StatusAccepted)
		})
	})
	resp, err := http.Post(server.URL+"/api/v1/customer/ticket-sales/"+saleID+"/reverse", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	platform.NewSlogLogger(discardLogger(), fake).Error(<-detached, "kick failed", "error", errConnectionRefused)

	report := onlyReport(t, fake)
	want := map[string]string{"route": "POST /api/v1/customer/ticket-sales/{ticketSaleId}/reverse", "audience": "customer", "sale_id": saleID}
	if report.UserID != customerID || fmt.Sprint(report.Tags) != fmt.Sprint(want) {
		t.Errorf("user, tags = %q, %v; want %q, %v", report.UserID, report.Tags, customerID, want)
	}
	if report.RequestID != resp.Header.Get(platform.RequestIDHeader) {
		t.Errorf("request id = %q, want the request's", report.RequestID)
	}
}
