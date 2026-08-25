package sri

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCheckDigitFichaExample(t *testing.T) {
	// Ficha Técnica §5.2 worked example: 41261533 → 6.
	got, err := CheckDigit("41261533")
	if err != nil {
		t.Fatal(err)
	}
	if got != 6 {
		t.Fatalf("check digit = %d, want 6", got)
	}
}

func TestCheckDigitBoundaries(t *testing.T) {
	// "1": weights from the right start at 2 → sum 2, 2 mod 11 = 2, 11-2 = 9.
	// "0": sum 0 → 11 - 0 = 11 → 0.
	// "6": sum 12, 12 mod 11 = 1, 11 - 1 = 10 → 1.
	cases := []struct {
		digits string
		want   int
	}{
		{"0", 0},  // 11 → 0
		{"6", 1},  // 10 → 1
		{"1", 9},  // plain
		{"00", 0}, // 11 → 0
		{"12", 4}, // 1*3 + 2*2 = 7 → 11-7 = 4
	}
	for _, c := range cases {
		got, err := CheckDigit(c.digits)
		if err != nil {
			t.Fatalf("%q: %v", c.digits, err)
		}
		if got != c.want {
			t.Errorf("CheckDigit(%q) = %d, want %d", c.digits, got, c.want)
		}
	}
	if _, err := CheckDigit("12a"); err == nil {
		t.Fatal("expected error on non-digit input")
	}
	if _, err := CheckDigit(""); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func validAccessKeyInput() AccessKeyInput {
	return AccessKeyInput{
		IssuedOn:      time.Date(2026, 8, 25, 15, 4, 5, 0, Guayaquil),
		DocumentType:  DocumentTypeFactura,
		RUC:           "1790012345001",
		Environment:   EnvironmentTest,
		Establishment: "001",
		EmissionPoint: "002",
		Sequential:    "000000123",
		NumericCode:   "12345678",
	}
}

func TestNewAccessKeyLayout(t *testing.T) {
	key, err := NewAccessKey(validAccessKeyInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 49 {
		t.Fatalf("len = %d, want 49: %s", len(key), key)
	}
	if !regexp.MustCompile(`^[0-9]{49}$`).MatchString(key) {
		t.Fatalf("not all digits: %s", key)
	}
	want := "25082026" + "01" + "1790012345001" + "1" + "001002" + "000000123" + "12345678" + "1"
	if !strings.HasPrefix(key, want) {
		t.Fatalf("key = %s\nwant prefix %s", key, want)
	}
	check, _ := CheckDigit(want)
	if key[48] != byte('0'+check) {
		t.Fatalf("check digit %c, want %d", key[48], check)
	}
	if err := ValidateAccessKey(key); err != nil {
		t.Fatalf("ValidateAccessKey: %v", err)
	}
}

func TestNewAccessKeyDateIsInGuayaquil(t *testing.T) {
	// 2026-08-26 03:00 UTC is still 2026-08-25 in Ecuador.
	in := validAccessKeyInput()
	in.IssuedOn = time.Date(2026, 8, 26, 3, 0, 0, 0, time.UTC)
	key, err := NewAccessKey(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "25082026") {
		t.Fatalf("key = %s, want date 25082026", key)
	}
}

func TestNewAccessKeyRefusesBadComponents(t *testing.T) {
	cases := map[string]func(*AccessKeyInput){
		"unpadded sequential":  func(in *AccessKeyInput) { in.Sequential = "123" },
		"long sequential":      func(in *AccessKeyInput) { in.Sequential = "0000000123" },
		"empty sequential":     func(in *AccessKeyInput) { in.Sequential = "" },
		"non-digit sequential": func(in *AccessKeyInput) { in.Sequential = "00000012x" },
		"short ruc":            func(in *AccessKeyInput) { in.RUC = "179001234500" },
		"bad environment":      func(in *AccessKeyInput) { in.Environment = "3" },
		"short numeric code":   func(in *AccessKeyInput) { in.NumericCode = "1234567" },
		"bad establishment":    func(in *AccessKeyInput) { in.Establishment = "1" },
		"bad emission point":   func(in *AccessKeyInput) { in.EmissionPoint = "0002" },
		"bad document type":    func(in *AccessKeyInput) { in.DocumentType = "1" },
		"zero date":            func(in *AccessKeyInput) { in.IssuedOn = time.Time{} },
	}
	for name, mutate := range cases {
		in := validAccessKeyInput()
		mutate(&in)
		if _, err := NewAccessKey(in); !errors.Is(err, ErrInvalidAccessKey) {
			t.Errorf("%s: err = %v, want ErrInvalidAccessKey", name, err)
		}
	}
}

func TestValidateAccessKey(t *testing.T) {
	key, _ := NewAccessKey(validAccessKeyInput())
	flipped := key[:48] + string('0'+(key[48]-'0'+1)%10)
	if err := ValidateAccessKey(flipped); !errors.Is(err, ErrInvalidAccessKey) {
		t.Fatalf("wrong check digit accepted: %v", err)
	}
	if err := ValidateAccessKey(key[:48]); !errors.Is(err, ErrInvalidAccessKey) {
		t.Fatalf("48 digits accepted: %v", err)
	}
}

func TestParseAccessKey(t *testing.T) {
	in := validAccessKeyInput()
	key, _ := NewAccessKey(in)
	parsed, err := ParseAccessKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.IssuedOn.Format("2006-01-02") != "2026-08-25" ||
		parsed.DocumentType != "01" || parsed.RUC != in.RUC || parsed.Environment != EnvironmentTest ||
		parsed.Establishment != "001" || parsed.EmissionPoint != "002" ||
		parsed.Sequential != "000000123" || parsed.NumericCode != "12345678" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestRandomNumericCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		code, err := RandomNumericCode()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[0-9]{8}$`).MatchString(code) {
			t.Fatalf("code %q is not 8 digits", code)
		}
		seen[code] = true
	}
	if len(seen) < 2 {
		t.Fatal("codes are not random")
	}
}

func TestFormatSequential(t *testing.T) {
	s, err := FormatSequential(123)
	if err != nil || s != "000000123" {
		t.Fatalf("got %q, %v", s, err)
	}
	if _, err := FormatSequential(0); err == nil {
		t.Fatal("zero accepted")
	}
	if _, err := FormatSequential(1_000_000_000); err == nil {
		t.Fatal("ten digits accepted")
	}
}
