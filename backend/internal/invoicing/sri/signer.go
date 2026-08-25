package sri

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strconv"
	"time"

	"github.com/beevik/etree"
)

// SignatureAlgorithm selects the digest and RSA signature algorithms.
type SignatureAlgorithm string

const (
	// SignatureRSASHA1 is what the Ficha prescribes (§6.8) and the default.
	SignatureRSASHA1 SignatureAlgorithm = "rsa-sha1"
	// SignatureRSASHA256 is offered for the day the SRI accepts it; it is
	// unverified against the SRI's validator.
	SignatureRSASHA256 SignatureAlgorithm = "rsa-sha256"
)

const (
	dsNamespace   = "http://www.w3.org/2000/09/xmldsig#"
	etsiNamespace = "http://uri.etsi.org/01903/v1.3.2#"

	algC14N      = "http://www.w3.org/TR/2001/REC-xml-c14n-20010315"
	algEnveloped = "http://www.w3.org/2000/09/xmldsig#enveloped-signature"
	algSHA1      = "http://www.w3.org/2000/09/xmldsig#sha1"
	algRSASHA1   = "http://www.w3.org/2000/09/xmldsig#rsa-sha1"
	algSHA256    = "http://www.w3.org/2001/04/xmlenc#sha256"
	algRSASHA256 = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"

	signedPropertiesType = "http://uri.etsi.org/01903#SignedProperties"
	comprobanteID        = "comprobante"
)

// ErrSign is wrapped by signing failures.
var ErrSign = errors.New("sri: sign")

// SignOptions tune Sign. The zero value signs RSA-SHA1, now, with random ids.
type SignOptions struct {
	Algorithm SignatureAlgorithm
	// SigningTime is written to etsi:SigningTime in Guayaquil time; zero
	// means time.Now().
	SigningTime time.Time
	// Nonce numbers the signature element ids (Signature123456, ...). Zero
	// draws a random one; tests pin it for golden output.
	Nonce uint32
}

type algorithmSpec struct {
	digestURI, signatureURI string
	hash                    crypto.Hash
	newHash                 func() hash.Hash
}

func lookupAlgorithm(a SignatureAlgorithm) (algorithmSpec, error) {
	switch a {
	case "", SignatureRSASHA1:
		return algorithmSpec{algSHA1, algRSASHA1, crypto.SHA1, sha1.New}, nil
	case SignatureRSASHA256:
		return algorithmSpec{algSHA256, algRSASHA256, crypto.SHA256, sha256.New}, nil
	}
	return algorithmSpec{}, fmt.Errorf("%w: unknown algorithm %q", ErrSign, a)
}

func algorithmByURIs(digestURI, signatureURI string) (SignatureAlgorithm, algorithmSpec, bool) {
	for _, a := range []SignatureAlgorithm{SignatureRSASHA1, SignatureRSASHA256} {
		spec, _ := lookupAlgorithm(a)
		if spec.digestURI == digestURI && spec.signatureURI == signatureURI {
			return a, spec, true
		}
	}
	return "", algorithmSpec{}, false
}

