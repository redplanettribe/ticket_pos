package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
)

// The Holder Address Purge's deployment contract (#331, parent #322, ADR 0046).
//
// What the purge DOES is proved over HTTP against a real database, in
// integration/holder_address_purge_test.go — that an unaccepted address goes at
// Event start, that an accepted one does not, and that the Ticket, its Answers
// and the fact of the assignment all survive. Nothing here repeats any of it.
//
// What is left is everything the behaviour tests structurally cannot see: this
// job's correctness lives half in Terraform, and a Go test is the only place the
// two halves are ever read side by side. These read Terraform's own defaults
// rather than mirroring them in constants, on the same terms the Abandoned
// Answer Purge's deployment tests do — a constant here is one more copy to
// forget.

// holderPurgeModuleDir is where the deployment's module lives, relative to this
// package.
func holderPurgeModuleDir() string {
	return filepath.Join("..", "..", "..", "..", "terraform", "modules", "ticket-pos")
}

// readTerraform reads a Terraform source file or fails the test.
func readTerraform(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(source)
}

// terraformVariableBody pulls one `variable "name" { ... }` block out of a
// variables file.
func terraformVariableBody(t *testing.T, path, name string) string {
	t.Helper()
	source := readTerraform(t, path)
	block := regexp.MustCompile(`(?s)variable "` + regexp.QuoteMeta(name) + `" \{.*?\n\}`).FindString(source)
	if block == "" {
		t.Fatalf("no variable %q in %s", name, path)
	}
	return block
}

// terraformSecondsVariable reads one numeric variable's default as a duration.
func terraformSecondsVariable(t *testing.T, path, name string) time.Duration {
	t.Helper()
	block := terraformVariableBody(t, path, name)
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

// TestTheHolderAddressPurgeShipsPaused is the assertion that keeps a deletion
// job from starting to fire the moment it is deployed.
//
// Every scheduled job in this deployment ships paused, and for this one the
// house rule is not the only reason. The column it empties is filled by a buyer
// surface behind TICKET_ASSIGNMENT_ENABLED, which is off until a Policy Version
// describing this collection publishes (ADR 0045) — so a purge that fired on
// deploy would be a nightly UPDATE matching no rows, running unwatched for
// however many months separate this ticket from that policy. The first run
// should happen when somebody is looking.
//
// It asserts the DEFAULT rather than the resource, because `paused` is wired to
// the variable and it is the variable's default that decides what a fresh apply
// does.
func TestTheHolderAddressPurgeShipsPaused(t *testing.T) {
	variables := filepath.Join(holderPurgeModuleDir(), "variables.tf")
	block := terraformVariableBody(t, variables, "holder_address_purge_enabled")

	if !regexp.MustCompile(`(?m)^\s*default\s*=\s*false\s*$`).MatchString(block) {
		t.Fatalf("holder_address_purge_enabled does not default to false — a job that DELETES contact details for people who never came to this platform would start firing on deploy, unwatched:\n%s", block)
	}
}

// TestTheHolderAddressPurgeIsPausableWithoutADeploy pins the incident lever, and
// for this job it is the ONLY lever there is.
//
// `paused = !var.holder_address_purge_enabled` is what makes stopping this job a
// `terraform apply -var holder_address_purge_enabled=false` rather than a console
// click the next apply silently undoes. It matters more here than for any job
// beside it: the purge is deliberately NOT gated on TICKET_ASSIGNMENT_ENABLED —
// the switch that turns a deletion off must never be the switch that turns
// collection off — so there is no second backend switch to fall back on, and its
// runs are only ever legible as absences.
//
// A schedule wired straight to a hardcoded `paused = false`, or a job that
// existed only when enabled, would both pass a behaviour test and fail an
// operator at 3am.
func TestTheHolderAddressPurgeIsPausableWithoutADeploy(t *testing.T) {
	source := readTerraform(t, filepath.Join(holderPurgeModuleDir(), "holder_address_purge.tf"))

	if !regexp.MustCompile(`(?m)^\s*paused\s*=\s*!var\.holder_address_purge_enabled\s*$`).MatchString(source) {
		t.Fatal("the holder address purge job's `paused` is not wired to !var.holder_address_purge_enabled: pausing it has to be an apply from the env directory, or the state and reality disagree the moment somebody pauses it in the console")
	}

	// The job, its identity and its grant must all EXIST while paused, so that
	// "turn it back on" is one variable rather than an apply that recreates three
	// resources under pressure. A `count` on any of them would undo that.
	if regexp.MustCompile(`(?m)^\s*count\s*=`).MatchString(source) {
		t.Fatal("the holder address purge job's resources are conditional on `count`: disabling must PAUSE the job, never delete it and its service account, or re-enabling is a three-resource apply during an incident")
	}
}

// TestTheHolderAddressPurgeHasItsOwnIdentityAndGrant pins the two resources that
// make an unexplained deletion traceable to a caller.
//
// This is the SECOND scheduled job in the deployment that deletes anything, and
// that is precisely why it may not borrow the first one's account: an audit log
// has to be able to say "the holder address purge ran" as a different sentence
// from "the answer purge ran", and revoking one must never silently revoke the
// other. Reusing an existing identity would pass every behaviour test and leave
// an incident with two suspects and one name.
//
// GCP caps a service account id at 30 characters, which is why the id is
// abbreviated to "holder-purge"; the assertion below is what stops somebody
// "tidying" it back to the full name and discovering the limit at apply time.
func TestTheHolderAddressPurgeHasItsOwnIdentityAndGrant(t *testing.T) {
	source := readTerraform(t, filepath.Join(holderPurgeModuleDir(), "holder_address_purge.tf"))

	account := regexp.MustCompile(`(?m)^\s*account_id\s*=\s*"\$\{var\.environment\}-(.*)"\s*$`).FindStringSubmatch(source)
	if account == nil {
		t.Fatal("the holder address purge has no service account of its own: an unexplained deletion is traceable to a caller only if the caller is distinguishable from the other job that also deletes")
	}
	// The longest environment name the module's own validation admits is 21
	// characters, but the name that actually has to fit is production's. Assert
	// against `prod`, which is the deployment this repository has.
	if id := "prod-" + account[1]; len(id) > 30 {
		t.Fatalf("service account id %q is %d characters; GCP caps it at 30 and the apply fails, not the plan", id, len(id))
	}

	if !regexp.MustCompile(`role\s*=\s*"roles/run\.invoker"`).MatchString(source) {
		t.Fatal("the holder address purge has no run.invoker grant of its own: everything it may do is one authenticated call to one Cloud Run service, and that has to be granted to its own identity")
	}
}

// TestTheHolderAddressPurgeEndpointCannotBeAimed is a contract about the URL,
// and it is the one that would be cheapest to break by accident.
//
// The moment is computed in PurgeUnacceptedHolderAddresses from this service's
// clock. If a future convenience ever moved it into the URL — an instant "for a
// backfill", an event id "to purge one Event" — then possession of the
// scheduler's OIDC token would become possession of a button that takes the
// holder address off every future Event on the platform. The URI in Terraform
// carries no query and no path parameter, and this is what says so.
func TestTheHolderAddressPurgeEndpointCannotBeAimed(t *testing.T) {
	source := readTerraform(t, filepath.Join(holderPurgeModuleDir(), "holder_address_purge.tf"))

	uri := regexp.MustCompile(`(?m)^\s*uri\s*=\s*"([^"]*)"`).FindStringSubmatch(source)
	if uri == nil {
		t.Fatal("the holder address purge job has no http_target uri")
	}
	const want = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/holder-addresses/purge"
	if uri[1] != want {
		t.Fatalf("holder address purge uri = %q, want %q — a moment or a tenant in this URL turns a retention job into a delete-everything button", uri[1], want)
	}
}

// TestTheHolderAddressPurgeDeadlineIsInsideTheRequestTimeout is the deadline
// chain, which for this job has two terms rather than the reversal drain's
// three: the run has no budget of its own to expire first, because it is a
// single UPDATE.
//
// A Scheduler deadline above Cloud Run's request timeout would mean Scheduler
// waiting on a request the platform has already killed, and recording the run as
// a timeout rather than as whatever actually went wrong.
func TestTheHolderAddressPurgeDeadlineIsInsideTheRequestTimeout(t *testing.T) {
	variables := filepath.Join(holderPurgeModuleDir(), "variables.tf")
	deadline := terraformSecondsVariable(t, variables, "holder_address_purge_attempt_deadline_seconds")
	requestTimeout := terraformSecondsVariable(t, variables, "api_request_timeout_seconds")

	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline = %v, Cloud Run's request timeout = %v: the deadline must expire first, or Scheduler is waiting on a request Cloud Run has already killed",
			deadline, requestTimeout)
	}
}

