-- The down migration of 146_event_recaps.sql (spec #884, #891).
--
-- APPLIED BY HAND, NEVER BY THE RUNNER: the runner applies the *.sql files
-- directly under backend/migrations and only forwards, so this directory is
-- outside its reach by construction.
--
-- Dropping the table forgets which Events were recapped. If the sweep still
-- runs afterwards against a schema that has the table again, every published
-- Event over since platform.EventRecapWentLiveAt is recapped a second time, so
-- pause the Cloud Scheduler job before running this.
DROP TABLE IF EXISTS event_recaps;
DELETE FROM schema_migrations WHERE version = '146_event_recaps';
