package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
)

// Step 1. Nothing here can be checked from the member account's profile,
// which does not exist yet; step 2's check proves the account is a member.
func (w *wizard) organization(ctx context.Context) error {
	w.printf(`The personal AWS account becomes the AWS Organization's management account
and does nothing else; SES runs in an AWS member account of its own (ADR 0077).

Signed in to the personal account's root user, in the AWS console:

  1. Secure the root user: IAM > Security credentials. Assign an MFA device,
     and delete every root access key (there should be none).
  2. AWS Organizations > Create an organization (all features).
  3. AWS Organizations > Add an AWS account > Create an AWS account, for the
     platform (for example "multiticketing-ses"). Give it an address you
     control; it becomes the member account's root email.
  4. Note the new account's 12-digit ID: step 2 asks for it (or pass it as
     -account) to check the profile reaches exactly this account.

How to tell it worked: AWS Organizations lists two accounts, the management
account and the new member, and the root user shows MFA assigned and no
access keys.

`)
	return w.confirm("the AWS Organization and the member account exist and the root user has MFA and no access keys")
}

// Step 2. Checked: the profile signs in, and the account it reaches belongs
// to an Organization without being its management account.
func (w *wizard) identityCenter(ctx context.Context) error {
	w.printf(`Humans reach the member account through IAM Identity Center, never an IAM
user or an access key. In the management account's console:

  1. IAM Identity Center > Enable (in %[2]s).
  2. Users > Add user: an admin user for yourself; accept the invitation
     mail and set its password and MFA.
  3. Permission sets > Create: the predefined AdministratorAccess.
  4. AWS accounts > the member account > Assign users or groups: your
     user, with that permission set.
  5. On this machine, configure the profile and sign in:

       aws configure sso --profile %[1]s
         (SSO start URL from the Identity Center dashboard, region %[2]s,
          the member account, AdministratorAccess, default region %[2]s)
       aws sso login --profile %[1]s

How to tell it worked: the profile signs in and reaches the member account,
which the wizard checks now.

`, w.profile, w.region)
	if err := w.confirm("the profile is configured and signed in"); err != nil {
		return err
	}
	if w.account == "" {
		account, err := w.memberAccountID()
		if err != nil {
			return err
		}
		w.account = account
	}
	return w.until(fmt.Sprintf("profile %s works against the member account", w.profile), func() error {
		return w.checkAWSMemberAccount(ctx)
	})
}

func (w *wizard) checkAWSMemberAccount(ctx context.Context) error {
	var caller struct{ Account, Arn string }
	if err := w.awsJSON(ctx, &caller, "sts", "get-caller-identity"); err != nil {
		return fmt.Errorf("the profile does not sign in (run: aws sso login --profile %s): %w", w.profile, err)
	}
	var org struct {
		Organization struct{ Id, MasterAccountId string }
	}
	if err := w.awsJSON(ctx, &org, "organizations", "describe-organization"); err != nil {
		return fmt.Errorf("account %s does not answer as a member of an AWS Organization: %w", caller.Account, err)
	}
	if caller.Account == org.Organization.MasterAccountId {
		return fmt.Errorf("profile %s reaches account %s, the AWS Organization's management account; assign it the member account instead", w.profile, caller.Account)
	}
	if w.account != "" && caller.Account != w.account {
		return fmt.Errorf("profile %s reaches account %s, not the member account %s", w.profile, caller.Account, w.account)
	}
	w.printf("  profile %s reaches account %s, a member of AWS Organization %s\n", w.profile, caller.Account, org.Organization.Id)
	return nil
}

// budgetAlertVariable is how the budget alert address reaches Terraform. It
// is personal, so it lives in the operator's sourced, gitignored .env and
// in no committed file; the wizard only checks it is set.
const budgetAlertVariable = "TF_VAR_aws_budget_alert_email"

