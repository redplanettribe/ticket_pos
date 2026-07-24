package platform

import "testing"

// TestNormalizeEmail pins the one rule the platform-global Customer's
// UNIQUE(email) rests on. Every spelling below must collapse to the same
// string, because each is a way the same person's address arrives from a
// different box office.
func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already normalised", "ana@example.com", "ana@example.com"},
		{"uppercase", "ANA@EXAMPLE.COM", "ana@example.com"},
		{"mixed case", "Ana@Example.Com", "ana@example.com"},
		{"surrounding spaces", "  ana@example.com  ", "ana@example.com"},
		{"tab and newline", "\tana@example.com\n", "ana@example.com"},
		{"case and whitespace", " Ana@Example.COM ", "ana@example.com"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeEmail(tt.input); got != tt.want {
				t.Fatalf("NormalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
