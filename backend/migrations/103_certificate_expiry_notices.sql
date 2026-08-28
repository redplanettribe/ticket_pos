-- The ledger of Certificate Expiry Warnings: which rung of the 30/7/1/0 ladder
-- has been fired for which signing certificate (#503, parent #490, ADR 0063).
--
-- THIS TABLE EXISTS ONLY TO SAY NO, in the family of `answer_reminders`
-- (migrations 079 and 083) and `ticket_assignment_mails` (082), and for the
-- same reason those two exist at all: the Sale Invoice Drainer's tick runs in a
-- Cloud Run instance that does not outlive the request, every five minutes,
-- and "have we already told the operators it is seven days out" is a question
-- only the database can answer. Without a row here, each tick would mail the
-- whole allowlist again, and a warning sent 288 times a day is not a warning.
--
-- IT IS NOT THE OTHER TWO LEDGERS AND MUST NOT BE CONFUSED WITH THEM. Those
-- ration MAIL: they hold one row per message (or per Ticket a message covered)
-- and are read for "how many" and "when was the last". This one holds one row
-- per THRESHOLD FIRED PER CERTIFICATE — at most four rows for the life of one
-- .p12 — and is read for one thing: which rungs the ladder has already passed
-- for the certificate in custody. It is never read for a count of mail. The
-- `recipient_count` column is a trail of how many addresses accepted the fan-out
-- that fired the rung, kept so a "did anyone get it" question is answerable by
-- hand; nothing rations on it, nothing renders it, and it is never a count of
-- mail either — a rung fired to three operators is one row.
--
-- ONE MAIL CAN WRITE SEVERAL ROWS, as 083's does, and for the mirror reason.
-- A certificate uploaded with five days left has reached the 30 rung and the 7
-- rung at once; it is mailed ONCE, for 30 (ADR 0063 §2, highest-unfired-only),
-- and the 7 rung is covered by that mail — so both rows are written, or the
-- next tick would send "seven days" to the same people about the same date.
-- Rows here say a rung is DONE for a certificate, never that a mail went out
-- for that rung in particular.
--
-- KEYED ON THE CERTIFICATE, NOT ON THE ISSUER. The fingerprint is the SHA-256
-- of the DER certificate the Issuer row also carries
-- (`invoicing_issuers.certificate_fingerprint_sha256`). A re-uploaded
-- certificate with a later NotAfter is a different fingerprint with no rows,
-- so the ladder starts over against the new date by construction; the same
-- file uploaded again is the same fingerprint, and re-fires nothing (ADR 0063
-- §3). There is deliberately NO foreign key to the Issuer: the rows for a
-- certificate no longer in custody are history, not garbage, and cleaning them
-- up is out of scope.
--
-- THE ROW IS WRITTEN AFTER THE FAN-OUT, once at least one address on the
-- operator allowlist accepted the mail, on 079's rule and for a sharper reason:
-- there is no second chance at "seven days". A row written first would spend
-- the rung on a provider outage; a row never written because the process died
-- between the send and the insert costs at most one repeat mail on the next
-- tick, which is the direction worth failing in. At-least-once, never at-most.
--
-- WHAT IT DELIBERATELY DOES NOT RECORD: no address, no subject, no body, no
-- provider message id. Who was told is the allowlist as it stood, and the
-- allowlist is identity's to read (ADR 0015).
--
-- THIS SCHEMA IS ALIVE ABOVE THE FLAG. Unlike 098–102, the writer of this
-- table is not behind SALE_INVOICING_ENABLED: the Drainer's tick works the
-- ladder before it checks the flag, so a closed-flag deployment with a
-- certificate uploaded ahead of launch still has the certificate watched (ADR
-- 0063 §1). What gates the writer is the Cloud Scheduler job driving the tick,
-- which ships paused; an empty table under a paused job is the correct state.
CREATE TABLE certificate_expiry_notices (
    -- The certificate the rung was fired for: lowercase hex SHA-256 of its
    -- DER bytes, as invoicing.CertificateMetadata carries it.
    fingerprint_sha256 TEXT NOT NULL,

    -- The rung: 30, 7, 1 or 0 days before NotAfter, in Ecuadorian calendar
    -- days (ADR 0063 §2). The primary key is what makes "once per threshold
    -- per certificate" a property of the schema rather than of the code.
    threshold_days SMALLINT NOT NULL,

    -- When the fan-out that fired this rung finished, on the service clock.
    sent_at TIMESTAMPTZ NOT NULL,

    -- How many allowlist addresses accepted the mail. A trail, never a
    -- rationing input; see above.
    recipient_count INTEGER NOT NULL,

    PRIMARY KEY (fingerprint_sha256, threshold_days)
);
