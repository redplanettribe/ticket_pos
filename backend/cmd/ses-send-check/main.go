// Command ses-send-check proves the dormant Amazon SES provider delivers
// (#697, ADR 0077). The Platform Operator runs it from their own machine:
// it sends one fixture message of every type the platform sends, through the
// SES sender, to the one recipient given on the command line, and prints each
// type's outcome. It exits non-zero if any failed.
//
//	aws sso login --profile multiticketing-ses
//	AWS_PROFILE=multiticketing-ses SES_REGION=us-east-1 \
//	SES_CONFIGURATION_SET=prod-ticket-pos \
//	EMAIL_FROM='Multiticketing <noreply@send.multiticketing.com>' \
//	DIGEST_EMAIL_FROM='Multiticketing <digest@digest.multiticketing.com>' \
//	go run ./cmd/ses-send-check -to you@example.com
//
// Transactional messages leave from EMAIL_FROM and the Follow Digest from
// DIGEST_EMAIL_FROM, read as the API reads them. Credentials come from the
// SDK's default chain (an Identity Center profile); AWS_ROLE_ARN is ignored.
// It reads no provider switch and never sends through Resend or falls back to
// the logging sender: SES or nothing. It needs no database and no running API.
// In the SES sandbox the recipient must be a verified address.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// sandboxPace is the default gap between two sends: the SES sandbox allows
// one message per second.
const sandboxPace = time.Second

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, platform.LoadSendCheckConfig))
}

// run is the whole command: parse the flags, load the settings with load,
// build one SES sender per identity and send every fixture. It returns the
// process exit code: 0 when every message was accepted, 1 when any failed,
// 2 when the command could not start.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, load func() (platform.SendCheckConfig, error)) int {
	flags := flag.NewFlagSet("ses-send-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	to := flags.String("to", "", "the recipient of every message (required; in the SES sandbox, a verified address)")
	pace := flags.Duration("pace", sandboxPace, "the gap between two sends (the SES sandbox allows one per second)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	address, err := parseTo(*to, flags.Args())
	if err != nil {
		fmt.Fprintf(stderr, "ses-send-check: %v\n", err)
		flags.Usage()
		return 2
	}

	cfg, err := load()
	if err != nil {
		fmt.Fprintf(stderr, "ses-send-check: %v\n", err)
		return 2
	}
	logger := discardLogger{}
	transactional, err := platform.NewSESEmailSender(ctx, cfg.SES, cfg.TransactionalFrom, logger)
	if err != nil {
		fmt.Fprintf(stderr, "ses-send-check: transactional sender: %v\n", err)
		return 2
	}
	digest, err := platform.NewSESEmailSender(ctx, cfg.SES, cfg.DigestFrom, logger)
	if err != nil {
		fmt.Fprintf(stderr, "ses-send-check: digest sender: %v\n", err)
		return 2
	}
	all, err := messages(address, time.Now(), transactional, digest)
	if err != nil {
		fmt.Fprintf(stderr, "ses-send-check: %v\n", err)
		return 2
	}

	fmt.Fprintf(stdout, "SES %s, configuration set %s\n", cfg.SES.Region, cfg.SES.ConfigurationSet)
	fmt.Fprintf(stdout, "transactional from %s\n", transactional.From())
	fmt.Fprintf(stdout, "digest from        %s\n\n", digest.From())
	if failed := sendAll(ctx, stdout, all, *pace); failed > 0 {
		return 1
	}
	return 0
}

// parseTo takes the one address every message goes to. A missing,
// unparseable or display-named address is refused: there is no default
// recipient to fall back on.
func parseTo(to string, rest []string) (string, error) {
	if len(rest) > 0 {
		return "", fmt.Errorf("unexpected arguments %q; give the recipient with -to", strings.Join(rest, " "))
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return "", errors.New("a recipient is required: -to you@example.com")
	}
	address, err := mail.ParseAddress(to)
	if err != nil || address.Name != "" || address.Address != to {
		return "", fmt.Errorf("-to %q is not a bare email address", to)
	}
	return to, nil
}

// sendAll sends every message in order, pace apart, and prints one line per
// message type: "ok" or "FAIL" with the provider's error, which the SES
// sender has already stripped of the recipient. It returns how many failed.
// A failure never stops the run: the operator needs every type's outcome.
// A cancelled run (ctx done) stops before the next send; the types not tried
// count as failed.
func sendAll(ctx context.Context, out io.Writer, all []message, pace time.Duration) int {
	failed := 0
	for i, m := range all {
		if i > 0 && pace > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(pace):
			}
		}
		if ctx.Err() != nil {
			failed += len(all) - i
			fmt.Fprintf(out, "stopped: %v; %d message types not tried\n", ctx.Err(), len(all)-i)
			break
		}
		if err := m.send(ctx); err != nil {
			failed++
			fmt.Fprintf(out, "FAIL  %-13s  %s  %-31s  %v\n", m.identity, m.locale, m.name, err)
			continue
		}
		fmt.Fprintf(out, "ok    %-13s  %s  %s\n", m.identity, m.locale, m.name)
	}
	fmt.Fprintf(out, "\n%d of %d message types sent, %d failed\n", len(all)-failed, len(all), failed)
	return failed
}

// discardLogger drops the SES sender's failure logs: sendAll prints the same
// error on the message's own line.
type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}
