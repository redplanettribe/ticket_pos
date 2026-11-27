package service

import (
	"strings"
	"testing"
)

// One copy per person (#885): the Event Recap's readers are compared by
// normalized address, so the same person written with other case or stray
// spaces is one reader, kept as first given, and an empty address is nobody.
func TestDistinctEmailsKeepsOneAddressPerPersonComparedNormalized(t *testing.T) {
	got := distinctEmails([]string{
		"ana@example.com",
		"Ana@Example.com",
		" ana@example.com ",
		"",
		"ben@example.com",
		"BEN@example.com",
	})
	if want := "ana@example.com|ben@example.com"; strings.Join(got, "|") != want {
		t.Fatalf("distinctEmails = %q, want %q", got, want)
	}
}
