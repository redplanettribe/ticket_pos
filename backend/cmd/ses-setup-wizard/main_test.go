package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// The wizard's seam (#698): the whole command, run() with its flags, the
// operator's answers on stdin and a fake commander standing in for the AWS
// CLI, Terraform and the send check. The tests assert what the operator
// reads, which commands run and the exit code, never how a step is built.

const (
	testRecipient   = "operator@example.com"
	testBudgetEmail = "budget-alerts@example.com"
	memberAccount   = "222222222222"
	managementAcct  = "111111111111"
)

// reply answers one command. n counts the calls with the same key so far,
// from 0, so a reply can change between a check and its retry.
type reply func(c command, n int) ([]byte, error)

func ok(out string) reply { return func(command, int) ([]byte, error) { return []byte(out), nil } }

func fails(msg string) reply {
	return func(command, int) ([]byte, error) { return nil, errors.New(msg) }
}

// thenOK fails the first call and answers out from the second on.
func thenOK(msg, out string) reply {
	return func(_ command, n int) ([]byte, error) {
		if n == 0 {
			return nil, errors.New(msg)
		}
		return []byte(out), nil
	}
}

const verifiedIdentity = `{"VerifiedForSendingStatus": true, "DkimAttributes": {"Status": "SUCCESS"}, "MailFromAttributes": {"MailFromDomain": "bounce.x", "MailFromDomainStatus": "SUCCESS"}}`

// happyReplies is a member account where everything works: Terraform has
// applied, both identities are verified, the recipient is new to SES and
// verifies once created, and the account is in the sandbox with no request.
func happyReplies() map[string]reply {
	return map[string]reply{
		"aws sts get-caller-identity":                            ok(`{"Account": "` + memberAccount + `", "Arn": "arn:aws:sts::` + memberAccount + `:assumed-role/AWSReservedSSO_AdministratorAccess/op"}`),
		"aws organizations describe-organization":                ok(`{"Organization": {"Id": "o-abc123", "MasterAccountId": "` + managementAcct + `"}}`),
		"terraform apply":                                        ok(""),
		"terraform output -json ses_dns_records":                 ok(`{"send.multiticketing.com": [], "digest.multiticketing.com": []}`),
		"terraform output -raw ses_configuration_set_name":       ok("prod-ticket-pos"),
		"terraform output -raw ses_dns_setup":                    ok("SES DNS CHECKLIST\n"),
		"aws sesv2 get-configuration-set":                        ok(`{}`),
		"aws sesv2 get-email-identity send.multiticketing.com":   ok(verifiedIdentity),
		"aws sesv2 get-email-identity digest.multiticketing.com": ok(verifiedIdentity),
		"aws sesv2 get-email-identity " + testRecipient: func(_ command, n int) ([]byte, error) {
			if n == 0 {
				return nil, errors.New("aws: exit status 254: An error occurred (NotFoundException) when calling the GetEmailIdentity operation")
			}
			return []byte(`{"VerifiedForSendingStatus": true}`), nil
		},
		"aws sesv2 create-email-identity": ok(`{}`),
		"go run ./cmd/ses-send-check":     ok(""),
		"aws sesv2 get-account": func(_ command, n int) ([]byte, error) {
			if n == 0 {
				return []byte(`{"ProductionAccessEnabled": false, "Details": {}}`), nil
			}
			return []byte(`{"ProductionAccessEnabled": false, "Details": {"ReviewDetails": {"Status": "PENDING", "CaseId": "1234"}}}`), nil
		},
		"aws sesv2 put-account-details": ok(`{}`),
	}
}

type fakeCommander struct {
	t       *testing.T
	replies map[string]reply
	calls   []command
	counts  map[string]int
}

// key names a command by its program and first arguments: the subcommand
// for aws and Terraform (with the identity for get-email-identity), the
// package for go run.
func key(c command) string {
	var args []string
	for _, a := range c.Args {
		if strings.HasPrefix(a, "-chdir=") {
			continue
		}
		if strings.HasPrefix(a, "--") || a == "-to" {
			if a == "--email-identity" && len(args) == 2 && args[1] == "get-email-identity" {
				continue
			}
			break
		}
		args = append(args, a)
	}
	switch {
	case c.Name == "terraform" && len(args) > 0 && args[0] == "output":
		return strings.Join(append([]string{c.Name}, args...), " ")
	case c.Name == "aws" && len(args) == 3:
		return strings.Join(append([]string{c.Name}, args...), " ")
	case len(args) > 2:
		args = args[:2]
	}
	return strings.Join(append([]string{c.Name}, args...), " ")
}

