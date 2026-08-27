package invoicing

import "testing"

// The one rule for what raises a Recipient Warning (#482): the SRI's 59 or
// 62 as an advertencia. The test environment's 60 never does, nor does a
// 59 the authority somehow typed as anything but a warning.
func TestRecipientWarningIn(t *testing.T) {
	warning := func(id string) AuthorityMessage {
		return AuthorityMessage{Identifier: id, Message: "x", Type: AuthorityMessageTypeWarning}
	}
	cases := []struct {
		name     string
		messages []AuthorityMessage
		want     bool
	}{
		{"none", nil, false},
		{"60 alone", []AuthorityMessage{warning("60")}, false},
		{"59 beside 60", []AuthorityMessage{warning("60"), warning("59")}, true},
		{"62 alone", []AuthorityMessage{warning("62")}, true},
		{"59 typed as an error is not the warning", []AuthorityMessage{{Identifier: "59", Type: "ERROR"}}, false},
		{"another advertencia", []AuthorityMessage{warning("68")}, false},
	}
	for _, c := range cases {
		if got := RecipientWarningIn(c.messages); got != c.want {
			t.Fatalf("%s: RecipientWarningIn = %v; want %v", c.name, got, c.want)
		}
	}
}