// Step 3. Checked: the address is in the environment before the apply, and
// the SES resources exist in the member account after it.
func (w *wizard) terraformApply(ctx context.Context) error {
	w.printf(`One apply of %[1]s creates the SES side beside everything else
(ADR 0077): both sending identities, their MAIL FROM, the configuration set,
account-level suppression, the role the API assumes and the budget alert.
It changes no delivery: email_provider and digest_email_provider stay "resend".

Before the apply, in the shell you run this wizard from:

  gcloud auth application-default login     (the state lives in GCS)
  set -a; source .env; set +a                (the TF_VAR_* secrets, as usual)
  %[2]s must be set: the budget alert's address, never committed.

The wizard runs Terraform with TF_VAR_aws_profile=%[3]s and TF_VAR_aws_region=%[4]s,
so the apply uses the profile from step 2.

`, w.terraformDir, budgetAlertVariable, w.profile, w.region)
	if err := w.until(budgetAlertVariable+" is set in the environment", func() error {
		if strings.TrimSpace(w.getenv(budgetAlertVariable)) == "" {
			return fmt.Errorf("%s is not set; export it (for example from your sourced .env) and run the wizard again from step 3", budgetAlertVariable)
		}
		return nil
	}); err != nil {
		return err
	}

	if w.yes(fmt.Sprintf("Run terraform apply in %s now? Terraform shows the plan and asks for its own yes.", w.terraformDir)) {
		if err := w.cmd.Run(ctx, w.terraform("apply")); err != nil {
			w.printf("  terraform apply did not finish: %v\n", err)
		}
	} else {
		w.printf("Run it yourself, with the same environment:\n\n  TF_VAR_aws_profile=%s TF_VAR_aws_region=%s terraform -chdir=%s apply\n\n", w.profile, w.region, w.terraformDir)
		if err := w.confirm("the apply has finished"); err != nil {
			return err
		}
	}

	w.printf(`
How to tell it worked: Terraform outputs the SES identities, and the member
account has them and the configuration set, which the wizard checks now.

`)
	return w.until("the SES identities and configuration set exist in the member account", func() error {
		domains, err := w.sendingDomains(ctx)
		if err != nil {
			return err
		}
		set, err := w.configurationSet(ctx)
		if err != nil {
			return err
		}
		if err := w.awsJSON(ctx, nil, "sesv2", "get-configuration-set", "--configuration-set-name", set); err != nil {
			return fmt.Errorf("configuration set %s: %w", set, err)
		}
		for _, d := range domains {
			if err := w.awsJSON(ctx, nil, "sesv2", "get-email-identity", "--email-identity", d); err != nil {
				return fmt.Errorf("identity %s: %w", d, err)
			}
		}
		w.printf("  identities %s, configuration set %s\n", strings.Join(domains, ", "), set)
		return nil
	})
}

// Step 4. Not checked here: SES reads the records itself, and step 5 checks
// what it found.
func (w *wizard) dnsRecords(ctx context.Context) error {
	setup, err := w.cmd.Output(ctx, w.terraform("output", "-raw", "ses_dns_setup"))
	if err != nil {
		return fmt.Errorf("read the DNS checklist (terraform output -raw ses_dns_setup): %w", err)
	}
	w.printf(`DNS stays at Namecheap and is added by hand (ADR 0007). Add every record
below in Namecheap > Domain List > multiticketing.com > Advanced DNS, BESIDE
Resend's records: replace or remove none of them. Namecheap's Host field is
the name without ".multiticketing.com" (bounce.send for
bounce.send.multiticketing.com).

%s
How to tell it worked: SES verifies the identities on its own once the
records resolve, usually within minutes and at most 72 hours; step 5 checks.

`, setup)
	return w.confirm("every record is added at Namecheap")
}

