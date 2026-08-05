package repository

import "context"

// The Follow Digest switch (#224, parent #215, ADR 0030): one column, one write,
// and nothing else in the system may touch it.
//
// UNSUBSCRIBING IS A SWITCH, NOT A PURGE, and the shape of this file is the
// whole of that sentence. There is no statement here that reaches
// `customer_organization_follows` or `customer_tag_follows`, and there never
// may be: the unsubscribe link is reachable without signing in, mail security
// scanners prefetch links, and an unsubscribe that unfollowed everything would
// let a corporate scanner silently wipe its own users' lists. What this file can
// do at its very worst is stop a weekly email the Customer can turn back on from
// their own Area.

// SetDigestEnabled turns one Customer's Follow Digest on or off, and reports
// whether there was a Customer to turn it on or off for.
//
// IDEMPOTENT BY CONSTRUCTION. It writes the state asked for rather than
// toggling, so the same request arriving twice — the same unsubscribe link
// pressed from two different Digests, a retried tap, a browser replaying a POST
// — means what it meant the first time. A toggle would have made the second
// press turn the Digest back ON, which is the opposite of what a person pressing
// unsubscribe twice is asking for.
//
// `found` is false for a Customer who was deleted, which the caller reports as
// an invalid link rather than as a missing Customer: whoever is holding the
// token is not entitled to learn which of the two it was.
func (r *Repository) SetDigestEnabled(ctx context.Context, customerID string, enabled bool) (found bool, err error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE customers
		SET digest_enabled = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, customerID, enabled)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
