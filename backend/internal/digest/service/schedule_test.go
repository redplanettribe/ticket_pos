package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// The Follow Digest pipeline's deployment, asserted against the code that has to
// live inside it (#226, ADR 0030).
//
// Nothing here runs a scheduler. What it does is compare numbers that live in
// two languages in two directories — Go constants beside digestDrainBudget, and
// Terraform defaults beside the two Cloud Scheduler jobs — and that a reviewer
// reading either one alone cannot see the bug in. It is the same assertion, for
// the same reason, that ADR 0024's reconciler already carries
// (sales/service.TestTheDrainBudgetIsStrictlyInsideTheSchedulerDeadline).

// terraformModuleDir is where the deployment this code runs inside is written
// down. Nothing else in this package reaches out of the backend directory, and
// that is the point: these assertions have nowhere else they could live.
func terraformModuleDir() string {
	return filepath.Join("..", "..", "..", "..", "terraform", "modules", "ticket-pos")
}

// resendSendAllowance is the provider timeout the last send of a run may still
// be paying when the drain's budget expires — platform.ResendEmailSender's HTTP
// client timeout. It is stated here rather than imported because it belongs to
// the platform's Resend client and this is the only place the drain's own
// arithmetic needs it; if that timeout ever grows, this test is what says so.
const resendSendAllowance = 10 * time.Second

// TestTheDigestDrainBudgetIsStrictlyInsideTheSchedulerDeadline is the whole
// deadline chain for the drain — budget, Cloud Scheduler's attempt deadline,
// Cloud Run's request timeout — checked against the deployment that has to
// honour it.
//
// All three terms are here because the innermost one being smallest is not
// enough: an attempt deadline raised past Cloud Run's timeout would be a
// Scheduler patiently waiting on a request Cloud Run has already killed.
//
// The innermost term is the only one that stops a run POLITELY. The other two
// abandon the request where it stands, and here that means losing the write that
// records a message the provider has already accepted — which costs a Customer a
// duplicate Digest on the retry.
func TestTheDigestDrainBudgetIsStrictlyInsideTheSchedulerDeadline(t *testing.T) {
	variables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, variables, "follow_digest_drain_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, variables, "api_request_timeout_seconds")

	if digestDrainBudget >= deadline {
		t.Fatalf("digestDrainBudget = %v, Cloud Scheduler's attempt deadline = %v: the budget must expire FIRST, or a run with real backlog is abandoned mid-send and the write recording an accepted message is lost",
			digestDrainBudget, deadline)
	}

	// And with room for a send already in flight when the budget expires: the
	// loop stops STARTING sends at the budget, so the last one can still cost the
	// full provider timeout on top of it.
	if slack := deadline - digestDrainBudget; slack < resendSendAllowance {
		t.Fatalf("the attempt deadline leaves %v after the drain budget, want at least %v — a send started just inside the budget can cost the full provider timeout",
			slack, resendSendAllowance)
	}

	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first, or Scheduler is waiting on a request Cloud Run has already killed",
			deadline, requestTimeout)
	}
}

// TestTheDigestEnqueueDeadlineIsInsideTheRequestTimeout holds the same outer
// relationship for the weekly job.
//
// The enqueue has no budget of its own to be the innermost term — it is one
// statement, and there is no loop to stop politely — so its chain has two terms
// rather than three. A deadline beyond Cloud Run's timeout would still be the
// same lie: Scheduler waiting on a request that no longer exists, and an
// operator reading "the enqueue timed out" with no way to tell which limit ended
// it.
func TestTheDigestEnqueueDeadlineIsInsideTheRequestTimeout(t *testing.T) {
	variables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, variables, "follow_digest_enqueue_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, variables, "api_request_timeout_seconds")

	if deadline >= requestTimeout {
		t.Fatalf("the enqueue attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first",
			deadline, requestTimeout)
	}
}

// TestTheWeeklyEnqueueIsScheduledForThursdayMorningInEcuador pins the one
// scheduling decision this feature actually makes.
//
// THURSDAY 09:00 America/Guayaquil, and both halves matter. Thursday because a
// Digest lands before the weekend while the tickets it advertises are still
// buyable; Ecuador because a cron expression without its zone is a time of day
// nobody can read, and the zone is the one the existing reconciler already
// schedules in (ADR 0024).
//
// It reads the Terraform rather than restating the numbers, because a constant
// here would be a second copy of a decision that has to stay one.
func TestTheWeeklyEnqueueIsScheduledForThursdayMorningInEcuador(t *testing.T) {
	schedule := terraformStringDefault(t,
		filepath.Join(terraformModuleDir(), "variables.tf"),
		"follow_digest_enqueue_schedule")
	if schedule != "0 9 * * 4" {
		t.Fatalf("follow_digest_enqueue_schedule = %q, want %q — Thursday 09:00, so the Digest lands before the weekend while the tickets are still buyable",
			schedule, "0 9 * * 4")
	}

	source := readFile(t, filepath.Join(terraformModuleDir(), "follow_digest.tf"))
	job := blockOf(t, source, `resource "google_cloud_scheduler_job" "follow_digest_enqueue"`)
	if !regexp.MustCompile(`time_zone\s*=\s*"America/Guayaquil"`).MatchString(job) {
		t.Fatalf("the weekly enqueue job does not schedule in America/Guayaquil; a cron expression without its zone is a time of day nobody can read")
	}
}

