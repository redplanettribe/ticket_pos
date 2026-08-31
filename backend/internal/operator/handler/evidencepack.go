package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Consent Evidence Pack's HTTP seam (#568, parent #556, ADR 0067).
//
// TWO ROUTES, ONE PER RECORD SCREEN, and both produce the same file for the
// same human being: a pack spans both populations for one address, so an
// operator who reached the Customer record and one who reached the staff record
// hand over identical bytes. Each is keyed on what its screen is keyed on — an
// opaque Customer id, an opaque Staff Digest — so no address reaches a request
// line here any more than it does on the reads beside them.
//
// THE SYNCHRONOUS `serveDocument` SHAPE: build in memory, set
// Content-Disposition, one Write. The RIDE's shape (ADR 0062), and adequate for
// the measured reason — a pack's size is bounded by how many editions exist
// rather than by how active the person was, because its text is deduplicated by
// edition. The Holder Export's 2k ceiling does not transfer: that cap was
// excelize buffering hundreds of bytes per cell across tens of thousands of
// cells. No async job, no object storage.
//
// A GET, like every other read on this path, and for the same reason: nothing
// in the request line is an address. It is not a POST despite writing a row —
// the row it writes is a hash and a list of ids, recording that a document was
// handed over, and making the download a POST would make it unbookmarkable
// without making it safer.
//
// PLATFORM OPERATOR ONLY, AND THERE IS NO SUBJECT-FACING EQUIVALENT ANYWHERE.
// A passcode buys a WITHDRAWAL — an act that only ever takes something away
// (ADR 0039) — where a pack DISCLOSES everything the platform holds. That
// absence is the platform's, not a screen's: there is no route to this outside
// the operator mux.

// DownloadCustomerEvidencePack generates and serves one Customer's pack.
//
// @Summary      Generate a Customer's Consent Evidence Pack
// @Description  Generates and downloads the Consent Evidence Pack for one Customer: a deterministic ZIP holding `record.json` (the record — acts, editions, fingerprints, evidence, prior values and the preimage rule), `evidence.pdf` (its human reading, in ADR 0062's shape) and `texts/<document>/<edition-label>/<locale>/<ordinal>-<slug>.md`, the raw stored markdown of every edition named, deduplicated BY EDITION and carried in EVERY LOCALE each was published in — because the fingerprint spans an edition's languages at once and a single-locale pack could not verify its own hash (#568, spec #556, ADR 0067). THE PACK IS NEVER STORED: only its SHA-256, its size and the ids of the acts it covered are persisted (migration 118), which is enough to prove a handover because the bytes are reproducible. THE ZIP IS DETERMINISTIC — fixed entry ordering, one fixed entry timestamp, fixed PDF metadata — so two packs generated over identical acts on different days are BYTE-IDENTICAL. That is why an edition states `published_as`, fixed when it was published, and never whether it currently satisfies a gate, which changes at midnight as the database's own day moves. IT SPANS BOTH POPULATIONS FOR ONE ADDRESS: a person who is also on the Staff platform gets ONE file, with their Terms acceptances in it, and the identical file is served by the staff route. IT IS GENERATED EVEN WHEN EMPTY, so "we hold nothing about this person" is a provable answer rather than an error. WITHDRAWALS APPEAR AS THE ACTS THEMSELVES, never as a synthesised log, and `null` keeps meaning "not shown". THE HMAC STAFF DIGEST APPEARS NOWHERE IN THE CONTENTS (#548); the filename is keyed on the pack's own SHA-256 — `consent-evidence-<sha256[:16]>-<YYYY-MM-DD>.zip` — which is not the email, depends on no rotatable key, and is the one value the platform persists about the handover. The document states plainly that `email_proven` is what makes a consent valid, and states its two limitations: "no staff acceptances" means "no rows for this address" and never "not staff", and the email on each act is the one ASSERTED AT CAPTURE rather than a canonical identity. Generated on demand and synchronously. An id nobody holds is 404 LEGAL_SUBJECT_NOT_FOUND. Platform Operator only; there is no self-service download.
// @Tags         operator
// @Produce      application/zip
// @Security     BearerAuth
// @Param        customerID  path  string  true  "Customer id (UUID)"
// @Success      200  {file}  binary
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/customers/{customerID}/evidence-pack [get]
func (h *Handler) DownloadCustomerEvidencePack(w http.ResponseWriter, r *http.Request) {
	h.servePack(w, r, strings.TrimSpace(r.PathValue("customerID")), h.svc.CustomerEvidencePack)
}

// DownloadStaffEvidencePack generates and serves one staff person's pack.
//
// @Summary      Generate a staff person's Consent Evidence Pack
// @Description  Generates and downloads the Consent Evidence Pack for the person a Staff Digest names — the SAME FILE the Customer route serves where that address is also a Customer, because a pack spans both populations for one address and one access request has one answer (#568, spec #556, ADR 0067). The digest is resolved by MATCHING across the staff population and never by reversing, exactly as the staff record is: it is one-way by design. A digest matching nobody is 404 STAFF_SUBJECT_NOT_FOUND; a deployment with no link secret is 503 STAFF_DIGEST_UNAVAILABLE, because with no key the match would resolve an arbitrary digest to the first person in the list, which is the wrong person's evidence rather than a degraded answer. THE DIGEST ITSELF APPEARS NOWHERE IN THE FILE OR ITS NAME (#548): a key rotation must not orphan a document whose purpose is to stay meaningful for years, so the filename is keyed on the pack's own SHA-256. Contents, determinism, the never-stored rule and the stated limitations are exactly as described on the Customer route. Platform Operator only; there is no self-service download.
// @Tags         operator
// @Produce      application/zip
// @Security     BearerAuth
// @Param        digest  path  string  true  "Staff Digest (32 hex characters)"
// @Success      200  {file}  binary
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/staff/{digest}/evidence-pack [get]
func (h *Handler) DownloadStaffEvidencePack(w http.ResponseWriter, r *http.Request) {
	h.servePack(w, r, r.PathValue("digest"), h.svc.StaffEvidencePack)
}

// servePack is the RIDE's serveDocument, over a ZIP.
//
// A REFUSAL IS THE ORDINARY JSON ENVELOPE and not a broken download: the caller
// gets a reason it can show, which is the same choice the Holder Export's proxy
// makes and for the same reason.
func (h *Handler) servePack(
	w http.ResponseWriter,
	r *http.Request,
	key string,
	generate func(context.Context, string) (*service.EvidencePack, error),
) {
	reqID := platform.RequestID(r.Context())

	pack, err := generate(r.Context(), key)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	w.Header().Set("Content-Type", pack.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+pack.Filename+"\"")
	// The pack's own fingerprint, on the response that carries it. It is the
	// value stored (migration 118) and the value #569's access log records, so
	// an operator can check the file they received without opening it.
	w.Header().Set("X-Pack-SHA256", pack.SHA256)
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pack.Body)
}
