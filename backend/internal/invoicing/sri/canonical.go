package sri

import (
	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// canonicalize renders an element subtree in inclusive Canonical XML 1.0
// (REC-xml-c14n-20010315, without comments), the algorithm the SRI's
// validator applies. Namespace declarations in scope from ancestors are
// rendered on the apex, as the spec requires for a document subset.
func canonicalize(el *etree.Element) ([]byte, error) {
	return dsig.MakeC14N10RecCanonicalizer().Canonicalize(el)
}
