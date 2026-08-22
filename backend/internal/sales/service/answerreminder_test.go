package service

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// The Answer Reminder sweep's deployment contract (#317, ADR 0044).
//
// What the sweep DOES — who is mailed, who is not, and how often — is proved
// over HTTP against a real database in integration/answer_reminder_test.go, and
// the rationing itself is proved clause by clause in
// internal/catalog/answer_reminder_test.go. Nothing here repeats either.
//
// What is left is what a behaviour test structurally cannot see: this job's
// correctness lives half in Terraform, and a Go test is the only place the two
// halves are ever read side by side. These read Terraform's own defaults rather
// than mirroring them in constants, on answerpurge_test.go's terms — a constant
// here is one more copy to forget. The helpers they use live in that file.

// TestTheAnswerReminderShipsPaused is the assertion this whole ticket is shipped
// behind.
//
// #317 says the job ships paused, ADR 0045 says the feature ships dark, and the
// difference between a tracked gap and a live one is this default. A sweep that
// fired on deploy would put mail about Ticket Questions in buyers' inboxes
// before the Privacy Policy describes the collection — the one outcome ADR 0045
// exists to prevent — and, unlike a table that filled by mistake, mail cannot be
// recalled.
//
// It asserts the DEFAULT rather than the resource, because `paused` is wired to
// the variable and it is the variable's default that decides what a fresh apply
// does.
func TestTheAnswerReminderShipsPaused(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	block := terraformVariableBlock(t, moduleVariables, "answer_reminder_enabled")

	if !regexp.MustCompile(`(?m)^\s*default\s*=\s*false\s*$`).MatchString(block) {
		t.Fatalf("answer_reminder_enabled does not default to false — the sweep would start mailing buyers the moment it is deployed, before anybody decided it should:\n%s", block)
	}
}

// The production environment must ship it paused too. The module default is what
// a fresh apply of the module does; this is what the deployment that actually
// has customers in it does, and the two are separate files that can drift.
func TestTheAnswerReminderShipsPausedInProduction(t *testing.T) {
	prodVariables := filepath.Join(terraformModuleDir(), "..", "..", "envs", "prod", "variables.tf")
	block := terraformVariableBlock(t, prodVariables, "answer_reminder_enabled")

	if !regexp.MustCompile(`(?m)^\s*default\s*=\s*false\s*$`).MatchString(block) {
		t.Fatalf("the production answer_reminder_enabled does not default to false — the one job whose output is other people's mail would fire on the next apply:\n%s", block)
	}
}

// TestTheAnswerReminderIsPausableWithoutADeploy pins the incident lever.
//
// `paused = !var.answer_reminder_enabled` is what makes stopping this job a
// `terraform apply -var answer_reminder_enabled=false` rather than a console
// click the next apply silently undoes. It matters here for a reason the other
// jobs do not have: the reversal drain and the purge can be reasoned about after
// the fact from the rows they left behind, and a wrong mail is already read.
//
// A schedule wired straight to a hardcoded `paused = false`, or a job that
// existed only when enabled, would both pass a behaviour test and fail an
// operator at 3am.
func TestTheAnswerReminderIsPausableWithoutADeploy(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_reminder.tf"))

	if !regexp.MustCompile(`(?m)^\s*paused\s*=\s*!var\.answer_reminder_enabled\s*$`).MatchString(source) {
		t.Fatal("the sweep's `paused` is not wired to !var.answer_reminder_enabled: pausing it has to be an apply from the env directory, or the state and reality disagree the moment somebody pauses it in the console")
	}

	// The job, its identity and its grant must all EXIST while paused, so that
	// "turn it back on" is one variable rather than an apply that recreates three
	// resources under pressure. A `count` on any of them would undo that.
	if regexp.MustCompile(`(?m)^\s*count\s*=`).MatchString(source) {
		t.Fatal("the sweep's resources are conditional on `count`: disabling must PAUSE the job, never delete it and its service account, or re-enabling is a three-resource apply during an incident")
	}
}

// TestTheSweepEndpointCannotBeAimed is a contract about the URL, and it is the
// one that would be cheapest to break by accident.
//
// The moment the rationing is measured against comes from the backend's clock.
// If a convenience ever moved it into the URL — a `since` for a backfill, an
// event id "to remind one Event's buyers", an organization id "for a launch" —
// then possession of the scheduler's OIDC token would become possession of a
// button that mails the platform's entire outstanding backlog on demand, seven-
// day silence and all. The URI carries no query and no path parameter, and this
// is what says so.
func TestTheSweepEndpointCannotBeAimed(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_reminder.tf"))

	uri := regexp.MustCompile(`(?m)^\s*uri\s*=\s*"([^"]*)"`).FindStringSubmatch(source)
	if uri == nil {
		t.Fatal("the sweep job has no http_target uri")
	}
	const want = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/answer-reminders/sweep"
	if uri[1] != want {
		t.Fatalf("sweep uri = %q, want %q — an event, an organization or a moment in this URL turns a rationed reminder into a mail-everybody button", uri[1], want)
	}
}

