package service

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Abandoned Answer Purge's deployment contract (#316, ADR 0044).
//
// What the purge DOES is proved over HTTP against a real database, in
// integration/checkout_answers_purge_test.go — including the one assertion this
// whole ticket exists for, that an 'expired' Payment inside the window keeps its
// Answers and can still flip to approved with them intact. Nothing here repeats
// that.
//
// What is left is everything the behaviour tests structurally cannot see: this
// job's correctness lives half in Terraform, and a Go test is the only place the
// two halves are ever read side by side. These read Terraform's own defaults
// rather than mirroring them in constants, on reconciler_test.go's terms — a
// constant here is one more copy to forget.

// terraformModuleDir is where the deployment's module lives, relative to this
// package.
func terraformModuleDir() string {
	return filepath.Join("..", "..", "..", "..", "terraform", "modules", "ticket-pos")
}

// TestTheAnswerPurgeShipsPaused is the assertion that keeps a deletion job from
// starting to fire the moment it is deployed.
//
// Every scheduled job in this deployment ships paused, and for this one the
// house rule is not the only reason. The table it deletes from is filled by a
// checkout section behind TICKET_QUESTIONS_ENABLED, which is off until a Policy
// Version describing the collection publishes (ADR 0045) — so a purge that fired
// on deploy would be a nightly DELETE against an empty table, running unwatched
// for however many months separate this ticket from that policy. The first run
// should happen when somebody is looking.
//
// It asserts the DEFAULT rather than the resource, because `paused` is wired to
// the variable and it is the variable's default that decides what a fresh apply
// does.
func TestTheAnswerPurgeShipsPaused(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	block := terraformVariableBlock(t, moduleVariables, "answer_purge_enabled")

	if !regexp.MustCompile(`(?m)^\s*default\s*=\s*false\s*$`).MatchString(block) {
		t.Fatalf("answer_purge_enabled does not default to false — the only scheduled job in this deployment that DELETES anything would start firing on deploy, against a table nobody is watching yet:\n%s", block)
	}
}

// TestTheAnswerPurgeIsPausableWithoutADeploy pins the incident lever.
//
// `paused = !var.answer_purge_enabled` is what makes stopping this job a
// `terraform apply -var answer_purge_enabled=false` rather than a console click
// the next apply silently undoes. It matters more here than for the jobs beside
// it: the reversal drain and the Digest jobs can be reasoned about after the
// fact from the rows they left behind, and this one's runs are only legible as
// absences.
//
// A schedule wired straight to a hardcoded `paused = false`, or a job that
// existed only when enabled, would both pass a behaviour test and fail an
// operator at 3am.
func TestTheAnswerPurgeIsPausableWithoutADeploy(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_purge.tf"))

	if !regexp.MustCompile(`(?m)^\s*paused\s*=\s*!var\.answer_purge_enabled\s*$`).MatchString(source) {
		t.Fatal("the purge job's `paused` is not wired to !var.answer_purge_enabled: pausing it has to be an apply from the env directory, or the state and reality disagree the moment somebody pauses it in the console")
	}

	// The job, its identity and its grant must all EXIST while paused, so that
	// "turn it back on" is one variable rather than an apply that recreates three
	// resources under pressure. A `count` on any of them would undo that.
	if regexp.MustCompile(`(?m)^\s*count\s*=`).MatchString(source) {
		t.Fatal("the purge job's resources are conditional on `count`: disabling must PAUSE the job, never delete it and its service account, or re-enabling is a three-resource apply during an incident")
	}
}

// TestThePurgeEndpointCannotBeAimed is a contract about the URL, and it is the
// one that would be cheapest to break by accident.
//
// The window is computed in PurgeAbandonedAnswers from this service's clock. If
// a future convenience ever moved it into the URL — a cutoff for a backfill, an
// organization id "to purge one tenant" — then possession of the scheduler's
// OIDC token would become possession of a button that deletes every Answer on
// the platform, and the retention job would have become an exfiltration-proof
// deletion oracle. The URI in Terraform carries no query and no path parameter,
// and this is what says so.
func TestThePurgeEndpointCannotBeAimed(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_purge.tf"))

	uri := regexp.MustCompile(`(?m)^\s*uri\s*=\s*"([^"]*)"`).FindStringSubmatch(source)
	if uri == nil {
		t.Fatal("the purge job has no http_target uri")
	}
	const want = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/checkout-answers/purge"
	if uri[1] != want {
		t.Fatalf("purge uri = %q, want %q — a cutoff or a tenant in this URL turns a retention job into a delete-everything button", uri[1], want)
	}
}

// TestThePurgeDeadlineIsInsideTheRequestTimeout is the deadline chain, which for
// this job has two terms rather than the reversal drain's three: the run has no
// budget of its own to expire first, because it is a single DELETE.
//
// A Scheduler deadline above Cloud Run's request timeout would mean Scheduler
// waiting on a request the platform has already killed, and recording the run as
// a timeout rather than as whatever actually went wrong.
func TestThePurgeDeadlineIsInsideTheRequestTimeout(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "answer_purge_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first, or Scheduler is waiting on a request Cloud Run has already killed",
			deadline, requestTimeout)
	}
}

// TestTheRetentionWindowIsThirtyDays pins the number the whole feature was
// specified around, in the one place it is stated.
//
// It is not a tautology guarding a constant against typos. The window is what
// makes acting on non-approval SAFE — the argument is written out beside the
// constant — and a future reader shortening it to "clean up faster" would be
// making the purge start deleting the Answers of sales that then commit, with no
// test failing anywhere else in this repository to say so. This one fails.
func TestTheRetentionWindowIsThirtyDays(t *testing.T) {
	if days := sales.AbandonedAnswerRetention.Hours() / 24; days != 30 {
		t.Fatalf("AbandonedAnswerRetention = %v (%v days), want 30 days — the window is not a tidiness figure, it is what makes a non-approved Payment safe to act on at all (ADR 0044)", sales.AbandonedAnswerRetention, days)
	}
}

// terraformFile reads a Terraform source file, failing the test if it has moved.
func terraformFile(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(source)
}

// terraformVariableBlock returns one `variable "name" { ... }` block's source.
func terraformVariableBlock(t *testing.T, path, name string) string {
	t.Helper()
	source := terraformFile(t, path)
	block := regexp.MustCompile(`(?s)variable "` + regexp.QuoteMeta(name) + `" \{.*?\n\}`).FindString(source)
	if block == "" {
		t.Fatalf("no variable %q in %s", name, path)
	}
	return block
}
