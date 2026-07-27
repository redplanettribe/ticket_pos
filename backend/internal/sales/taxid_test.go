package sales_test

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

func TestRequireTaxID(t *testing.T) {
	full := platform.SaleTaxID{Type: platform.TaxIDTypeCedula, Number: "1712345675"}

	cases := []struct {
		name    string
		channel string
		taxID   platform.SaleTaxID
		wantErr bool
	}{
		{name: "online with tax id passes", channel: "online", taxID: full},
		{name: "in_person with tax id passes", channel: "in_person", taxID: full},
		{name: "online without tax id fails", channel: "online", wantErr: true},
		{name: "in_person without tax id fails", channel: "in_person", wantErr: true},
		{name: "import without tax id passes", channel: "import"},
		{name: "import with tax id passes", channel: "import", taxID: full},
		// A half-set pair is as absent as a missing one: the columns are paired
		// by CHECK constraint, so the spine must never see one without the other.
		{name: "online with type but no number fails", channel: "online", taxID: platform.SaleTaxID{Type: platform.TaxIDTypeCedula}, wantErr: true},
		{name: "online with number but no type fails", channel: "online", taxID: platform.SaleTaxID{Number: "1712345675"}, wantErr: true},
		// An unknown future channel gets the strict default, not the exemption.
		{name: "unknown channel without tax id fails", channel: "kiosk", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sales.RequireTaxID(tc.channel, tc.taxID)
			if tc.wantErr && err == nil {
				t.Fatalf("RequireTaxID(%q, %+v) = nil, want error", tc.channel, tc.taxID)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("RequireTaxID(%q, %+v) = %v, want nil", tc.channel, tc.taxID, err)
			}
		})
	}
}
