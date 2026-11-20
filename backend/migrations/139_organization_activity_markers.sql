-- A Member working marks their Organization active for the day (#816, spec
-- #808, ADR 0080).
--
-- The Member half of Active Organization: an Organization counts on a
-- platform day a Member worked in it through a Staff Session, even if
-- nothing sold. Nothing recorded that until now, since a Staff Session row
-- says who is signed in and not on which days they were.
--
-- One row per Organization per platform day, America/Guayaquil, and nothing
-- else in it: no Member, no time of day, no count of requests. It is written
-- by the first request of the day that loads an Active Member of the
-- Organization, and writing one that exists is a no-op. The Operator
-- Dashboard loads no Active Member, so it writes none.
--
--   day              the platform day the Organization was worked in.
--   organization_id  the Organization.
--
-- The daily close reads these rows beside the Ticket Sales recorded that day
-- and freezes the count into platform_activity_totals; a marker that turns
-- up for a day already frozen changes nothing. The same close deletes the
-- rows older than thirteen months, which the frozen totals outlive. The key
-- leads on day because both of those go by day.
--
-- The rows belong to Platform Trends, as the totals do. An Organization that
-- is deleted takes its markers with it.
CREATE TABLE organization_activity_markers (
    day             DATE NOT NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    PRIMARY KEY (day, organization_id)
);

-- THE DAY EXACT RECORDING BEGAN for Active Organizations is the first day
-- the daily close froze with a marker in it: from that day on a frozen day
-- holds both halves, before it the sale half alone. The close writes it
-- down here once, the night it happens, and never moves it. It cannot be
-- inferred later: the markers are deleted after thirteen months and the
-- totals do not say what they were counted from.
--
-- No row until that night. Unlike events_published_recording the day is not
-- this migration's own: a deployment whose close is paused freezes nothing
-- with markers until it runs.
CREATE TABLE organization_activity_recording (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    began_on  DATE NOT NULL
);
