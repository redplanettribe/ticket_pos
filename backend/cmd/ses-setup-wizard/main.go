// Command ses-setup-wizard walks the Platform Operator through the steps of
// the Amazon SES setup only a human can perform (#698, ADR 0077), in the
// order they must happen:
//
//  1. the AWS Organization and the platform's member account;
//  2. IAM Identity Center and a local SSO profile for that account;
//  3. the Terraform apply;
//  4. the DNS records at Namecheap;
//  5. verification of both SES sending identities;
//  6. a sandbox-verified recipient and the SES send check;
//  7. the request for SES production access.
//
// Each step says what to do and how to tell it worked. Where the AWS CLI or
// Terraform can tell, the wizard checks, and it refuses to move on until the
// check passes or the operator stops. -from-step resumes at a given step.
//
//	make ses-setup-wizard
//	make ses-setup-wizard ARGS='-from-step 5'
//
// It writes no file. The budget alert address is read from the environment
// (TF_VAR_aws_budget_alert_email) and passed on to Terraform unread; the
// send check's recipient is typed at a prompt and passed to the AWS CLI and
// the send check only. Nothing it does switches a sending identity: both
// provider variables stay "resend".
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// Defaults that match envs/prod: the aws_profile and aws_region variables'
// defaults, and the From headers the API is deployed with (email_from and
// the Digest From derived from digest_email_domain).
const (
	defaultProfile         = "multiticketing-ses"
	defaultRegion          = "us-east-1"
	defaultTerraformDir    = "../terraform/envs/prod"
	defaultEmailFrom       = "Multiticketing <noreply@send.multiticketing.com>"
	defaultDigestEmailFrom = "Multiticketing <digest@digest.multiticketing.com>"
	defaultWebsiteURL      = "https://discover.multiticketing.com"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, execCommander{}))
}

// run is the whole command: parse the flags and walk the steps from the
// chosen one to the end. It returns the process exit code: 0 when the last
// step is done, 1 when the operator stopped (or a check never passed), 2
// when the flags are wrong.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, cmd commander) int {
	flags := flag.NewFlagSet("ses-setup-wizard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fromStep := flags.Int("from-step", 1, fmt.Sprintf("the step to start at, 1 to %d, to resume an earlier run", len(steps)))
	profile := flags.String("profile", defaultProfile, "the IAM Identity Center profile for the SES member account (Terraform's aws_profile)")
	region := flags.String("region", defaultRegion, "the AWS region SES runs in (Terraform's aws_region)")
	account := flags.String("account", "", "the SES member account's ID; when set, step 2 also checks the profile reaches exactly this account")
	terraformDir := flags.String("terraform-dir", defaultTerraformDir, "the Terraform root that manages SES")
	emailFrom := flags.String("email-from", orDefault(getenv("EMAIL_FROM"), defaultEmailFrom), "the transactional From the send check sends as (EMAIL_FROM)")
	digestEmailFrom := flags.String("digest-email-from", orDefault(getenv("DIGEST_EMAIL_FROM"), defaultDigestEmailFrom), "the Follow Digest From the send check sends as (DIGEST_EMAIL_FROM)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "ses-setup-wizard: unexpected arguments %q\n", flags.Args())
		flags.Usage()
		return 2
	}
	if *fromStep < 1 || *fromStep > len(steps) {
		fmt.Fprintf(stderr, "ses-setup-wizard: -from-step %d: there are steps 1 to %d\n", *fromStep, len(steps))
		return 2
	}

	w := &wizard{
		in:              bufio.NewReader(stdin),
		out:             stdout,
		cmd:             cmd,
		getenv:          getenv,
		profile:         *profile,
		region:          *region,
		account:         *account,
		terraformDir:    *terraformDir,
		emailFrom:       *emailFrom,
		digestEmailFrom: *digestEmailFrom,
	}
	return w.walk(ctx, *fromStep)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
