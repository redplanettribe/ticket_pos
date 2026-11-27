package service

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// One copy per person (#885): the Event Recap's readers are compared by
// normalized address, so the same person written with other case or stray
// spaces is one reader, kept as first given, and an empty address is nobody.
func TestDistinctEmailsKeepsOneAddressPerPersonComparedNormalized(t *testing.T) {
	got := distinctEmails([]string{
		"ana@example.com",
		"Ana@Example.com",
		" ana@example.com ",
		"",
		"ben@example.com",
		"BEN@example.com",
	})
	if want := "ana@example.com|ben@example.com"; strings.Join(got, "|") != want {
		t.Fatalf("distinctEmails = %q, want %q", got, want)
	}
}

// --- The Terraform half (#889) ---------------------------------------------------

// The sweep is driven by its own job at the pinned route: no body, no
// parameters and no moment, since a run is the run of the latest 09:00 the
// platform clock has reached, whenever it is invoked.
func TestTheEventRecapJobCallsTheSweepRoute(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "event_recap.tf"))
	uri := regexp.MustCompile(`(?m)^\s*uri\s*=\s*"([^"]*)"`).FindStringSubmatch(source)
	if uri == nil {
		t.Fatal("the Event Recap job has no http_target uri")
	}
	const want = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/event-recaps/sweep"
	if uri[1] != want {
		t.Fatalf("sweep uri = %q, want %q", uri[1], want)
	}
	if !regexp.MustCompile(`(?m)^\s*http_method\s*=\s*"POST"\s*$`).MatchString(source) {
		t.Fatal("the Event Recap job does not POST the sweep route")
	}
	if !regexp.MustCompile(`(?m)^\s*name\s*=\s*"\$\{var\.environment\}-ticket-pos-event-recap"\s*$`).MatchString(source) {
		t.Fatal("the Event Recap job is not named for the job the sweep checks in as, ticket-pos-event-recap")
	}
	if !regexp.MustCompile(`(?m)^\s*retry_count\s*=\s*0\s*$`).MatchString(source) {
		t.Fatal("the sweep's retry_count is not 0: the next morning's run makes a missed one good, and a retried run whose record writes were failing sends the same recaps again")
	}
}

// The job fires at the hour the sweep decides for: catalog.EventRecapRunAt is
// the latest 09:00 of the platform clock, so a tick at any other hour, or in
// any other zone, would send a morning's recaps up to a day late.
func TestTheEventRecapJobFiresAtTheHourTheSweepDecidesFor(t *testing.T) {
	if catalog.EventRecapHour != 9 {
		t.Fatalf("catalog.EventRecapHour = %d: move event_recap_schedule with it", catalog.EventRecapHour)
	}
	for _, path := range []string{
		filepath.Join(terraformModuleDir(), "variables.tf"),
		filepath.Join(terraformModuleDir(), "..", "..", "envs", "prod", "variables.tf"),
	} {
		block := terraformVariableBlock(t, path, "event_recap_schedule")
		if !regexp.MustCompile(`(?m)^\s*default\s*=\s*"0 9 \* \* \*"\s*$`).MatchString(block) {
			t.Fatalf("%s: event_recap_schedule does not default to daily at 09:00:\n%s", path, block)
		}
	}
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "event_recap.tf"))
	if !regexp.MustCompile(`(?m)^\s*schedule\s*=\s*var\.event_recap_schedule\s*$`).MatchString(source) {
		t.Fatal("the Event Recap job's schedule is not var.event_recap_schedule")
	}
	if !regexp.MustCompile(`(?m)^\s*time_zone\s*=\s*"America/Guayaquil"\s*$`).MatchString(source) {
		t.Fatal("the Event Recap job is not scheduled on the platform clock, America/Guayaquil")
	}
}

// Pausing is an apply, never a console click the next apply undoes, and the
// job, its identity and its grant exist while paused.
func TestTheEventRecapJobIsPausableWithoutADeploy(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "event_recap.tf"))
	if !regexp.MustCompile(`(?m)^\s*paused\s*=\s*!var\.event_recap_enabled\s*$`).MatchString(source) {
		t.Fatal("the job's `paused` is not wired to !var.event_recap_enabled")
	}
	if regexp.MustCompile(`(?m)^\s*count\s*=`).MatchString(googleResources(t, source)) {
		t.Fatal("the job's resources are conditional on `count`: disabling must pause the job, never delete it")
	}
}

// PRODUCTION RUNS IT. The Event Recap has no dark-ship flag and no catch-up to
// watch: platform.EventRecapWentLiveAt keeps the first run to the Events over
// since go-live. So the apply the deploy needs is the whole launch, and
// production's tfvars must switch the job on.
func TestTheEventRecapJobRunsInProduction(t *testing.T) {
	tfvars := terraformFile(t, filepath.Join(terraformModuleDir(), "..", "..", "envs", "prod", "terraform.tfvars"))
	if !regexp.MustCompile(`(?m)^\s*event_recap_enabled\s*=\s*true\s*$`).MatchString(tfvars) {
		t.Fatal("production does not enable the Event Recap job: nobody would ever be sent an Event Recap")
	}
}

// The deadline chain the Reminder sweeps keep:
//
//	eventRecapBudget  <  attempt_deadline  <  api_request_timeout_seconds
//
// The budget is checked once per Event, so a run overruns it by one Event's
// readers at the pacer's gap; the deadline's slack is what that overrun spends.
func TestTheEventRecapDeadlineChainHolds(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "event_recap_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if eventRecapBudget >= deadline {
		t.Fatalf("the run budget %v is not inside Cloud Scheduler's attempt deadline %v", eventRecapBudget, deadline)
	}
	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline %v is not inside Cloud Run's request timeout %v", deadline, requestTimeout)
	}
}
