package sri

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

var (
	// ErrCertificatePassword means the .p12 could not be opened with the
	// password given.
	ErrCertificatePassword = errors.New("sri: certificate password is incorrect")
	// ErrCertificateNoRSAKey means the .p12 opened but holds no RSA private key.
	ErrCertificateNoRSAKey = errors.New("sri: certificate has no RSA private key")
	// ErrCertificateInvalid means the bytes are not a usable PKCS#12 file.
	ErrCertificateInvalid = errors.New("sri: certificate file is not valid")
)

// Certificate is an opened signing certificate: the key that signs, the
// leaf certificate that goes into KeyInfo, and the metadata ticket #453
// records on the Issuer.
type Certificate struct {
	Key      *rsa.PrivateKey
	Leaf     *x509.Certificate
	Chain    []*x509.Certificate
	Metadata CertificateMetadata
}

// CertificateMetadata is what the Issuer page shows about the certificate.
type CertificateMetadata struct {
	// Subject and Issuer are RFC 2253 distinguished names.
	Subject string
	Issuer  string
	// SerialNumber is the certificate serial in decimal.
	SerialNumber string
	// RUC is the 13-digit RUC found in the subject or an extension, or ""
	// when none was found. Best effort: Ecuadorian CAs place it in different
	// attributes, and the SRI's own check is the final word.
	RUC       string
	NotBefore time.Time
	NotAfter  time.Time
	// FingerprintSHA256 is the lowercase hex SHA-256 of the DER certificate.
	FingerprintSHA256 string
}

// OpenCertificate parses a PKCS#12 (.p12) file with its password. Legacy
// RC2-40 / 3DES encryption, common among Ecuadorian CAs, is supported.
func OpenCertificate(p12 []byte, password string) (*Certificate, error) {
	key, leaf, chain, err := pkcs12.DecodeChain(p12, password)
	if err != nil {
		if errors.Is(err, pkcs12.ErrIncorrectPassword) || errors.Is(err, pkcs12.ErrDecryption) {
			return nil, ErrCertificatePassword
		}
		return nil, fmt.Errorf("%w: %v", ErrCertificateInvalid, err)
	}
	if leaf == nil {
		return nil, fmt.Errorf("%w: no certificate", ErrCertificateInvalid)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrCertificateNoRSAKey
	}
	return NewCertificate(rsaKey, leaf, chain), nil
}

// NewCertificate assembles a Certificate from parsed parts and computes its
// metadata; tests and harnesses that generate a throwaway key use it.
func NewCertificate(key *rsa.PrivateKey, leaf *x509.Certificate, chain []*x509.Certificate) *Certificate {
	sum := sha256.Sum256(leaf.Raw)
	return &Certificate{
		Key:   key,
		Leaf:  leaf,
		Chain: chain,
		Metadata: CertificateMetadata{
			Subject:           leaf.Subject.String(),
			Issuer:            leaf.Issuer.String(),
			SerialNumber:      leaf.SerialNumber.String(),
			RUC:               findRUC(leaf),
			NotBefore:         leaf.NotBefore,
			NotAfter:          leaf.NotAfter,
			FingerprintSHA256: hex.EncodeToString(sum[:]),
		},
	}
}

var rucPattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]{10}001)(?:[^0-9]|$)`)

// findRUC scans the subject attributes, then every extension's printable
// content, for a 13-digit RUC.
func findRUC(cert *x509.Certificate) string {
	for _, atv := range cert.Subject.Names {
		if s, ok := atv.Value.(string); ok {
			if m := rucPattern.FindStringSubmatch(s); m != nil {
				return m[1]
			}
		}
	}
	for _, ext := range cert.Extensions {
		if m := rucPattern.FindStringSubmatch(printable(ext.Value)); m != nil {
			return m[1]
		}
	}
	return ""
}

// printable keeps ASCII printable bytes and turns the rest into spaces, so
// a RUC embedded in a DER structure is found as a delimited digit run.
func printable(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 0x20 && c < 0x7f {
			out[i] = c
		} else {
			out[i] = ' '
		}
	}
	return string(out)
}