// Step 5. Checked: each sending identity verified for sending, with DKIM
// and the custom MAIL FROM both SUCCESS.
func (w *wizard) identitiesVerified(ctx context.Context) error {
	w.printf(`SES must read both sending identities as verified before anything can send
from them, and before production access is worth requesting.

How to tell it worked: for each identity, verified for sending, DKIM
"SUCCESS" and MAIL FROM "SUCCESS". DNS can take a while; check again as
often as you like, or stop and resume with -from-step 5 later.

`)
	return w.until("both SES sending identities are verified (DKIM and MAIL FROM)", func() error {
		domains, err := w.sendingDomains(ctx)
		if err != nil {
			return err
		}
		var pending []string
		for _, d := range domains {
			var id struct {
				VerifiedForSendingStatus bool
				DkimAttributes           struct{ Status string }
				MailFromAttributes       struct{ MailFromDomain, MailFromDomainStatus string }
			}
			if err := w.awsJSON(ctx, &id, "sesv2", "get-email-identity", "--email-identity", d); err != nil {
				return fmt.Errorf("identity %s: %w", d, err)
			}
			w.printf("  %s: verified for sending %t, DKIM %s, MAIL FROM %s %s\n",
				d, id.VerifiedForSendingStatus, orDefault(id.DkimAttributes.Status, "-"),
				orDefault(id.MailFromAttributes.MailFromDomain, "-"), orDefault(id.MailFromAttributes.MailFromDomainStatus, "-"))
			if !id.VerifiedForSendingStatus || id.DkimAttributes.Status != "SUCCESS" || id.MailFromAttributes.MailFromDomainStatus != "SUCCESS" {
				pending = append(pending, d)
			}
		}
		if len(pending) > 0 {
			return fmt.Errorf("not verified yet: %s (compare the records at Namecheap with step 4's checklist)", strings.Join(pending, ", "))
		}
		return nil
	})
}

// Step 6. Checked: the recipient is verified, then the send check exits 0.
// The recipient is typed here and goes only to the AWS CLI and the send
// check's -to; it is never printed back or written anywhere.
func (w *wizard) sendCheck(ctx context.Context) error {
	w.printf(`In the sandbox SES delivers only to verified addresses. Choose an address
you read; AWS mails it a verification link. The wizard keeps it in memory
only: it is not written to any file. It stays a verified identity in the
member account until you delete it there.

Then the send check (make ses-send-check) sends one message of every type
the platform sends through SES to it, on your profile, and must report every
type ok.

`)
	to, err := w.recipient()
	if err != nil {
		return err
	}

	var id struct{ VerifiedForSendingStatus bool }
	if err := w.awsJSON(ctx, &id, "sesv2", "get-email-identity", "--email-identity", to); err != nil {
		if !strings.Contains(err.Error(), "NotFoundException") {
			return fmt.Errorf("look up the recipient in SES: %w", err)
		}
		if err := w.awsJSON(ctx, nil, "sesv2", "create-email-identity", "--email-identity", to); err != nil {
			return fmt.Errorf("ask SES to verify the recipient: %w", err)
		}
		w.printf("AWS has mailed a verification link to the address. Open it.\n\n")
	}
	if err := w.until("the recipient is verified in SES", func() error {
		if err := w.awsJSON(ctx, &id, "sesv2", "get-email-identity", "--email-identity", to); err != nil {
			return err
		}
		if !id.VerifiedForSendingStatus {
			return errors.New("the address is not verified yet; open the link AWS mailed to it")
		}
		return nil
	}); err != nil {
		return err
	}

	set, err := w.configurationSet(ctx)
	if err != nil {
		return err
	}
	check := command{
		Name: "go",
		Args: []string{"run", "./cmd/ses-send-check", "-to", to},
		Env: []string{
			"AWS_PROFILE=" + w.profile,
			"SES_REGION=" + w.region,
			"SES_CONFIGURATION_SET=" + set,
			"EMAIL_FROM=" + w.emailFrom,
			"DIGEST_EMAIL_FROM=" + w.digestEmailFrom,
		},
	}
	w.printf(`
How to tell it worked: every line of the send check says ok, it exits 0, and
every message arrives in the inbox. The wizard checks the exit code.

`)
	return w.until("the SES send check delivers every message type", func() error {
		if err := w.cmd.Run(ctx, check); err != nil {
			return fmt.Errorf("the send check failed (see its FAIL lines above): %w", err)
		}
		return nil
	})
}

// memberAccountID asks for the AWS member account's 12-digit ID, so step 2
// checks the profile reaches exactly it rather than any member account.
func (w *wizard) memberAccountID() (string, error) {
	for {
		answer, ok := w.ask("The member account's 12-digit ID (q to stop here):")
		if stops(answer, ok) {
			return "", errStopped
		}
		if len(answer) == 12 && strings.Trim(answer, "0123456789") == "" {
			return answer, nil
		}
		w.printf("  That is not a 12-digit AWS account ID.\n")
	}
}

