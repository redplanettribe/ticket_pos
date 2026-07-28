package platform

import (
	"errors"
	"testing"
)

// The phone number rule (#105). These cases are mirrored case for case in
// apps/storefront/lib/phone.test.ts, because the rule is deliberately stated
// twice — once per runtime — and that duplication is exactly the risk worth
// testing. When a case is added here it belongs there too; when the two tables
// cannot agree, the Storefront must be the one that accepts more.

// TestValidatePhoneEcuador pins the strict tier. Ecuador is the home market and
// the default country selection, and PayPhone's form wants a cardholder's
// mobile — so a landline is a rejection rather than a pass-through.
func TestValidatePhoneEcuador(t *testing.T) {
	tests := []struct {
		name    string
		phone   string
		want    string
		wantErr error
	}{
		{"canonical mobile", "+593987654321", "+593987654321", nil},
		{"another canonical mobile", "+593991234567", "+593991234567", nil},
		{"lowest nine-leading mobile", "+593900000000", "+593900000000", nil},
		{"domestic trunk zero dropped", "+5930987654321", "+593987654321", nil},
		{"spaces as the buyer typed them", "+593 98 765 4321", "+593987654321", nil},
		{"dashes", "+593-98-765-4321", "+593987654321", nil},
		{"parenthesised trunk zero", "+593 (0)98 765 4321", "+593987654321", nil},
		{"dots", "+593.98.765.4321", "+593987654321", nil},
		{"surrounding whitespace", "  +593987654321  ", "+593987654321", nil},
		{"tab and newline", "\t+593987654321\n", "+593987654321", nil},

		// The landline rejections. An 02… number is a real, dialable Ecuadorian
		// number; it is refused because it is the wrong *kind* of number for a
		// card form, not because it is malformed.
		{"quito landline", "+59322345678", "", ErrPhoneInvalid},
		{"quito landline with trunk zero", "+593022345678", "", ErrPhoneInvalid},
		{"guayaquil landline", "+59342345678", "", ErrPhoneInvalid},
		{"national part leading 8", "+593887654321", "", ErrPhoneInvalid},
		{"eight-digit national part", "+59398765432", "", ErrPhoneInvalid},
		{"ten-digit national part", "+5939876543210", "", ErrPhoneInvalid},
		{"dialling code alone", "+593", "", ErrPhoneInvalid},
		{"two leading zeros, only one is a trunk prefix", "+59300987654321", "", ErrPhoneInvalid},
		{"letters among the digits", "+59398765432a", "", ErrPhoneInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPhone(t, tt.phone, tt.want, tt.wantErr)
		})
	}
}

// TestValidatePhoneGeneric pins the permissive tier. Its job is to prove the
// rule stays out of the way: no phone library is used, so anything that could
// plausibly be a foreign number must survive, and only the E.164 bounds bite.
func TestValidatePhoneGeneric(t *testing.T) {
	tests := []struct {
		name    string
		phone   string
		want    string
		wantErr error
	}{
		{"us number", "+12025550123", "+12025550123", nil},
		{"us number as typed", "+1 (202) 555-0123", "+12025550123", nil},
		{"uk mobile", "+447911123456", "+447911123456", nil},
		{"spain mobile", "+34612345678", "+34612345678", nil},
		{"colombia mobile", "+573001234567", "+573001234567", nil},
		{"peru mobile", "+51987654321", "+51987654321", nil},
		{"germany with a long national part", "+4915112345678", "+4915112345678", nil},
		{"minimum four digits", "+1234", "+1234", nil},
		{"maximum fifteen digits", "+123456789012345", "+123456789012345", nil},
		{"foreign landline is fine, only Ecuador is strict", "+551133334444", "+551133334444", nil},
		{"foreign number with a leading zero is kept", "+440987654321", "+440987654321", nil},

		{"three digits", "+123", "", ErrPhoneInvalid},
		{"sixteen digits", "+1234567890123456", "", ErrPhoneInvalid},
		{"no plus", "2025550123", "", ErrPhoneInvalid},
		{"plus after the digits", "1202555+0123", "", ErrPhoneInvalid},
		{"two pluses", "++12025550123", "", ErrPhoneInvalid},
		{"international dialling prefix instead of a plus", "0012025550123", "", ErrPhoneInvalid},
		{"letters", "+1202555ABCD", "", ErrPhoneInvalid},
		{"extension suffix", "+12025550123 ext 4", "", ErrPhoneInvalid},
		{"plus with no digits", "+", "", ErrPhoneInvalid},
		{"empty", "", "", ErrPhoneInvalid},
		{"whitespace only", "   ", "", ErrPhoneInvalid},
		{"punctuation only", "+()-", "", ErrPhoneInvalid},
		{"fullwidth digits are not digits", "+５９３９８７６５４３２１", "", ErrPhoneInvalid},
		{"arabic-indic digits are not digits", "+٥٩٣", "", ErrPhoneInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPhone(t, tt.phone, tt.want, tt.wantErr)
		})
	}
}

