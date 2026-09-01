package invoicing

import "testing"

// The one rule for a refusal by number (#576, ADR 0068): the SRI's 45 as an
// error. Any other refusal code is an ordinary rejection, and a 45 the
// authority typed as anything but an error is not a refusal at all.
func TestRefusedByNumberIn(t *testing.T) {
	errorMessage := func(id string) AuthorityMessage {
		return AuthorityMessage{Identifier: id, Message: "x", Type: AuthorityMessageTypeError}
	}
	cases := []struct {
		name     string
		messages []AuthorityMessage
		want     bool
	}{
		{"none", nil, false},
		{"45 alone", []AuthorityMessage{errorMessage("45")}, true},
		{"another refusal alone", []AuthorityMessage{errorMessage("35")}, false},
		{"45 beside another refusal", []AuthorityMessage{errorMessage("35"), errorMessage("45")}, true},
		{"45 as an advertencia is not the refusal", []AuthorityMessage{{Identifier: "45", Type: AuthorityMessageTypeWarning}}, false},
		{"45 with no tipo at all is not the refusal", []AuthorityMessage{{Identifier: "45"}}, false},
		{"the test environment's advertencia", []AuthorityMessage{{Identifier: "60", Type: AuthorityMessageTypeWarning}}, false},
	}
	for _, c := range cases {
		if got := RefusedByNumberIn(c.messages); got != c.want {
			t.Fatalf("%s: RefusedByNumberIn = %v; want %v", c.name, got, c.want)
		}
	}
}