func (f *fakeCommander) answer(c command) ([]byte, error) {
	f.calls = append(f.calls, c)
	k := key(c)
	r, found := f.replies[k]
	if !found {
		f.t.Errorf("unexpected command %q (key %q)", c, k)
		return nil, fmt.Errorf("unexpected command %q", c)
	}
	n := f.counts[k]
	f.counts[k]++
	return r(c, n)
}

func (f *fakeCommander) Output(_ context.Context, c command) ([]byte, error) { return f.answer(c) }

func (f *fakeCommander) Run(_ context.Context, c command) error {
	_, err := f.answer(c)
	return err
}

// called lists the keys of the commands run, in order.
func (f *fakeCommander) called() []string {
	keys := make([]string, len(f.calls))
	for i, c := range f.calls {
		keys[i] = key(c)
	}
	return keys
}

func (f *fakeCommander) find(k string) (command, bool) {
	for _, c := range f.calls {
		if key(c) == k {
			return c, true
		}
	}
	return command{}, false
}

type result struct {
	code   int
	out    string
	errOut string
	cmd    *fakeCommander
}

func runWizard(t *testing.T, args []string, input string, replies map[string]reply, env map[string]string) result {
	t.Helper()
	cmd := &fakeCommander{t: t, replies: replies, counts: map[string]int{}}
	if env == nil {
		env = map[string]string{budgetAlertVariable: testBudgetEmail}
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(input), &stdout, &stderr, func(k string) string { return env[k] }, cmd)
	return result{code: code, out: stdout.String(), errOut: stderr.String(), cmd: cmd}
}

// The answers a run from step 1 to the end needs when every check passes:
// Enter (step 1), Enter (step 2), y to apply (step 3), Enter (step 4), the
// recipient (step 6), Enter for the default volume and y to submit (step 7).
var happyInput = strings.Join([]string{"", "", "y", "", testRecipient, "", "y"}, "\n") + "\n"

func stepHeadings(out string) []int {
	var seen []int
	for n := 1; n <= len(steps); n++ {
		if strings.Contains(out, fmt.Sprintf("== Step %d of %d:", n, len(steps))) {
			seen = append(seen, n)
		}
	}
	return seen
}

func TestWizardWalksTheSevenStepsInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	r := runWizard(t, nil, happyInput, happyReplies(), nil)

	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s%s", r.code, r.out, r.errOut)
	}
	titles := []string{
		"Organization", "Identity Center", "Terraform", "Namecheap",
		"verified", "send check", "production access",
	}
	last := -1
	for n, title := range titles {
		heading := fmt.Sprintf("== Step %d of 7: ", n+1)
		at := strings.Index(r.out, heading)
		if at < 0 || at < last {
			t.Fatalf("step %d heading missing or out of order\n%s", n+1, r.out)
		}
		if line, _, _ := strings.Cut(r.out[at:], "\n"); !strings.Contains(line, title) {
			t.Errorf("step %d heading %q does not name %q", n+1, line, title)
		}
		last = at
	}

	wantOrder := []string{
		"aws sts get-caller-identity",
		"aws organizations describe-organization",
		"terraform apply",
		"terraform output -raw ses_dns_setup",
		"aws sesv2 get-email-identity " + testRecipient,
		"aws sesv2 create-email-identity",
		"go run ./cmd/ses-send-check",
		"aws sesv2 put-account-details",
	}
	called := r.cmd.called()
	pos := -1
	for _, k := range wantOrder {
		i := slices.Index(called[pos+1:], k)
		if i < 0 {
			t.Fatalf("%q not run after position %d; ran %q", k, pos, called)
		}
		pos += i + 1
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the wizard wrote %d entries into its working directory", len(entries))
	}
}

