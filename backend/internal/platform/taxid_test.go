package platform

import (
	"errors"
	"testing"
)

// The cédulas used below are known-valid numbers: their check digit is the one
// the published modulo-10 algorithm produces. Bumping the last digit by one is
// how each "check digit" case is built, because that is exactly what a mistyped
// cédula looks like in practice.
const (
	validCedulaQuito      = "1710034065" // province 17 (Pichincha)
	validCedulaGuayas     = "0926687856" // province 09 (Guayas)
	validCedulaChimborazo = "0602910945" // province 06 (Chimborazo)
)

// TestValidateTaxIDCedula pins the strictest arm of the gradient. A cédula is
// algorithmically verifiable, so a number that fails here would otherwise reach
// a tax declaration wrong.
func TestValidateTaxIDCedula(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		want    string
		wantErr error
	}{
		{"valid pichincha", validCedulaQuito, validCedulaQuito, nil},
		{"valid guayas with leading zero province", validCedulaGuayas, validCedulaGuayas, nil},
		{"valid chimborazo", validCedulaChimborazo, validCedulaChimborazo, nil},
		{"valid province 24", "2400176398", "2400176398", nil},
		{"valid province 30 registered abroad", "3000000004", "3000000004", nil},
		{"surrounding whitespace trimmed", "  1710034065  ", validCedulaQuito, nil},
		{"tab and newline trimmed", "\t1710034065\n", validCedulaQuito, nil},

		{"check digit off by one", "1710034066", "", ErrTaxIDNumberInvalid},
		{"check digit zero instead of five", "1710034060", "", ErrTaxIDNumberInvalid},
		{"transposed digits break the check", "1710034605", "", ErrTaxIDNumberInvalid},
		{"nine digits", "171003406", "", ErrTaxIDNumberInvalid},
		{"eleven digits", "17100340650", "", ErrTaxIDNumberInvalid},
		{"thirteen digits (a RUC under the cedula type)", "1710034065001", "", ErrTaxIDNumberInvalid},
		{"province 00", "0010034064", "", ErrTaxIDNumberInvalid},
		{"province 25", "2510034065", "", ErrTaxIDNumberInvalid},
		{"province 29", "2910034061", "", ErrTaxIDNumberInvalid},
		{"province 31", "3110034067", "", ErrTaxIDNumberInvalid},
		{"province 99", "9910034066", "", ErrTaxIDNumberInvalid},
		{"letters", "17100340AB", "", ErrTaxIDNumberInvalid},
		{"dashes as separators", "171-003406", "", ErrTaxIDNumberInvalid},
		{"internal space", "171003 4065", "", ErrTaxIDNumberInvalid},
		{"empty", "", "", ErrTaxIDNumberInvalid},
		{"whitespace only", "   ", "", ErrTaxIDNumberInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTaxID(t, TaxIDTypeCedula, tt.number, tt.want, tt.wantErr)
		})
	}
}

// TestValidateTaxIDRUC pins the three RUC forms. The natural-person form is a
// cédula plus an establishment suffix and is checked to the digit; the company
// and public-entity forms are accepted on structure alone (see isValidRUC).
func TestValidateTaxIDRUC(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		want    string
		wantErr error
	}{
		{"natural person, valid cedula root", "1710034065001", "1710034065001", nil},
		{"natural person, second establishment", "1710034065002", "1710034065002", nil},
		{"natural person, guayas root", "0926687856001", "0926687856001", nil},
		{"natural person, third digit 5", "1750034066001", "1750034066001", nil},
		{"company, third digit 9", "1790011674001", "1790011674001", nil},
		{"public entity, third digit 6", "1760001550001", "1760001550001", nil},
		{"surrounding whitespace trimmed", "  1790011674001  ", "1790011674001", nil},

		{"natural person, check digit off by one", "1710034066001", "", ErrTaxIDNumberInvalid},
		{"natural person, transposed digits", "1710034605001", "", ErrTaxIDNumberInvalid},
		{"third digit 7 names no RUC form", "1770034065001", "", ErrTaxIDNumberInvalid},
		{"third digit 8 names no RUC form", "1780034065001", "", ErrTaxIDNumberInvalid},
		{"ten digits (a cedula under the ruc type)", "1710034065", "", ErrTaxIDNumberInvalid},
		{"twelve digits", "179001167400", "", ErrTaxIDNumberInvalid},
		{"fourteen digits", "17900116740010", "", ErrTaxIDNumberInvalid},
		{"province 00", "0090011674001", "", ErrTaxIDNumberInvalid},
		{"province 25", "2590011674001", "", ErrTaxIDNumberInvalid},
		{"province 31", "3190011674001", "", ErrTaxIDNumberInvalid},
		{"letters", "17900116740O1", "", ErrTaxIDNumberInvalid},
		{"consumidor final placeholder is not a RUC", "9999999999999", "", ErrTaxIDNumberInvalid},
		{"empty", "", "", ErrTaxIDNumberInvalid},
		{"whitespace only", "   ", "", ErrTaxIDNumberInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTaxID(t, TaxIDTypeRUC, tt.number, tt.want, tt.wantErr)
		})
	}
}

