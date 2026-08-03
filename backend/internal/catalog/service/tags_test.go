package service

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

func TestNormalizeTagNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       []string
		wantKeys    []string
		wantDisplay []string
		wantInvalid []string
	}{
		{
			name:        "trims and lowercases to a canonical key while keeping display casing",
			input:       []string{"  Techno  "},
			wantKeys:    []string{"techno"},
			wantDisplay: []string{"Techno"},
		},
		{
			name:        "collapses internal whitespace",
			input:       []string{"Live   Music"},
			wantKeys:    []string{"live music"},
			wantDisplay: []string{"Live Music"},
		},
		{
			name:        "dedupes case and whitespace variants keeping the first display",
			input:       []string{"Techno", "techno", "  TECHNO "},
			wantKeys:    []string{"techno"},
			wantDisplay: []string{"Techno"},
		},
		{
			name:        "rejects empty and whitespace-only names",
			input:       []string{"   ", ""},
			wantInvalid: []string{"   ", ""},
		},
		{
			name:        "rejects names over the length cap",
			input:       []string{"this tag name is definitely way too long"},
			wantInvalid: []string{"this tag name is definitely way too long"},
		},
		{
			name:        "rejects disallowed characters",
			input:       []string{"rock&roll", "hip/hop"},
			wantInvalid: []string{"rock&roll", "hip/hop"},
		},
		{
			name:        "allows letters, digits, spaces, and hyphens",
			input:       []string{"Drum-and-Bass 2"},
			wantKeys:    []string{"drum-and-bass 2"},
			wantDisplay: []string{"Drum-and-Bass 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			normalized, invalid := normalizeTagNames(tt.input)

			if len(normalized) != len(tt.wantKeys) {
				t.Fatalf("normalized len = %d, want %d (%+v)", len(normalized), len(tt.wantKeys), normalized)
			}
			for i, nt := range normalized {
				if nt.CanonicalKey != tt.wantKeys[i] {
					t.Errorf("key[%d] = %q, want %q", i, nt.CanonicalKey, tt.wantKeys[i])
				}
				if nt.DisplayName != tt.wantDisplay[i] {
					t.Errorf("display[%d] = %q, want %q", i, nt.DisplayName, tt.wantDisplay[i])
				}
			}

			if len(invalid) != len(tt.wantInvalid) {
				t.Fatalf("invalid = %+v, want %+v", invalid, tt.wantInvalid)
			}
			for i, got := range invalid {
				if got != tt.wantInvalid[i] {
					t.Errorf("invalid[%d] = %q, want %q", i, got, tt.wantInvalid[i])
				}
			}
		})
	}
}

// A TagView carries the Tag's canonical key alongside its English display name.
// The Storefront keys its Locale copy on that key (ADR 0027), so it must not
// have to rebuild it by lowercasing the display name — a Preset Tag rendered in
// Spanish lowercases to a key that matches nothing.
func TestToTagViewsCarriesCanonicalKey(t *testing.T) {
	t.Parallel()

	views := toTagViews([]repository.Tag{
		{CanonicalKey: "arts & theatre", DisplayName: "Arts & Theatre", Curated: true},
		{CanonicalKey: "techno", DisplayName: "Techno", Curated: false},
	})

	want := []TagView{
		{CanonicalKey: "arts & theatre", Name: "Arts & Theatre", Curated: true},
		{CanonicalKey: "techno", Name: "Techno", Curated: false},
	}
	if len(views) != len(want) {
		t.Fatalf("views = %+v, want %+v", views, want)
	}
	for i, got := range views {
		if got != want[i] {
			t.Errorf("views[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}
