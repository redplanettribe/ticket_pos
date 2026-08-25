package sri

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// loadTestCertificate reads the throwaway key pair committed under testdata
// (generated once for the golden file; it protects nothing).
func loadTestCertificate(t *testing.T) *Certificate {
	t.Helper()
	keyPEM, err := os.ReadFile(filepath.Join("testdata", "test_key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join("testdata", "test_cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := pem.Decode(keyPEM)
	key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return NewCertificate(key, cert, nil)
}

var pinnedSigningTime = time.Date(2026, 8, 25, 10, 31, 7, 0, Guayaquil)

func signGolden(t *testing.T) []byte {
	t.Helper()
	unsigned, err := os.ReadFile(filepath.Join("testdata", "golden", "factura_one_line.xml"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(unsigned, loadTestCertificate(t), SignOptions{SigningTime: pinnedSigningTime, Nonce: 654321})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestSignGolden(t *testing.T) {
	signed := signGolden(t)
	assertGolden(t, filepath.Join("testdata", "golden", "factura_one_line_signed.xml"), signed)
}

func TestSignLayoutFollowsTheFicha(t *testing.T) {
	signed := string(signGolden(t))
	if !strings.HasPrefix(signed, `<?xml version="1.0" encoding="UTF-8"?><factura id="comprobante" version="1.1.0">`) {
		t.Fatal("root must stay prefix-free")
	}
	if strings.Count(signed, "xmlns:ds=") != 1 || strings.Count(signed, "xmlns:etsi=") != 1 {
		t.Fatal("xmlns:ds and xmlns:etsi must be declared exactly once")
	}
	if !strings.Contains(signed, `<ds:Signature xmlns:ds="http://www.w3.org/2000/09/xmldsig#" xmlns:etsi="http://uri.etsi.org/01903/v1.3.2#" Id="Signature654321">`) {
		t.Fatal("namespaces must be declared on ds:Signature")
	}
	if !strings.HasSuffix(signed, "</ds:Signature></factura>") {
		t.Fatal("the signature must be the last child of the root")
	}
	for _, want := range []string{
		`<ds:CanonicalizationMethod Algorithm="http://www.w3.org/TR/2001/REC-xml-c14n-20010315">`,
		`<ds:SignatureMethod Algorithm="http://www.w3.org/2000/09/xmldsig#rsa-sha1">`,
		`<ds:Reference Id="SignedPropertiesID654321" Type="http://uri.etsi.org/01903#SignedProperties" URI="#Signature654321-SignedProperties654321">`,
		`<ds:Reference URI="#Certificate654321">`,
		`<ds:Reference Id="Reference-ID-654321" URI="#comprobante"><ds:Transforms><ds:Transform Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature">`,
		`<ds:DigestMethod Algorithm="http://www.w3.org/2000/09/xmldsig#sha1">`,
		`<ds:KeyInfo Id="Certificate654321"><ds:X509Data><ds:X509Certificate>`,
		`<ds:KeyValue><ds:RSAKeyValue><ds:Modulus>`,
		`<ds:Exponent>AQAB</ds:Exponent>`,
		`<etsi:QualifyingProperties Target="#Signature654321"><etsi:SignedProperties Id="Signature654321-SignedProperties654321">`,
		`<etsi:SigningTime>2026-08-25T10:31:07-05:00</etsi:SigningTime>`,
		`<etsi:DataObjectFormat ObjectReference="#Reference-ID-654321"><etsi:Description>contenido comprobante</etsi:Description><etsi:MimeType>text/xml</etsi:MimeType>`,
	} {
		if !strings.Contains(signed, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// TestSignedDocumentVerifiesIndependently recomputes every digest and the
// RSA signature with the XML parser, the C14N library and crypto/rsa
// directly — none of the package's Sign or Verify code — so the two cannot
// agree by sharing a mistake.
func TestSignedDocumentVerifiesIndependently(t *testing.T) {
	cert := loadTestCertificate(t)
	signed := signGolden(t)
	independentlyVerify(t, signed, &cert.Key.PublicKey, crypto.SHA1)
}

func independentlyVerify(t *testing.T, signed []byte, pub *rsa.PublicKey, h crypto.Hash) {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		t.Fatal(err)
	}
	c14n := dsig.MakeC14N10RecCanonicalizer()
	digest := func(el *etree.Element) string {
		b, err := c14n.Canonicalize(el)
		if err != nil {
			t.Fatal(err)
		}
		hh := h.New()
		hh.Write(b)
		return base64.StdEncoding.EncodeToString(hh.Sum(nil))
	}
	digestOf := func(refPath string) string {
		el := doc.FindElement(refPath)
		if el == nil {
			t.Fatalf("missing %s", refPath)
		}
		return el.Text()
	}

	// 1. SignedProperties.
	props := doc.FindElement("/factura/ds:Signature/ds:Object/etsi:QualifyingProperties/etsi:SignedProperties")
	want := digestOf("/factura/ds:Signature/ds:SignedInfo/ds:Reference[@Type='http://uri.etsi.org/01903#SignedProperties']/ds:DigestValue")
	if got := digest(props); got != want {
		t.Errorf("SignedProperties digest %s, document says %s", got, want)
	}
	// 2. KeyInfo.
	keyInfo := doc.FindElement("/factura/ds:Signature/ds:KeyInfo")
	want = digestOf("/factura/ds:Signature/ds:SignedInfo/ds:Reference[@URI='#" + keyInfo.SelectAttrValue("Id", "") + "']/ds:DigestValue")
	if got := digest(keyInfo); got != want {
		t.Errorf("KeyInfo digest %s, document says %s", got, want)
	}
	// 3. The document without its signature.
	stripped := etree.NewDocument()
	if err := stripped.ReadFromBytes(signed); err != nil {
		t.Fatal(err)
	}
	stripped.Root().RemoveChild(stripped.FindElement("/factura/ds:Signature"))
	want = digestOf("/factura/ds:Signature/ds:SignedInfo/ds:Reference[@URI='#comprobante']/ds:DigestValue")
	if got := digest(stripped.Root()); got != want {
		t.Errorf("document digest %s, document says %s", got, want)
	}
	// 4. The RSA signature over the canonical SignedInfo.
	siBytes, err := c14n.Canonicalize(doc.FindElement("/factura/ds:Signature/ds:SignedInfo"))
	if err != nil {
		t.Fatal(err)
	}
	hh := h.New()
	hh.Write(siBytes)
	sigValue, err := base64.StdEncoding.DecodeString(digestOf("/factura/ds:Signature/ds:SignatureValue"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(pub, h, hh.Sum(nil), sigValue); err != nil {
		t.Fatalf("RSA signature over SignedInfo does not verify: %v", err)
	}
	// 5. The certificate in KeyInfo is the signing certificate.
	certDER, err := base64.StdEncoding.DecodeString(digestOf("/factura/ds:Signature/ds:KeyInfo/ds:X509Data/ds:X509Certificate"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.PublicKey.(*rsa.PublicKey).Equal(pub) {
		t.Fatal("KeyInfo certificate is not the signer's")
	}
}

// TestDocumentDigestMatchesLibxml2C14N cross-checks the document reference
// with libxml2's own canonicalizer: the signature is cut out of the bytes
// textually, `xmllint --c14n` canonicalizes what is left, and its SHA-1
// must be the DigestValue the signer wrote.
func TestDocumentDigestMatchesLibxml2C14N(t *testing.T) {
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("xmllint not installed")
	}
	signed := signGolden(t)
	start := bytes.Index(signed, []byte("<ds:Signature "))
	end := bytes.LastIndex(signed, []byte("</ds:Signature>")) + len("</ds:Signature>")
	if start < 0 || end < start {
		t.Fatal("no signature found")
	}
	stripped := append(append([]byte{}, signed[:start]...), signed[end:]...)
	path := filepath.Join(t.TempDir(), "stripped.xml")
	if err := os.WriteFile(path, stripped, 0o644); err != nil {
		t.Fatal(err)
	}
	c14n, err := exec.Command(xmllint, "--c14n", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(c14n)
	got := base64.StdEncoding.EncodeToString(sum[:])
	m := regexp.MustCompile(`URI="#comprobante">.*?<ds:DigestValue>([^<]+)</ds:DigestValue>`).FindSubmatch(signed)
	if m == nil {
		t.Fatal("document DigestValue not found")
	}
	if got != string(m[1]) {
		t.Fatalf("libxml2 C14N digest %s, signer wrote %s", got, m[1])
	}
}

func TestSignedDocumentValidatesAgainstXSD(t *testing.T) {
	validateWithXSD(t, "signed", signGolden(t))
}

func TestVerifyAcceptsTheSignedDocument(t *testing.T) {
	cert := loadTestCertificate(t)
	v, err := Verify(signGolden(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Algorithm != SignatureRSASHA1 {
		t.Errorf("algorithm = %q", v.Algorithm)
	}
	if !v.SigningTime.Equal(pinnedSigningTime) {
		t.Errorf("signing time = %v", v.SigningTime)
	}
	if !v.Certificate.Equal(cert.Leaf) {
		t.Error("certificate differs")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	signed := signGolden(t)
	cases := map[string][]byte{
		"amount changed":         bytes.Replace(signed, []byte("<importeTotal>23.00</importeTotal>"), []byte("<importeTotal>22.00</importeTotal>"), 1),
		"signing time changed":   bytes.Replace(signed, []byte("2026-08-25T10:31:07-05:00"), []byte("2026-08-25T10:31:08-05:00"), 1),
		"signature value zeroed": regexp.MustCompile(`<ds:SignatureValue Id="[^"]+">[^<]+`).ReplaceAll(signed, []byte(`<ds:SignatureValue Id="x">AAAA`)),
		"unsigned":               mustRead(t, filepath.Join("testdata", "golden", "factura_one_line.xml")),
		"not xml":                []byte("<factura"),
	}
	for name, tampered := range cases {
		if bytes.Equal(tampered, signed) {
			t.Fatalf("%s: tampering did not change the bytes", name)
		}
		if _, err := Verify(tampered); !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("%s: err = %v, want ErrSignatureInvalid", name, err)
		}
	}
}

func TestVerifyRejectsForeignCertificate(t *testing.T) {
	// Swap the KeyInfo certificate for another one: the KeyInfo digest and
	// the signature both stop matching.
	signed := signGolden(t)
	_, other := newSelfSigned(t, ecuadorianSubject())
	re := regexp.MustCompile(`<ds:X509Certificate>[^<]+`)
	swapped := re.ReplaceAll(signed, []byte("<ds:X509Certificate>"+base64.StdEncoding.EncodeToString(other.Raw)))
	if _, err := Verify(swapped); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSignWithFreshKeyBothAlgorithms(t *testing.T) {
	key, leaf := newSelfSigned(t, ecuadorianSubject())
	cert := NewCertificate(key, leaf, nil)
	built, err := BuildFactura(testFactura(t, mixedLines()))
	if err != nil {
		t.Fatal(err)
	}
	for _, alg := range []SignatureAlgorithm{SignatureRSASHA1, SignatureRSASHA256} {
		signed, err := Sign(built.XML, cert, SignOptions{Algorithm: alg})
		if err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		v, err := Verify(signed)
		if err != nil {
			t.Fatalf("%s: verify: %v", alg, err)
		}
		if v.Algorithm != alg {
			t.Errorf("algorithm = %q, want %q", v.Algorithm, alg)
		}
		h := crypto.SHA1
		if alg == SignatureRSASHA256 {
			h = crypto.SHA256
		}
		independentlyVerify(t, signed, &key.PublicKey, h)
		if !regexp.MustCompile(`Id="Signature[0-9]{6}"`).Match(signed) {
			t.Errorf("%s: ids are not numbered", alg)
		}
		if strings.Contains(string(signed), "<etsi:SigningTime>0001-") {
			t.Errorf("%s: signing time defaulted to zero", alg)
		}
	}
}

func TestSignRefuses(t *testing.T) {
	cert := loadTestCertificate(t)
	if _, err := Sign([]byte(`<factura version="1.1.0"><a/></factura>`), cert, SignOptions{}); !errors.Is(err, ErrSign) {
		t.Errorf("missing id: %v", err)
	}
	if _, err := Sign(signGolden(t), cert, SignOptions{}); !errors.Is(err, ErrSign) {
		t.Errorf("already signed: %v", err)
	}
	if _, err := Sign([]byte(`<factura id="comprobante"`), cert, SignOptions{}); !errors.Is(err, ErrSign) {
		t.Errorf("malformed: %v", err)
	}
	if _, err := Sign([]byte(`<factura id="comprobante"/>`), cert, SignOptions{Algorithm: "md5"}); !errors.Is(err, ErrSign) {
		t.Errorf("unknown algorithm: %v", err)
	}
	if _, err := Sign([]byte(`<factura id="comprobante"/>`), nil, SignOptions{}); !errors.Is(err, ErrSign) {
		t.Errorf("nil certificate: %v", err)
	}
}
