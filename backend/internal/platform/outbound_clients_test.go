package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// outboundNames are the names of the http.client spans under request, in
// order, each checked to be finished.
func outboundNames(t *testing.T, fake *reportertest.Reporter, request *reportertest.Span) []string {
	t.Helper()
	if request == nil {
		t.Fatal("no request span")
	}
	var names []string
	for _, span := range fake.ChildrenOf(request) {
		if span.Options.Op != "http.client" {
			continue
		}
		if !span.Finished || span.HTTPStatus == 0 {
			t.Errorf("span %q finished %v status %d, want finished with a status", span.Options.Name, span.Finished, span.HTTPStatus)
		}
		names = append(names, span.Options.Name)
	}
	return names
}

func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("outbound spans =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// A PAYPHONE CALL IN A SAMPLED REQUEST IS A CHILD SPAN (#769).
func TestAPayPhoneCallIsAChildSpan(t *testing.T) {
	payphone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"paymentId": 1, "payWithCard": "https://pay.example/card/abc"})
	}))
	t.Cleanup(payphone.Close)
	provider := platform.NewPayPhoneProvider("token", "store-1", payphone.URL, platform.NewSlogLogger(discardLogger(), platform.NoopReporter{}))
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		if _, err := provider.Initiate(ctx, platform.PaymentInitiateInput{AmountCents: 1000, Currency: "USD", ClientTransactionID: "ctid-1"}); err != nil {
			t.Errorf("initiate: %v", err)
		}
	})

	assertNames(t, outboundNames(t, fake, request), "POST "+payphone.URL+"/api/button/Prepare")
}

// A RESEND CALL IN A SAMPLED REQUEST IS A CHILD SPAN (#769), named by
// Resend's own host.
func TestAResendCallIsAChildSpan(t *testing.T) {
	sender := platform.ResendSenderReaching(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"email-1"}`))
	}))
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		if err := sender.SendOTP(ctx, "ana.perez@example.com", "123456", platform.LocaleEN); err != nil {
			t.Errorf("send: %v", err)
		}
	})

	assertNames(t, outboundNames(t, fake, request), "POST https://api.resend.com/emails")
}

// AN SES SEND IN A SAMPLED REQUEST IS A CHILD SPAN, AND SO ARE THE METADATA
// AND STS CALLS THAT FEDERATE ITS CREDENTIALS (#769). The metadata call's
// audience is its query string, and is not in its span.
func TestAnSESSendAndItsFederationAreChildSpans(t *testing.T) {
	sender := platform.FederatedSESSender(t)
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		if err := sender.SendOTP(ctx, "ana.perez@example.com", "123456", platform.LocaleEN); err != nil {
			t.Errorf("send: %v", err)
		}
	})

	names := outboundNames(t, fake, request)
	if len(names) != 3 {
		t.Fatalf("outbound spans = %q, want the metadata, STS and SES calls", names)
	}
	if !strings.HasPrefix(names[0], "GET http://127.0.0.1:") || !strings.HasSuffix(names[0], "/computeMetadata/v1/instance/service-accounts/default/identity") {
		t.Errorf("metadata span = %q, want GET <metadata host>/computeMetadata/v1/instance/service-accounts/default/identity", names[0])
	}
	if !strings.HasPrefix(names[1], "POST http://127.0.0.1:") {
		t.Errorf("sts span = %q, want POST <sts host>/", names[1])
	}
	if !strings.HasPrefix(names[2], "POST http://127.0.0.1:") || !strings.HasSuffix(names[2], "/v2/email/outbound-emails") {
		t.Errorf("ses span = %q, want POST <ses host>/v2/email/outbound-emails", names[2])
	}
}