// recipient asks for the send check's address until it is a bare one.
func (w *wizard) recipient() (string, error) {
	for {
		answer, ok := w.ask("Recipient address (q to stop here):")
		if stops(answer, ok) {
			return "", errStopped
		}
		if a, err := mail.ParseAddress(answer); err == nil && a.Name == "" && a.Address == answer {
			return answer, nil
		}
		w.printf("  That is not a bare email address, like you@example.com.\n")
	}
}

// Step 7. Checked: the account's production access state, before and after
// a request. A pending review is not something to wait for here: AWS
// answers in about a day, while Resend keeps carrying the mail.
func (w *wizard) productionAccess(ctx context.Context) error {
	w.printf(`A new SES account is in the sandbox: verified recipients only, 200 messages
a day, 1 a second. Production access is a request AWS reviews and can
refuse, so it is asked for now, while Resend still carries the load
(ADR 0077), and a refusal is learned before switch day.

`)
	account, err := w.sesAccount(ctx)
	if err != nil {
		return err
	}
	if account.ProductionAccessEnabled {
		w.printf("The member account already has production access in %s.\n", w.region)
		return nil
	}
	if status := account.Details.ReviewDetails.Status; status == "PENDING" {
		w.printf("A production access request is already under review (case %s). AWS answers by mail.\n", account.caseID())
		return nil
	}

	volume, ok := w.ask("Expected messages a day once switched (Enter for \"under 1,000\"):")
	if !ok {
		return errStopped
	}
	description := useCaseDescription(orDefault(volume, "under 1,000"), w.digestDomain())
	w.printf(`
The request (mail type TRANSACTIONAL, website %s). Use-case description:

%s

`, defaultWebsiteURL, indent(description))
	if w.yes("Submit this request now with the AWS CLI?") {
		if err := w.until("SES accepts the production access request", func() error {
			return w.awsJSON(ctx, nil, "sesv2", "put-account-details",
				"--production-access-enabled",
				"--mail-type", "TRANSACTIONAL",
				"--website-url", defaultWebsiteURL,
				"--contact-language", "EN",
				"--use-case-description", description)
		}); err != nil {
			return err
		}
	} else {
		w.printf(`Request it in the console instead, signed in to the member account:
SES (%s) > Get set up > Request production access, with the description above.

`, w.region)
		if err := w.confirm("the request is submitted"); err != nil {
			return err
		}
	}

	return w.until("the production access request is on record", func() error {
		account, err := w.sesAccount(ctx)
		if err != nil {
			return err
		}
		if account.ProductionAccessEnabled {
			w.printf("  production access granted\n")
			return nil
		}
		switch status := account.Details.ReviewDetails.Status; status {
		case "PENDING":
			w.printf("  under review (case %s); AWS answers by mail, usually within a day\n", account.caseID())
			return nil
		case "":
			return errors.New("SES shows no production access request for the account")
		default:
			return fmt.Errorf("the review reads %s (case %s); answer AWS's mail or the case in the Support Center, then check again", status, account.caseID())
		}
	})
}

type sesAccount struct {
	ProductionAccessEnabled bool
	Details                 struct {
		ReviewDetails struct{ Status, CaseId string }
	}
}

// caseID is the production access review's support case, "-" when none.
func (a sesAccount) caseID() string {
	return orDefault(a.Details.ReviewDetails.CaseId, "-")
}

func (w *wizard) sesAccount(ctx context.Context) (sesAccount, error) {
	var a sesAccount
	if err := w.awsJSON(ctx, &a, "sesv2", "get-account"); err != nil {
		return a, fmt.Errorf("read the SES account: %w", err)
	}
	return a, nil
}

