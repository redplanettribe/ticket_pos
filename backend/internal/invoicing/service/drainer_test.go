package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// The ladder (#474): read at the moments a healthy drain arrives — right
// after signing, a minute later, six minutes in, twenty-one minutes in — it
// says 1 min, 5 min, 15 min, then an hour forever.
func TestLadderDelayClimbsThenStaysHourly(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, time.Minute},
		{30 * time.Second, time.Minute},
		{time.Minute, 5 * time.Minute},
		{5 * time.Minute, 5 * time.Minute},
		{6 * time.Minute, 15 * time.Minute},
		{20 * time.Minute, 15 * time.Minute},
		{21 * time.Minute, time.Hour},
		{3 * time.Hour, time.Hour},
		{48 * time.Hour, time.Hour},
	}
	for _, c := range cases {
		if got := ladderDelay(c.elapsed); got != c.want {
			t.Fatalf("ladderDelay(%v) = %v; want %v", c.elapsed, got, c.want)
		}
	}
}

// TestTheSaleInvoiceDrainBudgetIsStrictlyInsideTheSchedulerDeadline is the
// Reversal Reconciler's deadline-chain assertion made for this drain: the
// budget must expire before Cloud Scheduler's attempt deadline, with room
// for a document whose submission and polls were started just inside the
// budget, and the deadline before Cloud Run's request timeout. It reads
// Terraform's defaults rather than mirroring them.
func TestTheSaleInvoiceDrainBudgetIsStrictlyInsideTheSchedulerDeadline(t *testing.T) {
	moduleVariables := filepath.Join("..", "..", "..", "..", "terraform", "modules", "ticket-pos", "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "sale_invoice_drainer_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if saleInvoiceDrainBudget >= deadline {
		t.Fatalf("saleInvoiceDrainBudget = %v, Cloud Scheduler's attempt deadline = %v: the budget must expire FIRST", saleInvoiceDrainBudget, deadline)
	}
	if slack := deadline - saleInvoiceDrainBudget; slack < DefaultPollBudget {
		t.Fatalf("the attempt deadline leaves %v after the drain budget, want at least the poll budget %v — a document started just inside the budget can still be paying it", slack, DefaultPollBudget)
	}
	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first", deadline, requestTimeout)
	}
}

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
