package sri

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"
)

// Guayaquil is Ecuador's mainland time zone (UTC-5, no daylight saving).
// Every date the SRI sees — the clave de acceso, fechaEmision, the signing
// time — is computed in it, from whatever instant the caller holds.
var Guayaquil = time.FixedZone("America/Guayaquil", -5*60*60)

// Environment is the SRI "ambiente" digit: "1" pruebas, "2" producción.
type Environment string

const (
	// EnvironmentTest is the SRI ambiente de pruebas (celcer.sri.gob.ec).
	EnvironmentTest Environment = "1"
	// EnvironmentProduction is the SRI ambiente de producción (cel.sri.gob.ec).
	EnvironmentProduction Environment = "2"
)

// Valid reports whether the environment is one of the two SRI ambientes.
func (e Environment) Valid() bool {
	return e == EnvironmentTest || e == EnvironmentProduction
}

// DocumentTypeFactura is the SRI "codDoc" for a factura (Ficha Tabla 3).
const DocumentTypeFactura = "01"

// DocumentTypeNotaCredito is the SRI "codDoc" for a nota de crédito (Ficha
// Tabla 3): the Credit Note that undoes a factura (#476, ADR 0060).
const DocumentTypeNotaCredito = "04"

// TipoEmisionNormal is the only tipo de emisión in the offline scheme
// (Ficha v2.34 Tabla 2).
const TipoEmisionNormal = "1"

// ErrInvalidAccessKey is returned when a clave de acceso, or one of its
// components, is malformed.
var ErrInvalidAccessKey = errors.New("sri: invalid clave de acceso")

// AccessKeyInput are the components of a clave de acceso (Ficha Tabla 1).
type AccessKeyInput struct {
	// IssuedOn is the emission instant; only its date in Guayaquil is used.
	IssuedOn time.Time
	// DocumentType is the codDoc, e.g. DocumentTypeFactura.
	DocumentType string
	// RUC is the emisor's 13-digit RUC.
	RUC string
	// Environment is the ambiente digit.
	Environment Environment
	// Establishment is the 3-digit establecimiento code.
	Establishment string
	// EmissionPoint is the 3-digit punto de emisión code.
	EmissionPoint string
	// Sequential is the secuencial, already zero-padded to 9 digits.
	// An unpadded value is refused, never padded silently (Ficha §5.2).
	Sequential string
	// NumericCode is the emisor's 8-digit código numérico; see RandomNumericCode.
	NumericCode string
}

var (
	digits2   = regexp.MustCompile(`^[0-9]{2}$`)
	digits3   = regexp.MustCompile(`^[0-9]{3}$`)
	digits8   = regexp.MustCompile(`^[0-9]{8}$`)
	digits9   = regexp.MustCompile(`^[0-9]{9}$`)
	digits13  = regexp.MustCompile(`^[0-9]{13}$`)
	digitsAny = regexp.MustCompile(`^[0-9]+$`)
)

// NewAccessKey builds the 49-digit clave de acceso: the 48 components in
// Ficha Tabla 1 order followed by the módulo 11 check digit.
func NewAccessKey(in AccessKeyInput) (string, error) {
	if in.IssuedOn.IsZero() {
		return "", fmt.Errorf("%w: emission date is zero", ErrInvalidAccessKey)
	}
	if !digits2.MatchString(in.DocumentType) {
		return "", fmt.Errorf("%w: document type %q is not two digits", ErrInvalidAccessKey, in.DocumentType)
	}
	if !digits13.MatchString(in.RUC) {
		return "", fmt.Errorf("%w: RUC %q is not 13 digits", ErrInvalidAccessKey, in.RUC)
	}
	if !in.Environment.Valid() {
		return "", fmt.Errorf("%w: environment %q is not 1 or 2", ErrInvalidAccessKey, in.Environment)
	}
	if !digits3.MatchString(in.Establishment) {
		return "", fmt.Errorf("%w: establishment %q is not three digits", ErrInvalidAccessKey, in.Establishment)
	}
	if !digits3.MatchString(in.EmissionPoint) {
		return "", fmt.Errorf("%w: emission point %q is not three digits", ErrInvalidAccessKey, in.EmissionPoint)
	}
	if !digits9.MatchString(in.Sequential) {
		return "", fmt.Errorf("%w: sequential %q is not nine digits", ErrInvalidAccessKey, in.Sequential)
	}
	if !digits8.MatchString(in.NumericCode) {
		return "", fmt.Errorf("%w: numeric code %q is not eight digits", ErrInvalidAccessKey, in.NumericCode)
	}
	body := in.IssuedOn.In(Guayaquil).Format("02012006") +
		in.DocumentType + in.RUC + string(in.Environment) +
		in.Establishment + in.EmissionPoint + in.Sequential + in.NumericCode + TipoEmisionNormal
	check, err := CheckDigit(body)
	if err != nil {
		return "", err
	}
	return body + string(rune('0'+check)), nil
}