// TestTheSweepDeadlineChainHolds is the three-term chain, read in the only place
// the three numbers are ever compared.
//
//	answerReminderBudget  <  attempt_deadline  <  api_request_timeout_seconds
//
// Whichever term is smallest stops the run, and only the innermost one stops it
// politely. If Scheduler's deadline expired first, a run would be abandoned
// mid-send and lose the ledger write for a mail the provider had already
// accepted — which is how a buyer receives a third reminder the cap says they
// cannot have.
func TestTheSweepDeadlineChainHolds(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "answer_reminder_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if answerReminderBudget >= deadline {
		t.Fatalf("the run budget = %v, Cloud Scheduler's attempt deadline = %v: the budget must expire first, or a run is cut off holding a send it has not recorded",
			answerReminderBudget, deadline)
	}
	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first, or Scheduler is waiting on a request Cloud Run has already killed",
			deadline, requestTimeout)
	}
}

// TestTheSweepIsScheduledInsideTheWorkingDay is a product assertion wearing
// infrastructure clothes.
//
// This is the only scheduled job in the deployment whose firing hour is a
// recipient's experience: the purge deletes at 03:20 because nobody is watching,
// and doing the same here would chase somebody about a t-shirt size in the
// middle of the night. The timezone is Ecuador's for the same reason — the
// schedule is read in the hour buyers live in, not the hour the servers do.
func TestTheSweepIsScheduledInsideTheWorkingDay(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_reminder.tf"))
	if !regexp.MustCompile(`(?m)^\s*time_zone\s*=\s*"America/Guayaquil"\s*$`).MatchString(source) {
		t.Fatal("the sweep is not scheduled in America/Guayaquil: the hour this job fires is the hour a buyer's mail arrives")
	}

	block := terraformVariableBlock(t, filepath.Join(terraformModuleDir(), "variables.tf"), "answer_reminder_schedule")
	cron := regexp.MustCompile(`(?m)^\s*default\s*=\s*"(\S+) (\S+) \S+ \S+ \S+"\s*$`).FindStringSubmatch(block)
	if cron == nil {
		t.Fatalf("answer_reminder_schedule has no five-field cron default:\n%s", block)
	}
	hour := cron[2]
	for _, night := range []string{"0", "1", "2", "3", "4", "5", "22", "23"} {
		if hour == night {
			t.Fatalf("the sweep fires at hour %s in Ecuador: this job's output is somebody's inbox, and a reminder about a t-shirt size in the night is worse than one that waits until morning", hour)
		}
	}
}

// TestTheSweepRetriesNothing pins the one retry policy that could duplicate
// mail.
//
// The ledger row is written after the send and before the response, so a retried
// run mails nobody it already reached — unless the reason the run failed was the
// ledger write itself, in which case a retry mails everybody in the batch again.
// The next tick is a day away and works from the same state, so there is nothing
// a retry recovers that waiting does not.
func TestTheSweepRetriesNothing(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "answer_reminder.tf"))
	if !regexp.MustCompile(`(?m)^\s*retry_count\s*=\s*0\s*$`).MatchString(source) {
		t.Fatal("the sweep's retry_count is not 0: a retried run whose ledger writes were failing mails the whole batch a second time")
	}
}

// TestTheSweepBudgetIsARealBound is a guard on the one constant a well-meaning
// change could quietly make meaningless.
//
// A zero or negative budget would make the loop stop before its first send —
// silence, which at least fails safe — and a budget of hours would make the
// Scheduler deadline the thing that stops a run, which is the failure mode the
// chain above exists to prevent.
func TestTheSweepBudgetIsARealBound(t *testing.T) {
	if answerReminderBudget <= 0 || answerReminderBudget > 5*time.Minute {
		t.Fatalf("answerReminderBudget = %v: it has to be positive and comfortably inside a Cloud Scheduler attempt deadline to be the term that stops a run", answerReminderBudget)
	}
	if answerReminderBatch <= 0 {
		t.Fatalf("answerReminderBatch = %d: a run that claims nothing sweeps nothing", answerReminderBatch)
	}
}