func TestWizardRunsTerraformAndTheSendCheckOnTheProfile(t *testing.T) {
	r := runWizard(t, nil, happyInput, happyReplies(), nil)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.out)
	}

	apply, _ := r.cmd.find("terraform apply")
	if !slices.Contains(apply.Args, "-chdir="+defaultTerraformDir) {
		t.Errorf("apply args = %q, want -chdir=%s", apply.Args, defaultTerraformDir)
	}
	for _, want := range []string{"TF_VAR_aws_profile=" + defaultProfile, "TF_VAR_aws_region=" + defaultRegion} {
		if !slices.Contains(apply.Env, want) {
			t.Errorf("apply env = %q, want %s", apply.Env, want)
		}
	}

	check, _ := r.cmd.find("go run ./cmd/ses-send-check")
	if want := []string{"run", "./cmd/ses-send-check", "-to", testRecipient}; !slices.Equal(check.Args, want) {
		t.Errorf("send check args = %q, want %q", check.Args, want)
	}
	for _, want := range []string{
		"AWS_PROFILE=" + defaultProfile,
		"SES_REGION=" + defaultRegion,
		"SES_CONFIGURATION_SET=prod-ticket-pos",
		"EMAIL_FROM=" + defaultEmailFrom,
		"DIGEST_EMAIL_FROM=" + defaultDigestEmailFrom,
	} {
		if !slices.Contains(check.Env, want) {
			t.Errorf("send check env = %q, want %s", check.Env, want)
		}
	}

	for _, c := range r.cmd.calls {
		if c.Name != "aws" {
			continue
		}
		if !slices.Contains(c.Args, "--profile") || !slices.Contains(c.Args, defaultProfile) || !slices.Contains(c.Args, defaultRegion) {
			t.Errorf("%q does not run on profile %s in %s", c, defaultProfile, defaultRegion)
		}
	}
}

func TestWizardNeverWritesOrEchoesTheAddresses(t *testing.T) {
	r := runWizard(t, nil, happyInput, happyReplies(), nil)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.out)
	}
	for _, secret := range []string{testRecipient, testBudgetEmail} {
		if strings.Contains(r.out, secret) || strings.Contains(r.errOut, secret) {
			t.Errorf("the wizard printed %q", secret)
		}
	}
	for _, c := range r.cmd.calls {
		if strings.Contains(strings.Join(c.Env, " "), testBudgetEmail) || strings.Contains(c.String(), testBudgetEmail) {
			t.Errorf("%q carries the budget address itself; it should reach Terraform only through the inherited environment", c)
		}
	}
}

func TestWizardEndsSayingNothingHasSwitched(t *testing.T) {
	r := runWizard(t, nil, happyInput, happyReplies(), nil)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.out)
	}
	_, tail, found := strings.Cut(r.out, "== Step 7 of 7")
	if !found {
		t.Fatalf("no step 7\n%s", r.out)
	}
	for _, want := range []string{
		"Nothing has switched",
		"Resend still carries all traffic",
		`still "resend"`,
		`email_provider = "ses"`,
		"EMAIL_PROVIDER",
		`digest_email_provider = "ses"`,
		"DIGEST_EMAIL_PROVIDER",
	} {
		if !strings.Contains(tail, want) {
			t.Errorf("the end does not say %q\n%s", want, tail)
		}
	}
}

func TestWizardResumesFromAGivenStep(t *testing.T) {
	for from := 1; from <= len(steps); from++ {
		t.Run(fmt.Sprint("step ", from), func(t *testing.T) {
			r := runWizard(t, []string{"-from-step", fmt.Sprint(from)}, strings.Repeat("y\n", 12)+testRecipient+"\n\ny\n", resumeReplies(), nil)
			var want []int
			for n := from; n <= len(steps); n++ {
				want = append(want, n)
			}
			if got := stepHeadings(r.out); !slices.Equal(got, want) {
				t.Errorf("steps shown = %v, want %v\n%s", got, want, r.out)
			}
		})
	}
}

// resumeReplies are happyReplies with the recipient already verified, so a
// run's answers do not depend on where it starts beyond the recipient.
func resumeReplies() map[string]reply {
	replies := happyReplies()
	replies["aws sesv2 get-email-identity "+testRecipient] = ok(`{"VerifiedForSendingStatus": true}`)
	return replies
}