// TestPhoneNumberMessage pins which explanation a rejected number gets. The tier
// is read from what the buyer typed, not from what parsed, so someone who
// mistyped an Ecuadorian mobile is told about Ecuadorian mobiles.
func TestPhoneNumberMessage(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  string
	}{
		{"ecuadorian landline", "+59322345678", PhoneEcuadorMessage},
		{"ecuadorian number too short", "+59398765", PhoneEcuadorMessage},
		{"ecuadorian number with letters still gets its own tier", "+593 987 65432a", PhoneEcuadorMessage},
		{"foreign number too short", "+123", PhoneGenericMessage},
		{"foreign number too long", "+1234567890123456", PhoneGenericMessage},
		{"no dialling code at all", "0987654321", PhoneGenericMessage},
		{"empty", "", PhoneGenericMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PhoneNumberMessage(tt.phone); got != tt.want {
				t.Fatalf("PhoneNumberMessage(%q) = %q, want %q", tt.phone, got, tt.want)
			}
		})
	}
}

// TestPhoneFieldErrors pins the shape a handler surfaces: the field name it was
// given, and the message for the tier the number was aiming at. A valid number
// yields the canonical form and no errors at all.
func TestPhoneFieldErrors(t *testing.T) {
	t.Run("valid number normalises with no errors", func(t *testing.T) {
		got, fieldErrors := PhoneFieldErrors("customer_phone", "+593 (0)98 765 4321")
		if len(fieldErrors) != 0 {
			t.Fatalf("PhoneFieldErrors returned %v, want none", fieldErrors)
		}
		if got != "+593987654321" {
			t.Fatalf("PhoneFieldErrors = %q, want %q", got, "+593987654321")
		}
	})

	t.Run("invalid number names the field it was given", func(t *testing.T) {
		got, fieldErrors := PhoneFieldErrors("phone", "+59322345678")
		if got != "" {
			t.Fatalf("PhoneFieldErrors = %q, want empty on failure", got)
		}
		want := []FieldError{{Field: "phone", Message: PhoneEcuadorMessage}}
		if len(fieldErrors) != 1 || fieldErrors[0] != want[0] {
			t.Fatalf("PhoneFieldErrors errors = %v, want %v", fieldErrors, want)
		}
	})

	t.Run("a foreign number gets the generic message", func(t *testing.T) {
		_, fieldErrors := PhoneFieldErrors("customer_phone", "+12")
		if len(fieldErrors) != 1 || fieldErrors[0].Message != PhoneGenericMessage {
			t.Fatalf("PhoneFieldErrors errors = %v, want the generic message", fieldErrors)
		}
	})
}

// TestValidatePhoneIsIdempotent pins that the canonical form is a fixed point:
// the value is revalidated whenever it comes back off a Payment or a Customer
// profile, and a normaliser that changed its own output would corrupt it.
func TestValidatePhoneIsIdempotent(t *testing.T) {
	for _, phone := range []string{"+593987654321", "+12025550123", "+447911123456"} {
		once, err := ValidatePhone(phone)
		if err != nil {
			t.Fatalf("ValidatePhone(%q) err = %v, want nil", phone, err)
		}
		twice, err := ValidatePhone(once)
		if err != nil || twice != once {
			t.Fatalf("ValidatePhone(%q) = %q, %v; want %q, nil", once, twice, err, once)
		}
	}
}

func assertPhone(t *testing.T, phone, want string, wantErr error) {
	t.Helper()
	got, err := ValidatePhone(phone)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ValidatePhone(%q) err = %v, want %v", phone, err, wantErr)
	}
	if got != want {
		t.Fatalf("ValidatePhone(%q) = %q, want %q", phone, got, want)
	}
}
