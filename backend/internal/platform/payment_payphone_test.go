package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Unit coverage for the PayPhone request/response mapping. The full checkout
// flow against a fake PayPhone server lives in the integration suite; these
// tests pin the pure translation rules — field names, the USD refusal, and the
// three-way verdict (approved / declined / unknown-outcome error) that keeps a
// possibly-charged Customer's Payment out of the failed state.

func discardLogger() Logger {
	return NewSlogLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPayPhoneInitiateRefusesNonUSD(t *testing.T) {
	p := NewPayPhoneProvider("token", "store-1", "http://payphone.invalid", discardLogger())

	_, err := p.Initiate(context.Background(), PaymentInitiateInput{
		AmountCents:         1000,
		Currency:            "EUR",
		ClientTransactionID: "ctid-1",
	})
	if err == nil {
		t.Fatal("initiate accepted EUR; PayPhone charges USD only and must be refused before any request is made")
	}
	if !strings.Contains(err.Error(), "USD") {
		t.Fatalf("error = %v, want it to name the USD-only rule", err)
	}
}

func TestPayPhoneInitiateCallsPrepareAndReturnsTheCardURL(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/button/Prepare" {
			t.Errorf("path = %q, want /api/button/Prepare", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode prepare body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"paymentId":       118201001,
			"payWithPayPhone": "https://ppls.io/app/abc",
			"payWithCard":     "https://pay.example/card/abc",
		})
	}))
	defer srv.Close()

	p := NewPayPhoneProvider("secret-token", "store-1", srv.URL, discardLogger())
	init, err := p.Initiate(context.Background(), PaymentInitiateInput{
		AmountCents:         3500,
		Currency:            "USD",
		ClientTransactionID: "ctid-123",
		Reference:           "Summer Concert",
		ResponseURL:         "http://storefront.example/checkout/return",
	})
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q, want the Bearer token", gotAuth)
	}
	// PayPhone requires amount to equal the sum of its monetary components;
	// with no tax breakdown the whole amount rides as amountWithoutTax.
	if gotBody["amount"] != float64(3500) || gotBody["amountWithoutTax"] != float64(3500) {
		t.Fatalf("amounts = %v/%v, want 3500/3500", gotBody["amount"], gotBody["amountWithoutTax"])
	}
	if gotBody["clientTransactionId"] != "ctid-123" {
		t.Fatalf("clientTransactionId = %v", gotBody["clientTransactionId"])
	}
	if gotBody["currency"] != "USD" || gotBody["storeId"] != "store-1" {
		t.Fatalf("currency/storeId = %v/%v", gotBody["currency"], gotBody["storeId"])
	}
	if gotBody["reference"] != "Summer Concert" {
		t.Fatalf("reference = %v", gotBody["reference"])
	}
	if gotBody["responseUrl"] != "http://storefront.example/checkout/return" {
		t.Fatalf("responseUrl = %v", gotBody["responseUrl"])
	}

	// The card form is the one hosted page any guest can complete in a browser;
	// payWithPayPhone is the app flow and is deliberately not the redirect.
	if init.RedirectURL != "https://pay.example/card/abc" {
		t.Fatalf("redirect url = %q, want the payWithCard url", init.RedirectURL)
	}
	if init.ProviderPaymentID != "118201001" {
		t.Fatalf("provider payment id = %q, want the numeric paymentId as a string", init.ProviderPaymentID)
	}
}

func TestPayPhoneInitiateErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"http error": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"invalid token"}`, http.StatusUnauthorized)
		},
		"malformed json": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json"))
		},
		"no payment url": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"paymentId": 1})
		},
	}
	for name, handler := range cases {
		srv := httptest.NewServer(handler)
		p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())
		_, err := p.Initiate(context.Background(), PaymentInitiateInput{
			AmountCents:         1000,
			Currency:            "USD",
			ClientTransactionID: "ctid-1",
		})
		srv.Close()
		if err == nil {
			t.Fatalf("%s: initiate succeeded, want an error", name)
		}
	}
}

func TestPayPhoneConfirmMapsTheVerdict(t *testing.T) {
	respond := func(body map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/button/V2/Confirm" {
				t.Errorf("path = %q, want /api/button/V2/Confirm", r.URL.Path)
			}
			var got map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			// V2/Confirm takes PayPhone's integer id and our id under its
			// clientTxId name; both are load-bearing field names.
			if got["id"] != float64(11820) {
				t.Errorf("confirm id = %v, want 11820", got["id"])
			}
			if got["clientTxId"] != "ctid-123" {
				t.Errorf("confirm clientTxId = %v", got["clientTxId"])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		}
	}

	t.Run("statusCode 3 approves with instrument", func(t *testing.T) {
		srv := httptest.NewServer(respond(map[string]any{
			"statusCode":        3,
			"transactionStatus": "Approved",
			"transactionId":     987654,
			"authorizationCode": "AUTH99",
			"cardBrand":         "Visa",
			"lastDigits":        "1234",
		}))
		defer srv.Close()
		p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())

		conf, err := p.Confirm(context.Background(), PaymentConfirmInput{
			ClientTransactionID: "ctid-123",
			Params:              map[string]string{"id": "11820", "clientTransactionId": "ctid-123"},
		})
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if !conf.Approved {
			t.Fatal("statusCode 3 must approve")
		}
		if conf.ProviderTransactionID != "987654" {
			t.Fatalf("provider transaction id = %q, want 987654", conf.ProviderTransactionID)
		}
		if conf.Instrument != "visa ····1234" {
			t.Fatalf("instrument = %q, want %q", conf.Instrument, "visa ····1234")
		}
	})

	t.Run("statusCode 2 declines", func(t *testing.T) {
		srv := httptest.NewServer(respond(map[string]any{
			"statusCode":        2,
			"transactionStatus": "Canceled",
			"transactionId":     987654,
		}))
		defer srv.Close()
		p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())

		conf, err := p.Confirm(context.Background(), PaymentConfirmInput{
			ClientTransactionID: "ctid-123",
			Params:              map[string]string{"id": "11820"},
		})
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if conf.Approved {
			t.Fatal("statusCode 2 must decline")
		}
		if conf.ProviderTransactionID != "987654" {
			t.Fatalf("provider transaction id = %q, want it recorded on the decline too", conf.ProviderTransactionID)
		}
	})

	t.Run("unrecognized statusCode is an error, never a decline", func(t *testing.T) {
		// A verdict PayPhone does not document: the charge might have succeeded,
		// so declining here would strand a paid Customer. The Payment must stay
		// pending upstream, which means Confirm must ERROR.
		srv := httptest.NewServer(respond(map[string]any{
			"statusCode":        1,
			"transactionStatus": "Pending",
		}))
		defer srv.Close()
		p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())

		_, err := p.Confirm(context.Background(), PaymentConfirmInput{
			ClientTransactionID: "ctid-123",
			Params:              map[string]string{"id": "11820"},
		})
		if err == nil {
			t.Fatal("confirm returned a verdict for an unrecognized statusCode; it must error")
		}
	})
}

func TestPayPhoneConfirmUnknownOutcomeErrors(t *testing.T) {
	t.Run("missing id param never reaches PayPhone", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("confirm called PayPhone without a transaction id to confirm")
		}))
		defer srv.Close()
		p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())

		for name, params := range map[string]map[string]string{
			"missing":   nil,
			"malformed": {"id": "not-a-number"},
		} {
			_, err := p.Confirm(context.Background(), PaymentConfirmInput{
				ClientTransactionID: "ctid-123",
				Params:              params,
			})
			if err == nil {
				t.Fatalf("%s id param: confirm succeeded, want an error", name)
			}
		}
	})

	t.Run("http error and malformed json", func(t *testing.T) {
		cases := map[string]http.HandlerFunc{
			"http 500":       func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
			"malformed json": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>garbage</html>")) },
		}
		for name, handler := range cases {
			srv := httptest.NewServer(handler)
			p := NewPayPhoneProvider("token", "store-1", srv.URL, discardLogger())
			_, err := p.Confirm(context.Background(), PaymentConfirmInput{
				ClientTransactionID: "ctid-123",
				Params:              map[string]string{"id": "11820"},
			})
			srv.Close()
			if err == nil {
				t.Fatalf("%s: confirm succeeded, want an error (unknown outcome must not settle the Payment)", name)
			}
		}
	})
}

func TestPayPhoneReverseIsReserved(t *testing.T) {
	p := NewPayPhoneProvider("token", "store-1", "http://payphone.invalid", discardLogger())
	if err := p.Reverse(context.Background(), "ctid-123"); !errors.Is(err, ErrPaymentReverseNotSupported) {
		t.Fatalf("reverse error = %v, want ErrPaymentReverseNotSupported", err)
	}
}

func TestPayPhoneInstrument(t *testing.T) {
	cases := []struct {
		brand, digits, want string
	}{
		{"Visa", "1234", "visa ····1234"},
		{"MasterCard", "", "mastercard"},
		{"", "1234", "····1234"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := payPhoneInstrument(tc.brand, tc.digits); got != tc.want {
			t.Fatalf("instrument(%q, %q) = %q, want %q", tc.brand, tc.digits, got, tc.want)
		}
	}
}

// PayPhone's docs show numeric ids, but ids are opaque strings on our side and
// a string id must not fail a confirm whose outcome is otherwise known.
func TestPayPhoneIDReadsNumbersAndStrings(t *testing.T) {
	var out struct {
		ID payPhoneNumberOrString `json:"id"`
	}
	for raw, want := range map[string]string{
		`{"id": 987654}`:     "987654",
		`{"id": "987654"}`:   "987654",
		`{"id": null}`:       "",
		`{}`:                 "",
		`{"id": " 987654 "}`: "987654",
	} {
		out.ID = ""
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if string(out.ID) != want {
			t.Fatalf("id from %s = %q, want %q", raw, out.ID, want)
		}
	}
}