func TestResumingAtStepFiveRunsNothingEarlier(t *testing.T) {
	r := runWizard(t, []string{"-from-step", "5"}, testRecipient+"\n\ny\n", happyReplies(), nil)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.out)
	}
	for _, k := range []string{"aws sts get-caller-identity", "terraform apply", "terraform output -raw ses_dns_setup"} {
		if slices.Contains(r.cmd.called(), k) {
			t.Errorf("resuming at step 5 ran %q", k)
		}
	}
}

func TestStepTwoBlocksUntilTheProfileReachesTheMemberAccount(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		replies map[string]reply
		says    string
	}{
		{
			name:    "not signed in",
			replies: map[string]reply{"aws sts get-caller-identity": fails("Error when retrieving token from sso: Token has expired")},
			says:    "aws sso login --profile " + defaultProfile,
		},
		{
			name:    "not in an Organization",
			replies: map[string]reply{"aws organizations describe-organization": fails("AWSOrganizationsNotInUseException")},
			says:    "does not answer as a member of an Organization",
		},
		{
			name: "the management account",
			replies: map[string]reply{
				"aws sts get-caller-identity": ok(`{"Account": "` + managementAcct + `"}`),
			},
			says: "management account",
		},
		{
			name:    "another member than -account",
			args:    []string{"-account", "333333333333"},
			replies: map[string]reply{},
			says:    "333333333333",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replies := happyReplies()
			for k, r := range tc.replies {
				replies[k] = r
			}
			args := append([]string{"-from-step", "2"}, tc.args...)
			r := runWizard(t, args, "\nq\n", replies, nil)

			if r.code != 1 {
				t.Errorf("exit code = %d, want 1", r.code)
			}
			if !strings.Contains(r.out, "NOT YET") || !strings.Contains(r.out, tc.says) {
				t.Errorf("output does not refuse with %q\n%s", tc.says, r.out)
			}
			if got := stepHeadings(r.out); !slices.Equal(got, []int{2}) {
				t.Errorf("steps shown = %v, want only 2", got)
			}
			if !strings.Contains(r.out, "-from-step 2") {
				t.Errorf("output does not say how to resume\n%s", r.out)
			}
		})
	}
}

func TestStepTwoPassesWithTheGivenAccount(t *testing.T) {
	r := runWizard(t, []string{"-from-step", "2", "-account", memberAccount}, "\nq\n", happyReplies(), nil)
	if !strings.Contains(r.out, "Step 2 done") {
		t.Errorf("step 2 did not pass\n%s", r.out)
	}
}

func TestStepTwoChecksAgainUntilItPasses(t *testing.T) {
	replies := happyReplies()
	replies["aws sts get-caller-identity"] = thenOK("Token has expired", `{"Account": "`+memberAccount+`"}`)

	r := runWizard(t, []string{"-from-step", "2"}, "\n\nq\n", replies, nil)

	if strings.Count(r.out, "NOT YET") != 1 || !strings.Contains(r.out, "Step 2 done") {
		t.Errorf("step 2 did not pass on its second check\n%s", r.out)
	}
}

func TestStepThreeRefusesWithoutTheBudgetAddress(t *testing.T) {
	r := runWizard(t, []string{"-from-step", "3"}, "", happyReplies(), map[string]string{})

	if r.code != 1 {
		t.Errorf("exit code = %d, want 1", r.code)
	}
	if !strings.Contains(r.out, budgetAlertVariable+" is not set") {
		t.Errorf("output does not name %s\n%s", budgetAlertVariable, r.out)
	}
	if slices.Contains(r.cmd.called(), "terraform apply") {
		t.Error("terraform apply ran without the budget address")
	}
}

func TestStepThreeCanLeaveTheApplyToTheOperator(t *testing.T) {
	r := runWizard(t, []string{"-from-step", "3"}, "n\n\nq\n", happyReplies(), nil)

	if slices.Contains(r.cmd.called(), "terraform apply") {
		t.Error("terraform apply ran after the operator said no")
	}
	if !strings.Contains(r.out, "terraform -chdir="+defaultTerraformDir+" apply") || !strings.Contains(r.out, "Step 3 done") {
		t.Errorf("step 3 did not give the command and check\n%s", r.out)
	}
}