// useCaseDescription drafts the production access request from what the
// platform sends: the message types the send check sends, grouped as a
// reviewer reads them.
func useCaseDescription(volume, digestDomain string) string {
	return strings.Join([]string{
		"Multiticketing (" + defaultWebsiteURL + ") is a ticketing and point-of-sale platform for organizations that run events in Ecuador.",
		"All mail is triggered by something the recipient did or is owed, and is plain text in Spanish or English.",
		"",
		"What we send:",
		"- One-time passcodes for signing in, for customers and for the staff of those organizations.",
		"- Sale mail: sale confirmations, notices that a sale was voided or reversed, and electronic tax documents (Ecuadorian SRI invoices, with their XML and PDF attached).",
		"- Ticket mail to the person holding a ticket: assignment, reassignment, reminders to complete it, and answers to the event's questions.",
		"- Confirmation that a customer's consent withdrawal was recorded.",
		"- Operational notices to those organizations and the platform operator: payout requests, question reviews and signing certificate expiry warnings.",
		"- A weekly digest of upcoming events, sent only to customers who chose to follow an organization, from its own subdomain (" + digestDomain + "), with an unsubscribe link in every message.",
		"",
		"Recipients: every address was given to us by the person in a sale, a sign-in or a follow, or by a customer for the holder of a ticket they bought. We never buy, rent or import lists.",
		"",
		"Bounces and complaints: the account-level suppression list is on for bounces and complaints, so an address that bounces or complains is not mailed again. Both sending subdomains are verified with Easy DKIM and a custom MAIL FROM, and DMARC is published.",
		"",
		"Volume: " + volume + " messages a day. This mail is delivered by another provider today; we are moving it to SES.",
	}, "\n")
}

// digestDomain is the Follow Digest's sending domain, from its From.
func (w *wizard) digestDomain() string {
	if a, err := mail.ParseAddress(w.digestEmailFrom); err == nil {
		if at := strings.LastIndex(a.Address, "@"); at >= 0 {
			return a.Address[at+1:]
		}
	}
	return w.digestEmailFrom
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// terraform is a Terraform command in the SES root, on the step 2 profile.
func (w *wizard) terraform(args ...string) command {
	return command{
		Name: "terraform",
		Args: append([]string{"-chdir=" + w.terraformDir}, args...),
		Env:  []string{"TF_VAR_aws_profile=" + w.profile, "TF_VAR_aws_region=" + w.region},
	}
}

// sendingDomains are the SES sending identities, read from Terraform's
// ses_dns_records output, sorted.
func (w *wizard) sendingDomains(ctx context.Context) ([]string, error) {
	out, err := w.cmd.Output(ctx, w.terraform("output", "-json", "ses_dns_records"))
	if err != nil {
		return nil, fmt.Errorf("read the SES identities (terraform output ses_dns_records; has step 3's apply run?): %w", err)
	}
	var records map[string]json.RawMessage
	if err := json.Unmarshal(out, &records); err != nil {
		return nil, fmt.Errorf("read terraform output ses_dns_records: %w", err)
	}
	if len(records) == 0 {
		return nil, errors.New("terraform output ses_dns_records lists no SES identity; has step 3's apply run?")
	}
	domains := make([]string, 0, len(records))
	for d := range records {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains, nil
}

// configurationSet is the SES configuration set Terraform created.
func (w *wizard) configurationSet(ctx context.Context) (string, error) {
	out, err := w.cmd.Output(ctx, w.terraform("output", "-raw", "ses_configuration_set_name"))
	if err != nil {
		return "", fmt.Errorf("read the configuration set (terraform output ses_configuration_set_name; has step 3's apply run?): %w", err)
	}
	set := strings.TrimSpace(string(out))
	if set == "" {
		return "", errors.New("terraform output ses_configuration_set_name is empty; has step 3's apply run?")
	}
	return set, nil
}

// awsJSON runs an AWS CLI command on the step 2 profile and region and, when
// into is not nil, decodes its JSON output into it.
func (w *wizard) awsJSON(ctx context.Context, into any, args ...string) error {
	c := command{
		Name: "aws",
		Args: append(args, "--profile", w.profile, "--region", w.region, "--output", "json"),
		Env:  []string{"AWS_PAGER="},
	}
	out, err := w.cmd.Output(ctx, c)
	if err != nil {
		return err
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(out, into); err != nil {
		return fmt.Errorf("aws %s: read its output: %w", strings.Join(args[:2], " "), err)
	}
	return nil
}