// TestTheHolderAddressPurgeResultIsCountsAndAnInstant pins the response's
// shape now that it reports two kinds of address (#424, ADR 0058).
//
// The corrected-address figure is its own field rather than folded into
// `addresses_purged`, because it is a different liability — an address a
// Platform Operator typed on a buyer's word, not one a buyer typed for a friend
// — and an operator reading the runbook needs to see which promise a run kept.
// And the whole payload is still numbers and one timestamp: a field that named
// an address, a Ticket, a Sale or an Event would publish what the deletion
// exists to remove, and this is where somebody adding one would be stopped.
func TestTheHolderAddressPurgeResultIsCountsAndAnInstant(t *testing.T) {
	encoded, err := json.Marshal(service.HolderAddressPurgeResult{
		AddressesPurged: 2, EventsPurged: 1, PurgedAt: "2026-07-07T12:00:00Z",
		AddressesHeld: 3, CorrectedAddressesPurged: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"addresses_purged":           float64(2),
		"events_purged":              float64(1),
		"purged_at":                  "2026-07-07T12:00:00Z",
		"addresses_held":             float64(3),
		"corrected_addresses_purged": float64(1),
	}
	if len(fields) != len(want) {
		t.Fatalf("the purge result has fields %v, want exactly %v — every field here is a count or the instant, and nothing may name what was deleted", fields, want)
	}
	for name, value := range want {
		if fields[name] != value {
			t.Errorf("result[%q] = %v, want %v", name, fields[name], value)
		}
	}
}