func TestStepThreeBlocksUntilTheResourcesExist(t *testing.T) {
	replies := happyReplies()
	replies["terraform apply"] = fails("exit status 1")
	replies["terraform output -json ses_dns_records"] = fails("Output \"ses_dns_records\" not found")

	r := runWizard(t, []string{"-from-step", "3"}, "y\nq\n", replies, nil)

	if r.code != 1 || !strings.Contains(r.out, "NOT YET") || strings.Contains(r.out, "Step 3 done") {
		t.Errorf("step 3 did not block (exit %d)\n%s", r.code, r.out)
	}
}

func TestStepFiveBlocksUntilBothIdentitiesAreVerified(t *testing.T) {
	cases := map[string]string{
		"DKIM pending":      `{"VerifiedForSendingStatus": false, "DkimAttributes": {"Status": "PENDING"}, "MailFromAttributes": {"MailFromDomainStatus": "SUCCESS"}}`,
		"MAIL FROM pending": `{"VerifiedForSendingStatus": true, "DkimAttributes": {"Status": "SUCCESS"}, "MailFromAttributes": {"MailFromDomainStatus": "PENDING"}}`,
		"MAIL FROM failed":  `{"VerifiedForSendingStatus": true, "DkimAttributes": {"Status": "SUCCESS"}, "MailFromAttributes": {"MailFromDomainStatus": "FAILED"}}`,
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			replies := happyReplies()
			replies["aws sesv2 get-email-identity digest.multiticketing.com"] = ok(identity)

			r := runWizard(t, []string{"-from-step", "5"}, "", replies, nil)

			if r.code != 1 {
				t.Errorf("exit code = %d, want 1", r.code)
			}
			if !strings.Contains(r.out, "not verified yet: digest.multiticketing.com") {
				t.Errorf("output does not name the unverified identity\n%s", r.out)
			}
			if strings.Contains(r.out, "send.multiticketing.com,") || strings.Contains(r.out, "Step 5 done") {
				t.Errorf("step 5 passed or blamed the verified identity\n%s", r.out)
			}
			if !strings.Contains(r.out, "-from-step 5") {
				t.Errorf("output does not say how to resume\n%s", r.out)
			}
		})
	}
}

func TestStepFiveMovesOnOnceVerified(t *testing.T) {
	replies := happyReplies()
	replies["aws sesv2 get-email-identity send.multiticketing.com"] = func(_ command, n int) ([]byte, error) {
		if n == 0 {
			return []byte(`{"VerifiedForSendingStatus": false, "DkimAttributes": {"Status": "PENDING"}}`), nil
		}
		return []byte(verifiedIdentity), nil
	}

	r := runWizard(t, []string{"-from-step", "5"}, "\nq\n", replies, nil)

	if !strings.Contains(r.out, "Step 5 done") || !slices.Equal(stepHeadings(r.out), []int{5, 6}) {
		t.Errorf("step 5 did not move on after a passing check\n%s", r.out)
	}
}

func TestStepSixBlocksOnTheSendCheckExitCode(t *testing.T) {
	replies := resumeReplies()
	replies["go run ./cmd/ses-send-check"] = fails("go: exit status 1")

	r := runWizard(t, []string{"-from-step", "6"}, testRecipient+"\n\nq\n", replies, nil)

	if r.code != 1 || strings.Contains(r.out, "Step 6 done") {
		t.Errorf("step 6 passed a failing send check (exit %d)\n%s", r.code, r.out)
	}
	if n := slices.Index(r.cmd.called(), "go run ./cmd/ses-send-check"); n < 0 {
		t.Fatal("the send check never ran")
	}
	if got := r.cmd.counts["go run ./cmd/ses-send-check"]; got != 2 {
		t.Errorf("send check ran %d times, want 2 (Enter checks again)", got)
	}
	if !strings.Contains(r.out, "-from-step 6") {
		t.Errorf("output does not say how to resume\n%s", r.out)
	}
}