// TestValidateTaxIDPassport pins the permissive arm: a passport is issued by any
// country under any scheme, so shape is all that can be asserted — and the
// stored form is uppercase, so one person's passport is one string.
func TestValidateTaxIDPassport(t *testing.T) {
	tests := []struct {
		name    string
		number  string
		want    string
		wantErr error
	}{
		{"already uppercase", "AB123456", "AB123456", nil},
		{"lowercase uppercased", "ab123456", "AB123456", nil},
		{"mixed case uppercased", "Ab123456", "AB123456", nil},
		{"all digits", "123456789", "123456789", nil},
		{"all letters", "ABCDEF", "ABCDEF", nil},
		{"minimum six characters", "a12345", "A12345", nil},
		{"maximum twenty characters", "abcdefghij0123456789", "ABCDEFGHIJ0123456789", nil},
		{"surrounding whitespace trimmed then uppercased", "  ab123456  ", "AB123456", nil},
		{"tab and newline trimmed", "\tab123456\n", "AB123456", nil},

		{"five characters", "AB123", "", ErrTaxIDNumberInvalid},
		{"twenty-one characters", "abcdefghij01234567890", "", ErrTaxIDNumberInvalid},
		{"empty", "", "", ErrTaxIDNumberInvalid},
		{"whitespace only", "      ", "", ErrTaxIDNumberInvalid},
		{"whitespace padding does not count toward the minimum", "  AB1  ", "", ErrTaxIDNumberInvalid},
		{"internal space", "AB 123456", "", ErrTaxIDNumberInvalid},
		{"dash", "AB-123456", "", ErrTaxIDNumberInvalid},
		{"accented letter", "ÁB123456", "", ErrTaxIDNumberInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTaxID(t, TaxIDTypePassport, tt.number, tt.want, tt.wantErr)
		})
	}
}

// TestValidateTaxIDType pins the type arm. It is matched exactly: the Tax ID
// Type arrives from a closed enum, and an unknown one is a fault of a different
// field than an unparseable number, which is why it has its own error.
func TestValidateTaxIDType(t *testing.T) {
	tests := []struct {
		name    string
		taxType string
		number  string
		wantErr error
	}{
		{"empty type", "", validCedulaQuito, ErrTaxIDTypeUnknown},
		{"unknown type", "dni", validCedulaQuito, ErrTaxIDTypeUnknown},
		{"capitalised cedula is not the enum value", "Cedula", validCedulaQuito, ErrTaxIDTypeUnknown},
		{"uppercase RUC is not the enum value", "RUC", "1790011674001", ErrTaxIDTypeUnknown},
		{"padded type is not trimmed", " cedula ", validCedulaQuito, ErrTaxIDTypeUnknown},
		{"spanish spelling with accent", "cédula", validCedulaQuito, ErrTaxIDTypeUnknown},
		// An unknown type wins over a bad number: the caller must fix the type first.
		{"unknown type with unusable number", "dni", "nonsense", ErrTaxIDTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTaxID(t, tt.taxType, tt.number, "", tt.wantErr)
		})
	}
}

func assertTaxID(t *testing.T, taxIDType, number, want string, wantErr error) {
	t.Helper()
	got, err := ValidateTaxID(taxIDType, number)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ValidateTaxID(%q, %q) err = %v, want %v", taxIDType, number, err, wantErr)
	}
	if got != want {
		t.Fatalf("ValidateTaxID(%q, %q) = %q, want %q", taxIDType, number, got, want)
	}
}
