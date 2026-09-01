package repository

import (
	"errors"
	"time"
)

// TermsVersion is one published edition of the Términos y Condiciones, as the
// `terms_versions` row records it.
//
// Like PolicyVersion it carries no text of its own. The text is no longer
// embedded in the binary (#558): it lives in this edition's artifact rows
// (terms_version_artifacts, migration 109), read WITH this row and never
// separately — see CurrentTermsEdition. This row ties the label, the effective
// date and the fingerprint together.
type TermsVersion struct {
	ID            string
	Label         string
	EffectiveDate time.Time
	ContentHash   string
}

// ErrNoCurrentTermsVersion reports that no Terms edition has taken effect. The
// service maps it to a domain error; migration 105 seeds edition 1, so like its
// policy counterpart it should be unreachable.
var ErrNoCurrentTermsVersion = errors.New("no current terms version")
