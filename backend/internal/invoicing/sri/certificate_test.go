package sri

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// newSelfSigned makes a throwaway RSA key and self-signed certificate.
func newSelfSigned(t *testing.T, subject pkix.Name, extra ...pkix.Extension) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:    big.NewInt(424242),
		Subject:         subject,
		NotBefore:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:        time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: extra,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func ecuadorianSubject() pkix.Name {
	return pkix.Name{
		CommonName:   "JUAN PEREZ",
		Organization: []string{"TICKET POS S.A.S."},
		Country:      []string{"EC"},
		SerialNumber: "1790012345001",
	}
}

func TestOpenCertificateRoundTrip(t *testing.T) {
	key, cert := newSelfSigned(t, ecuadorianSubject())
	p12, err := pkcs12.Modern.Encode(key, cert, nil, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenCertificate(p12, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Key.Equal(key) {
		t.Fatal("key differs")
	}
	if !opened.Leaf.Equal(cert) {
		t.Fatal("certificate differs")
	}
	md := opened.Metadata
	if md.RUC != "1790012345001" {
		t.Errorf("RUC = %q", md.RUC)
	}
	if md.SerialNumber != "424242" {
		t.Errorf("serial = %q", md.SerialNumber)
	}
	if md.Subject != cert.Subject.String() || md.Issuer != cert.Issuer.String() {
		t.Errorf("subject/issuer = %q / %q", md.Subject, md.Issuer)
	}
	if !md.NotBefore.Equal(cert.NotBefore) || !md.NotAfter.Equal(cert.NotAfter) {
		t.Errorf("validity = %v..%v", md.NotBefore, md.NotAfter)
	}
	sum := sha256.Sum256(cert.Raw)
	if md.FingerprintSHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("fingerprint = %q", md.FingerprintSHA256)
	}
	if len(opened.Chain) != 0 {
		t.Errorf("chain = %d", len(opened.Chain))
	}
}

func TestOpenCertificateLegacyRC2(t *testing.T) {
	key, cert := newSelfSigned(t, ecuadorianSubject())
	p12, err := pkcs12.LegacyRC2.Encode(key, cert, nil, "clave")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenCertificate(p12, "clave")
	if err != nil {
		t.Fatalf("RC2-40 legacy file: %v", err)
	}
	if !opened.Key.Equal(key) {
		t.Fatal("key differs")
	}
}

func TestOpenCertificateWithChain(t *testing.T) {
	key, cert := newSelfSigned(t, ecuadorianSubject())
	_, ca := newSelfSigned(t, pkix.Name{CommonName: "AC PRUEBAS"})
	p12, err := pkcs12.Modern.Encode(key, cert, []*x509.Certificate{ca}, "x")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenCertificate(p12, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Chain) != 1 || !opened.Chain[0].Equal(ca) {
		t.Fatalf("chain = %v", opened.Chain)
	}
}

func TestOpenCertificateWrongPassword(t *testing.T) {
	key, cert := newSelfSigned(t, ecuadorianSubject())
	for name, enc := range map[string]*pkcs12.Encoder{"modern": pkcs12.Modern, "legacy": pkcs12.LegacyRC2} {
		p12, err := enc.Encode(key, cert, nil, "right")
		if err != nil {
			t.Fatal(err)
		}
		_, err = OpenCertificate(p12, "wrong")
		if !errors.Is(err, ErrCertificatePassword) {
			t.Errorf("%s: err = %v, want ErrCertificatePassword", name, err)
		}
	}
}

func TestOpenCertificateNoRSAKey(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: ecuadorianSubject(),
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &ecKey.PublicKey, ecKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	p12, err := pkcs12.Modern.Encode(ecKey, cert, nil, "pw")
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenCertificate(p12, "pw")
	if !errors.Is(err, ErrCertificateNoRSAKey) {
		t.Fatalf("err = %v, want ErrCertificateNoRSAKey", err)
	}
}

func TestOpenCertificateGarbage(t *testing.T) {
	_, err := OpenCertificate([]byte("not a pkcs12 file"), "pw")
	if !errors.Is(err, ErrCertificateInvalid) {
		t.Fatalf("err = %v, want ErrCertificateInvalid", err)
	}
	if errors.Is(err, ErrCertificatePassword) {
		t.Fatal("garbage must not be reported as a wrong password")
	}
}

func TestCertificateRUCFromExtension(t *testing.T) {
	raw, err := asn1.Marshal("0992345678001")
	if err != nil {
		t.Fatal(err)
	}
	ext := pkix.Extension{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37746, 3, 11}, Value: raw}
	key, cert := newSelfSigned(t, pkix.Name{CommonName: "SIN RUC EN SUBJECT", Country: []string{"EC"}}, ext)
	c := NewCertificate(key, cert, nil)
	if c.Metadata.RUC != "0992345678001" {
		t.Fatalf("RUC = %q", c.Metadata.RUC)
	}
}

func TestCertificateRUCAbsent(t *testing.T) {
	key, cert := newSelfSigned(t, pkix.Name{CommonName: "SIN RUC", SerialNumber: "12345"})
	if ruc := NewCertificate(key, cert, nil).Metadata.RUC; ruc != "" {
		t.Fatalf("RUC = %q, want empty", ruc)
	}
}