func TestStepSixWaitsForTheRecipientToVerify(t *testing.T) {
	replies := happyReplies()
	replies["aws sesv2 get-email-identity "+testRecipient] = func(_ command, n int) ([]byte, error) {
		if n == 0 {
			return nil, errors.New("An error occurred (NotFoundException)")
		}
		return []byte(`{"VerifiedForSendingStatus": false}`), nil
	}

	r := runWizard(t, []string{"-from-step", "6"}, "not an address\n"+testRecipient+"\n", replies, nil)

	if r.code != 1 || !strings.Contains(r.out, "not verified yet") {
		t.Errorf("step 6 did not wait for the recipient (exit %d)\n%s", r.code, r.out)
	}
	if !strings.Contains(r.out, "not a bare email address") {
		t.Errorf("an invalid address was not refused\n%s", r.out)
	}
	if slices.Contains(r.cmd.called(), "go run ./cmd/ses-send-check") {
		t.Error("the send check ran before the recipient was verified")
	}
	if c, _ := r.cmd.find("aws sesv2 create-email-identity"); !slices.Contains(c.Args, testRecipient) {
		t.Errorf("create-email-identity args = %q, want the recipient", c.Args)
	}
}

func TestStepSevenDraftsAndSubmitsTheRequest(t *testing.T) {
	r := runWizard(t, []string{"-from-step", "7"}, "about 300\ny\n", happyReplies(), nil)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.out)
	}
	put, found := r.cmd.find("aws sesv2 put-account-details")
	if !found {
		t.Fatal("no request was submitted")
	}
	arg := func(name string) string {
		i := slices.Index(put.Args, name)
		if i < 0 || i+1 >= len(put.Args) {
			return ""
		}
		return put.Args[i+1]
	}
	if !slices.Contains(put.Args, "--production-access-enabled") || arg("--mail-type") != "TRANSACTIONAL" || arg("--website-url") != defaultWebsiteURL {
		t.Errorf("put-account-details args = %q", put.Args)
	}
	description := arg("--use-case-description")
	for _, want := range []string{"One-time passcodes", "tax documents", "weekly digest", "digest.multiticketing.com", "unsubscribe", "suppression", "about 300 messages a day"} {
		if !strings.Contains(description, want) {
			t.Errorf("the description does not say %q:\n%s", want, description)
		}
	}
	if !strings.Contains(r.out, "under review (case 1234)") {
		t.Errorf("output does not show the pending review\n%s", r.out)
	}
}

func TestStepSevenSkipsARequestAlreadyMade(t *testing.T) {
	cases := map[string]string{
		"granted": `{"ProductionAccessEnabled": true}`,
		"pending": `{"ProductionAccessEnabled": false, "Details": {"ReviewDetails": {"Status": "PENDING", "CaseId": "1234"}}}`,
	}
	for name, account := range cases {
		t.Run(name, func(t *testing.T) {
			replies := happyReplies()
			replies["aws sesv2 get-account"] = ok(account)

			r := runWizard(t, []string{"-from-step", "7"}, "", replies, nil)

			if r.code != 0 {
				t.Errorf("exit code = %d\n%s", r.code, r.out)
			}
			if slices.Contains(r.cmd.called(), "aws sesv2 put-account-details") {
				t.Error("a second request was submitted")
			}
			if !strings.Contains(r.out, "Nothing has switched") {
				t.Errorf("no closing word\n%s", r.out)
			}
		})
	}
}

func TestStepSevenBlocksWhenNoRequestIsOnRecord(t *testing.T) {
	replies := happyReplies()
	replies["aws sesv2 get-account"] = ok(`{"ProductionAccessEnabled": false, "Details": {}}`)

	r := runWizard(t, []string{"-from-step", "7"}, "\nn\n\nq\n", replies, nil)

	if r.code != 1 || !strings.Contains(r.out, "no production access request") || strings.Contains(r.out, "Nothing has switched") {
		t.Errorf("step 7 passed with no request on record (exit %d)\n%s", r.code, r.out)
	}
}

func TestBadFlagsExitTwo(t *testing.T) {
	for _, args := range [][]string{{"-from-step", "0"}, {"-from-step", "8"}, {"-nope"}, {"extra"}} {
		r := runWizard(t, args, "", happyReplies(), nil)
		if r.code != 2 {
			t.Errorf("%q: exit code = %d, want 2", args, r.code)
		}
		if len(r.cmd.calls) != 0 {
			t.Errorf("%q: ran %q", args, r.cmd.called())
		}
	}
}
