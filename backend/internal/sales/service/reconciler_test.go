package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// TestTheDrainBudgetIsStrictlyInsideTheSchedulerDeadline is the whole deadline
// chain — budget, Cloud Scheduler's attempt deadline, Cloud Run's request
// timeout — checked against the deployment that has to honour it. What each term
// is for and what breaks when one moves alone is written down once, beside
// reversalDrainBudget; this is the assertion that makes moving one alone fail.
//
// All three terms are here because the innermost one being smallest is not
// enough: an attempt deadline raised past Cloud Run's timeout would be a
// Scheduler patiently waiting on a request Cloud Run has already killed. The
// three numbers live in two languages in two directories and a reviewer reading
// any one of them cannot see the bug.
//
// It reads Terraform's defaults rather than mirroring them in constants, because
// a constant here is one more copy to forget. Nothing else in this suite reaches
// out of the backend directory, and that is the point: this assertion has
// nowhere else it could live.
func TestTheDrainBudgetIsStrictlyInsideTheSchedulerDeadline(t *testing.T) {
	moduleVariables := filepath.Join("..", "..", "..", "..", "terraform", "modules", "ticket-pos", "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "reversal_reconciler_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if reversalDrainBudget >= deadline {
		t.Fatalf("reversalDrainBudget = %v, Cloud Scheduler's attempt deadline = %v: the budget must expire FIRST, or a run with real backlog is abandoned mid-probe and the outcome of a provider call is lost",
			reversalDrainBudget, deadline)
	}

	// And with room for a probe already in flight when the budget expires: the
	// loop stops STARTING probes at the budget, so the last one can still cost
	// the full provider timeout on top of it.
	if slack := deadline - reversalDrainBudget; slack < payPhoneProbeAllowance {
		t.Fatalf("the attempt deadline leaves %v after the drain budget, want at least %v — a probe started just inside the budget can cost the full provider timeout",
			slack, payPhoneProbeAllowance)
	}

	// The outer term. Cloud Run kills the request whatever Cloud Scheduler is
	// still willing to wait for, so a deadline beyond it buys nothing and hides
	// which limit actually ended the run.
	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first, or Scheduler is waiting on a request Cloud Run has already killed",
			deadline, requestTimeout)
	}
}

// payPhoneProbeAllowance is the provider timeout the last probe of a run may
// still be paying when the budget expires. It is stated here rather than
// imported because it belongs to the platform's PayPhone client and this is the
// only place the drain's own arithmetic needs it; if that timeout ever grows,
// this test is the thing that says so.
const payPhoneProbeAllowance = 10 * time.Second

// terraformSecondsDefault reads one numeric variable's default out of a
// Terraform variables file, as a duration in seconds.
func terraformSecondsDefault(t *testing.T, path, name string) time.Duration {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block := regexp.MustCompile(`(?s)variable "` + regexp.QuoteMeta(name) + `" \{.*?\n\}`).Find(source)
	if block == nil {
		t.Fatalf("no variable %q in %s", name, path)
	}
	match := regexp.MustCompile(`(?m)^\s*default\s*=\s*(\d+)\s*$`).FindSubmatch(block)
	if match == nil {
		t.Fatalf("variable %q in %s has no numeric default", name, path)
	}
	seconds, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("default of %q is not a number: %v", name, err)
	}
	return time.Duration(seconds) * time.Second
}
