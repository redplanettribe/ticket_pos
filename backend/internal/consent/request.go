package consent

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EvidenceFromRequest reads the circumstances of a capture act off the request
// that carried it.
//
// ONE SPELLING, for every surface in every module. Consent capture happens in
// two packages that know nothing of each other — the Customer Area's toggle and
// unsubscribe link in customers, the guest checkout in sales — and each grew its
// own copy of these three lines while the other could not see it. Three lines
// are cheap to repeat and expensive to have repeated: the value of a prueba
// técnica is that a compliance officer can say what the field means, and two
// readings that drift (one trusting a proxy header the other rejects, say) would
// make the same column mean two things depending on which surface wrote it.
//
// The IP comes from platform.ClientIP rather than from any handler's own reading
// of the forwarding chain, which is the same rule the rest of the platform
// follows. The user agent and origin are the browser's own headers as the
// Storefront BFF relayed them (ADR 0008) — the API never sees a browser directly.
//
// The session is NOT here, and cannot be. Which session an act happened under is
// known to the service and not to the request: the sign-in's record names the
// Customer Session it is about to MINT, the Customer Area's names the one it
// authenticated, and the guest checkout has none at all. Each caller fills that
// field itself.
func EvidenceFromRequest(r *http.Request) Evidence {
	return Evidence{
		IP:        platform.ClientIP(r),
		UserAgent: r.UserAgent(),
		OriginURL: r.Referer(),
	}
}