// CheckDigit computes the módulo 11 check digit of a digit string, as the
// Ficha §5.2 prescribes: weights 2,3,4,5,6,7 cycling from the rightmost digit
// leftwards, 11 - (sum mod 11), with 11 → 0 and 10 → 1.
func CheckDigit(digits string) (int, error) {
	if !digitsAny.MatchString(digits) {
		return 0, fmt.Errorf("%w: %q is not a digit string", ErrInvalidAccessKey, digits)
	}
	sum := 0
	weight := 2
	for i := len(digits) - 1; i >= 0; i-- {
		sum += int(digits[i]-'0') * weight
		weight++
		if weight > 7 {
			weight = 2
		}
	}
	result := 11 - (sum % 11)
	switch result {
	case 11:
		return 0, nil
	case 10:
		return 1, nil
	default:
		return result, nil
	}
}

// ValidateAccessKey checks that a clave de acceso is 49 digits whose last
// digit is the módulo 11 check digit of the first 48.
func ValidateAccessKey(key string) error {
	if len(key) != 49 || !digitsAny.MatchString(key) {
		return fmt.Errorf("%w: %q is not 49 digits", ErrInvalidAccessKey, key)
	}
	check, err := CheckDigit(key[:48])
	if err != nil {
		return err
	}
	if int(key[48]-'0') != check {
		return fmt.Errorf("%w: check digit %c, expected %d", ErrInvalidAccessKey, key[48], check)
	}
	return nil
}

// ParseAccessKey splits a valid clave de acceso back into its components.
// IssuedOn is midnight of the emission date in Guayaquil.
func ParseAccessKey(key string) (AccessKeyInput, error) {
	if err := ValidateAccessKey(key); err != nil {
		return AccessKeyInput{}, err
	}
	issued, err := time.ParseInLocation("02012006", key[0:8], Guayaquil)
	if err != nil {
		return AccessKeyInput{}, fmt.Errorf("%w: date %q: %v", ErrInvalidAccessKey, key[0:8], err)
	}
	return AccessKeyInput{
		IssuedOn:      issued,
		DocumentType:  key[8:10],
		RUC:           key[10:23],
		Environment:   Environment(key[23:24]),
		Establishment: key[24:27],
		EmissionPoint: key[27:30],
		Sequential:    key[30:39],
		NumericCode:   key[39:47],
	}, nil
}

// RandomNumericCode draws the 8-digit código numérico from crypto/rand.
// The Ficha leaves the value to the emisor ("potestad absoluta del
// contribuyente emisor"); randomness only makes claves unguessable.
func RandomNumericCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return "", fmt.Errorf("sri: numeric code: %w", err)
	}
	return fmt.Sprintf("%08d", n.Int64()), nil
}

// FormatSequential zero-pads a positive secuencial to the 9 digits the clave
// de acceso and the XML require.
func FormatSequential(n int64) (string, error) {
	if n < 1 || n > 999_999_999 {
		return "", fmt.Errorf("%w: sequential %d is outside 1..999999999", ErrInvalidAccessKey, n)
	}
	return fmt.Sprintf("%09d", n), nil
}