// Sign wraps an unsigned comprobante (root with id="comprobante") in an
// enveloped XAdES-BES signature the way the Ficha (§6, Anexo 4) lays it out:
// ds:Signature appended as the last child of the root, xmlns:ds and
// xmlns:etsi declared on the signature only, inclusive C14N, and three
// references — etsi:SignedProperties, ds:KeyInfo, and the document itself
// through the enveloped-signature transform. KeyInfo carries the leaf
// certificate and its RSA public key.
func Sign(unsigned []byte, cert *Certificate, opts SignOptions) ([]byte, error) {
	if cert == nil || cert.Key == nil || cert.Leaf == nil {
		return nil, fmt.Errorf("%w: certificate is incomplete", ErrSign)
	}
	spec, err := lookupAlgorithm(opts.Algorithm)
	if err != nil {
		return nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(unsigned); err != nil {
		return nil, fmt.Errorf("%w: parse document: %v", ErrSign, err)
	}
	root := doc.Root()
	if root == nil {
		return nil, fmt.Errorf("%w: document has no root element", ErrSign)
	}
	if root.SelectAttrValue("id", "") != comprobanteID {
		return nil, fmt.Errorf("%w: root element must carry id=%q", ErrSign, comprobanteID)
	}
	for _, child := range root.ChildElements() {
		if child.Tag == "Signature" && child.NamespaceURI() == dsNamespace {
			return nil, fmt.Errorf("%w: document is already signed", ErrSign)
		}
	}

	nonce := opts.Nonce
	if nonce == 0 {
		n, err := rand.Int(rand.Reader, big.NewInt(900_000))
		if err != nil {
			return nil, fmt.Errorf("%w: nonce: %v", ErrSign, err)
		}
		nonce = uint32(n.Int64()) + 100_000
	}
	signingTime := opts.SigningTime
	if signingTime.IsZero() {
		signingTime = time.Now()
	}
	ids := newSignatureIDs(nonce)

	// The document digest is taken over the root as it stands before the
	// signature is appended — exactly what the enveloped-signature transform
	// yields on the signed document.
	docDigest, err := digestElement(root, spec)
	if err != nil {
		return nil, err
	}

	sig := etree.NewElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsNamespace)
	sig.CreateAttr("xmlns:etsi", etsiNamespace)
	sig.CreateAttr("Id", ids.signature)
	root.AddChild(sig)

	signedInfo := sig.CreateElement("ds:SignedInfo")
	signedInfo.CreateAttr("Id", ids.signedInfo)
	signedInfo.CreateElement("ds:CanonicalizationMethod").CreateAttr("Algorithm", algC14N)
	signedInfo.CreateElement("ds:SignatureMethod").CreateAttr("Algorithm", spec.signatureURI)

	refProps := signedInfo.CreateElement("ds:Reference")
	refProps.CreateAttr("Id", ids.signedPropertiesRef)
	refProps.CreateAttr("Type", signedPropertiesType)
	refProps.CreateAttr("URI", "#"+ids.signedProperties)
	refProps.CreateElement("ds:DigestMethod").CreateAttr("Algorithm", spec.digestURI)
	refPropsDigest := refProps.CreateElement("ds:DigestValue")

	refKey := signedInfo.CreateElement("ds:Reference")
	refKey.CreateAttr("URI", "#"+ids.keyInfo)
	refKey.CreateElement("ds:DigestMethod").CreateAttr("Algorithm", spec.digestURI)
	refKeyDigest := refKey.CreateElement("ds:DigestValue")

	refDoc := signedInfo.CreateElement("ds:Reference")
	refDoc.CreateAttr("Id", ids.documentRef)
	refDoc.CreateAttr("URI", "#"+comprobanteID)
	refDoc.CreateElement("ds:Transforms").CreateElement("ds:Transform").CreateAttr("Algorithm", algEnveloped)
	refDoc.CreateElement("ds:DigestMethod").CreateAttr("Algorithm", spec.digestURI)
	refDoc.CreateElement("ds:DigestValue").SetText(docDigest)

	signatureValue := sig.CreateElement("ds:SignatureValue")
	signatureValue.CreateAttr("Id", ids.signatureValue)

	keyInfo := sig.CreateElement("ds:KeyInfo")
	keyInfo.CreateAttr("Id", ids.keyInfo)
	keyInfo.CreateElement("ds:X509Data").CreateElement("ds:X509Certificate").SetText(base64.StdEncoding.EncodeToString(cert.Leaf.Raw))
	rsaValue := keyInfo.CreateElement("ds:KeyValue").CreateElement("ds:RSAKeyValue")
	rsaValue.CreateElement("ds:Modulus").SetText(base64.StdEncoding.EncodeToString(cert.Key.PublicKey.N.Bytes()))
	rsaValue.CreateElement("ds:Exponent").SetText(base64.StdEncoding.EncodeToString(big.NewInt(int64(cert.Key.PublicKey.E)).Bytes()))

	object := sig.CreateElement("ds:Object")
	object.CreateAttr("Id", ids.object)
	qualifying := object.CreateElement("etsi:QualifyingProperties")
	qualifying.CreateAttr("Target", "#"+ids.signature)
	signedProps := qualifying.CreateElement("etsi:SignedProperties")
	signedProps.CreateAttr("Id", ids.signedProperties)
	ssp := signedProps.CreateElement("etsi:SignedSignatureProperties")
	ssp.CreateElement("etsi:SigningTime").SetText(signingTime.In(Guayaquil).Format("2006-01-02T15:04:05-07:00"))
	certEl := ssp.CreateElement("etsi:SigningCertificate").CreateElement("etsi:Cert")
	certDigest := certEl.CreateElement("etsi:CertDigest")
	certDigest.CreateElement("ds:DigestMethod").CreateAttr("Algorithm", spec.digestURI)
	certDigest.CreateElement("ds:DigestValue").SetText(digestBytes(cert.Leaf.Raw, spec))
	issuerSerial := certEl.CreateElement("etsi:IssuerSerial")
	issuerSerial.CreateElement("ds:X509IssuerName").SetText(cert.Leaf.Issuer.String())
	issuerSerial.CreateElement("ds:X509SerialNumber").SetText(cert.Leaf.SerialNumber.String())
	format := signedProps.CreateElement("etsi:SignedDataObjectProperties").CreateElement("etsi:DataObjectFormat")
	format.CreateAttr("ObjectReference", "#"+ids.documentRef)
	format.CreateElement("etsi:Description").SetText("contenido comprobante")
	format.CreateElement("etsi:MimeType").SetText("text/xml")

	// SignedProperties and KeyInfo are digested in place, so the namespace
	// declarations inherited from ds:Signature are rendered on them the way
	// inclusive C14N renders a document subset.
	propsDigest, err := digestElement(signedProps, spec)
	if err != nil {
		return nil, err
	}
	refPropsDigest.SetText(propsDigest)
	keyDigest, err := digestElement(keyInfo, spec)
	if err != nil {
		return nil, err
	}
	refKeyDigest.SetText(keyDigest)

	signedInfoC14N, err := canonicalize(signedInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize SignedInfo: %v", ErrSign, err)
	}
	h := spec.newHash()
	h.Write(signedInfoC14N)
	signature, err := rsa.SignPKCS1v15(rand.Reader, cert.Key, spec.hash, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("%w: rsa: %v", ErrSign, err)
	}
	signatureValue.SetText(base64.StdEncoding.EncodeToString(signature))

	return serializeDocument(root)
}

type signatureIDs struct {
	signature, signedInfo, signedPropertiesRef, signedProperties, keyInfo, documentRef, signatureValue, object string
}

func newSignatureIDs(nonce uint32) signatureIDs {
	n := strconv.FormatUint(uint64(nonce), 10)
	return signatureIDs{
		signature:           "Signature" + n,
		signedInfo:          "Signature-SignedInfo" + n,
		signedPropertiesRef: "SignedPropertiesID" + n,
		signedProperties:    "Signature" + n + "-SignedProperties" + n,
		keyInfo:             "Certificate" + n,
		documentRef:         "Reference-ID-" + n,
		signatureValue:      "SignatureValue" + n,
		object:              "Signature" + n + "-Object" + n,
	}
}

func digestElement(el *etree.Element, spec algorithmSpec) (string, error) {
	c14n, err := canonicalize(el)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize %s: %v", ErrSign, el.FullTag(), err)
	}
	return digestBytes(c14n, spec), nil
}

func digestBytes(b []byte, spec algorithmSpec) string {
	h := spec.newHash()
	h.Write(b)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