// TestBothDigestJobsArePausableWithoutADeploy is the operator's switch, asserted
// where it can actually be lost.
//
// PAUSED, NOT ABSENT: the job, its identity and its run.invoker grant stay in
// place while nothing fires. That is how the rollout wants it applied — the
// endpoints are curled by hand first — and it is the first move in an incident,
// which must not be an apply that recreates three things under pressure.
func TestBothDigestJobsArePausableWithoutADeploy(t *testing.T) {
	source := readFile(t, filepath.Join(terraformModuleDir(), "follow_digest.tf"))
	for _, job := range []struct{ resource, variable string }{
		{"follow_digest_enqueue", "follow_digest_enqueue_enabled"},
		{"follow_digest_drain", "follow_digest_drain_enabled"},
	} {
		block := blockOf(t, source, `resource "google_cloud_scheduler_job" "`+job.resource+`"`)
		if !regexp.MustCompile(`paused\s*=\s*!var\.` + regexp.QuoteMeta(job.variable)).MatchString(block) {
			t.Fatalf("the %s job is not paused by var.%s; pausing has to be a variable, or stopping the send is a deploy",
				job.resource, job.variable)
		}
	}
}

// TestEachDigestJobPresentsItsOwnServiceAccount holds the authentication half of
// ADR 0024's pattern: a dedicated identity per job, presenting an OIDC token,
// and no application middleware anywhere in the gate.
//
// Two identities rather than one shared "digest" account, because the audit log
// has to distinguish "the weekly job enqueued a week" from "the minute tick
// drained one", and because revoking one must never silently revoke the other.
func TestEachDigestJobPresentsItsOwnServiceAccount(t *testing.T) {
	source := readFile(t, filepath.Join(terraformModuleDir(), "follow_digest.tf"))
	accounts := map[string]bool{}
	for _, job := range []string{"follow_digest_enqueue", "follow_digest_drain"} {
		block := blockOf(t, source, `resource "google_cloud_scheduler_job" "`+job+`"`)
		match := regexp.MustCompile(`oidc_token\s*\{[^}]*service_account_email\s*=\s*google_service_account\.(\w+)\.email`).FindStringSubmatch(block)
		if match == nil {
			t.Fatalf("the %s job has no oidc_token naming a service account of its own; Cloud Run IAM is the whole gate on these endpoints", job)
		}
		if accounts[match[1]] {
			t.Fatalf("both Digest jobs present google_service_account.%s; each job must carry its own identity so one can be revoked without the other", match[1])
		}
		accounts[match[1]] = true
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(source)
}

// blockOf returns one top-level Terraform block, from its header to the first
// line that closes it in column zero.
func blockOf(t *testing.T, source, header string) string {
	t.Helper()
	block := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(header) + ` \{.*?\n\}`).FindString(source)
	if block == "" {
		t.Fatalf("no %s in the Terraform", header)
	}
	return block
}

// terraformSecondsDefault reads one numeric variable's default out of a
// Terraform variables file, as a duration in seconds.
func terraformSecondsDefault(t *testing.T, path, name string) time.Duration {
	t.Helper()
	block := variableBlock(t, path, name)
	match := regexp.MustCompile(`(?m)^\s*default\s*=\s*(\d+)\s*$`).FindStringSubmatch(block)
	if match == nil {
		t.Fatalf("variable %q in %s has no numeric default", name, path)
	}
	seconds, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("default of %q is not a number: %v", name, err)
	}
	return time.Duration(seconds) * time.Second
}

// terraformStringDefault reads one string variable's default out of a Terraform
// variables file.
func terraformStringDefault(t *testing.T, path, name string) string {
	t.Helper()
	block := variableBlock(t, path, name)
	match := regexp.MustCompile(`(?m)^\s*default\s*=\s*"([^"]*)"\s*$`).FindStringSubmatch(block)
	if match == nil {
		t.Fatalf("variable %q in %s has no string default", name, path)
	}
	return match[1]
}

func variableBlock(t *testing.T, path, name string) string {
	t.Helper()
	source := readFile(t, path)
	block := regexp.MustCompile(`(?s)variable "` + regexp.QuoteMeta(name) + `" \{.*?\n\}`).FindString(source)
	if block == "" {
		t.Fatalf("no variable %q in %s", name, path)
	}
	return block
}
