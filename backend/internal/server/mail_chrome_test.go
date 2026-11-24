package server

import (
	"context"
	"errors"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	invoicingrepo "github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// warnCounter counts Warn lines.
type warnCounter struct {
	quietLogger
	warns int
}

func (l *warnCounter) Warn(string, ...any) { l.warns++ }

type fakeIssuers struct {
	row *invoicingrepo.EcuadorIssuerRow
	err error
}

func (f fakeIssuers) GetEcuadorIssuer(context.Context) (*invoicingrepo.EcuadorIssuerRow, error) {
	return f.row, f.err
}

// The mail chrome is resolved once at startup (ADR 0082): the mark on the
// Storefront's origin, the support contact, and the legal line from the
// Issuer's razón social as registered, omitted when there is no Issuer to
// name or it cannot be read.
func TestMailChromeNamesTheIssuerOrOmitsTheLegalLine(t *testing.T) {
	cfg := platform.Config{StorefrontBaseURL: "https://tickets.example.com/"}
	issuer := &invoicingrepo.EcuadorIssuerRow{Details: invoicing.EcuadorIssuerDetails{RazonSocial: "FUNDACION EJEMPLO"}}

	cases := []struct {
		name              string
		issuers           fakeIssuers
		operator, country string
		wantWarning       bool
	}{
		{"an Issuer is recorded", fakeIssuers{row: issuer}, "FUNDACION EJEMPLO", "Ecuador", false},
		{"no Issuer is recorded", fakeIssuers{}, "", "", false},
		{"the Issuer cannot be read", fakeIssuers{err: errors.New("connection reset")}, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &warnCounter{}
			chrome := mailChrome(context.Background(), cfg, tc.issuers, logger)
			want := platform.MailChrome{
				MarkURL:         "https://tickets.example.com/icon-192.png",
				OperatorName:    tc.operator,
				OperatorCountry: tc.country,
				SupportEmail:    platform.DefaultSupportEmail,
			}
			if chrome != want {
				t.Fatalf("chrome = %+v, want %+v", chrome, want)
			}
			if got := logger.warns > 0; got != tc.wantWarning {
				t.Fatalf("warned = %v, want %v", got, tc.wantWarning)
			}
		})
	}
}
