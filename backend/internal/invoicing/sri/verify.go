package sri

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beevik/etree"
)

// ErrSignatureInvalid is wrapped by every Verify failure.
var ErrSignatureInvalid = errors.New("sri: signature invalid")

// VerifiedSignature describes a signature Verify accepted.
type VerifiedSignature struct {
	Algorithm   SignatureAlgorithm
	SigningTime time.Time
	// Certificate is the signing certificate carried in KeyInfo. Verify
	// checks the signature against it; it does not check trust or expiry —
	// the SRI does that with the CA chain and OCSP.
	Certificate *x509.Certificate
}

// Verify checks a signed comprobante independently of how it was produced:
// it parses the bytes, recomputes the digest of every reference (the
// document with the signature removed, SignedProperties and KeyInfo), and
// verifies the RSA signature over the canonical SignedInfo with the
// certificate in KeyInfo. It insists on the Ficha's shape: inclusive C14N,
// exactly the three references, the document one enveloped.
func Verify(signed []byte) (*VerifiedSignature, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		return nil, fmt.Errorf("%w: parse: %v", ErrSignatureInvalid, err)
	}
	root := doc.Root()
	if root == nil {
		return nil, fmt.Errorf("%w: no root element", ErrSignatureInvalid)
	}
	sig := childNS(root, dsNamespace, "Signature")
	if sig == nil {
		return nil, fmt.Errorf("%w: no ds:Signature under the root", ErrSignatureInvalid)
	}
	signedInfo := childNS(sig, dsNamespace, "SignedInfo")
	if signedInfo == nil {
		return nil, fmt.Errorf("%w: no ds:SignedInfo", ErrSignatureInvalid)
	}
	if m := childNS(signedInfo, dsNamespace, "CanonicalizationMethod"); m == nil || m.SelectAttrValue("Algorithm", "") != algC14N {
		return nil, fmt.Errorf("%w: canonicalization method is not inclusive C14N", ErrSignatureInvalid)
	}
	sigMethod := childNS(signedInfo, dsNamespace, "SignatureMethod")
	if sigMethod == nil {
		return nil, fmt.Errorf("%w: no ds:SignatureMethod", ErrSignatureInvalid)
	}
	keyInfo := childNS(sig, dsNamespace, "KeyInfo")
	if keyInfo == nil {
		return nil, fmt.Errorf("%w: no ds:KeyInfo", ErrSignatureInvalid)
	}
	signedProps := findNS(sig, etsiNamespace, "SignedProperties")
	if signedProps == nil {
		return nil, fmt.Errorf("%w: no etsi:SignedProperties", ErrSignatureInvalid)
	}

	var algorithm SignatureAlgorithm
	var spec algorithmSpec
	var sawDoc, sawProps, sawKey bool
	refs := childrenNS(signedInfo, dsNamespace, "Reference")
	if len(refs) != 3 {
		return nil, fmt.Errorf("%w: %d references, the Ficha requires 3", ErrSignatureInvalid, len(refs))
	}
	for _, ref := range refs {
		uri := ref.SelectAttrValue("URI", "")
		if !strings.HasPrefix(uri, "#") {
			return nil, fmt.Errorf("%w: reference URI %q is not a fragment", ErrSignatureInvalid, uri)
		}
		digestMethod := childNS(ref, dsNamespace, "DigestMethod")
		digestValue := childNS(ref, dsNamespace, "DigestValue")
		if digestMethod == nil || digestValue == nil {
			return nil, fmt.Errorf("%w: reference %q lacks digest method or value", ErrSignatureInvalid, uri)
		}
		a, s, ok := algorithmByURIs(digestMethod.SelectAttrValue("Algorithm", ""), sigMethod.SelectAttrValue("Algorithm", ""))
		if !ok {
			return nil, fmt.Errorf("%w: unsupported algorithms for reference %q", ErrSignatureInvalid, uri)
		}
		if algorithm != "" && a != algorithm {
			return nil, fmt.Errorf("%w: references use different digest algorithms", ErrSignatureInvalid)
		}
		algorithm, spec = a, s

		enveloped := false
		if transforms := childNS(ref, dsNamespace, "Transforms"); transforms != nil {
			for _, tr := range childrenNS(transforms, dsNamespace, "Transform") {
				switch tr.SelectAttrValue("Algorithm", "") {
				case algEnveloped:
					enveloped = true
				case algC14N:
				default:
					return nil, fmt.Errorf("%w: unsupported transform on %q", ErrSignatureInvalid, uri)
				}
			}
		}

		id := uri[1:]
		var target *etree.Element
		switch {
		case id == comprobanteID:
			if root.SelectAttrValue("id", "") != comprobanteID || !enveloped {
				return nil, fmt.Errorf("%w: the document reference must be enveloped and point at the root", ErrSignatureInvalid)
			}
			copyRoot := root.Copy()
			copySig := childNS(copyRoot, dsNamespace, "Signature")
			copyRoot.RemoveChild(copySig)
			target = copyRoot
			sawDoc = true
		case signedProps.SelectAttrValue("Id", "") == id:
			if ref.SelectAttrValue("Type", "") != signedPropertiesType {
				return nil, fmt.Errorf("%w: SignedProperties reference lacks its Type", ErrSignatureInvalid)
			}
			target = signedProps
			sawProps = true
		case keyInfo.SelectAttrValue("Id", "") == id:
			target = keyInfo
			sawKey = true
		default:
			return nil, fmt.Errorf("%w: reference %q points outside the Ficha's three targets", ErrSignatureInvalid, uri)
		}
		if enveloped && target != root && id != comprobanteID {
			return nil, fmt.Errorf("%w: enveloped transform on %q", ErrSignatureInvalid, uri)
		}
		c14n, err := canonicalize(target)
		if err != nil {
			return nil, fmt.Errorf("%w: canonicalize %q: %v", ErrSignatureInvalid, uri, err)
		}
		if got, want := digestBytes(c14n, spec), strings.TrimSpace(digestValue.Text()); got != want {
			return nil, fmt.Errorf("%w: digest mismatch on reference %q", ErrSignatureInvalid, uri)
		}
	}
	if !sawDoc || !sawProps || !sawKey {
		return nil, fmt.Errorf("%w: references must cover the document, SignedProperties and KeyInfo", ErrSignatureInvalid)
	}

	certEl := findNS(keyInfo, dsNamespace, "X509Certificate")
	if certEl == nil {
		return nil, fmt.Errorf("%w: KeyInfo carries no X509Certificate", ErrSignatureInvalid)
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(certEl.Text()), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: X509Certificate: %v", ErrSignatureInvalid, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("%w: X509Certificate: %v", ErrSignatureInvalid, err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: certificate key is not RSA", ErrSignatureInvalid)
	}
	sigValueEl := childNS(sig, dsNamespace, "SignatureValue")
	if sigValueEl == nil {
		return nil, fmt.Errorf("%w: no ds:SignatureValue", ErrSignatureInvalid)
	}
	sigValue, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(sigValueEl.Text()), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: SignatureValue: %v", ErrSignatureInvalid, err)
	}
	signedInfoC14N, err := canonicalize(signedInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize SignedInfo: %v", ErrSignatureInvalid, err)
	}
	h := spec.newHash()
	h.Write(signedInfoC14N)
	if err := rsa.VerifyPKCS1v15(pub, spec.hash, h.Sum(nil), sigValue); err != nil {
		return nil, fmt.Errorf("%w: RSA signature does not verify", ErrSignatureInvalid)
	}

	result := &VerifiedSignature{Algorithm: algorithm, Certificate: cert}
	if st := findNS(signedProps, etsiNamespace, "SigningTime"); st != nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(st.Text())); err == nil {
			result.SigningTime = t
		}
	}
	return result, nil
}

func childNS(parent *etree.Element, ns, tag string) *etree.Element {
	for _, c := range parent.ChildElements() {
		if c.Tag == tag && c.NamespaceURI() == ns {
			return c
		}
	}
	return nil
}

func childrenNS(parent *etree.Element, ns, tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range parent.ChildElements() {
		if c.Tag == tag && c.NamespaceURI() == ns {
			out = append(out, c)
		}
	}
	return out
}

func findNS(parent *etree.Element, ns, tag string) *etree.Element {
	for _, c := range parent.ChildElements() {
		if c.Tag == tag && c.NamespaceURI() == ns {
			return c
		}
		if found := findNS(c, ns, tag); found != nil {
			return found
		}
	}
	return nil
}
