package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errStopped is the operator choosing to stop, or their input ending, before
// a step was done.
var errStopped = errors.New("stopped")

// A step of the setup. Its run says what to do, waits for the operator, and
// checks what can be checked; it returns errStopped when the operator stops.
type step struct {
	title string
	run   func(w *wizard, ctx context.Context) error
}

// The steps, in the order they must happen. A step's number is its index + 1.
var steps = []step{
	{"Create the AWS Organization and the platform's member account", (*wizard).organization},
	{"Set up IAM Identity Center and a local SSO profile", (*wizard).identityCenter},
	{"Apply Terraform", (*wizard).terraformApply},
	{"Add the SES DNS records at Namecheap", (*wizard).dnsRecords},
	{"Confirm both SES sending identities are verified", (*wizard).identitiesVerified},
	{"Verify a recipient in the sandbox and run the SES send check", (*wizard).sendCheck},
	{"Request SES production access", (*wizard).productionAccess},
}

type wizard struct {
	in     *bufio.Reader
	out    io.Writer
	cmd    commander
	getenv func(string) string

	profile, region, account string
	terraformDir             string
	emailFrom                string
	digestEmailFrom          string
}

// walk runs the steps from the from-th to the last, stopping at the first
// one the operator does not finish.
func (w *wizard) walk(ctx context.Context, from int) int {
	w.printf("Amazon SES setup (ADR 0077): %d steps, starting at step %d.\n", len(steps), from)
	w.printf("This wizard writes no file and switches no sending identity.\n")
	for i := from - 1; i < len(steps); i++ {
		n := i + 1
		w.printf("\n== Step %d of %d: %s ==\n\n", n, len(steps), steps[i].title)
		if err := steps[i].run(w, ctx); err != nil {
			if !errors.Is(err, errStopped) {
				w.printf("\nStep %d failed: %v\n", n, err)
			}
			w.printf("\nStopped at step %d. Resume with: make ses-setup-wizard ARGS='-from-step %d'\n", n, n)
			return 1
		}
		w.printf("\nStep %d done.\n", n)
	}
	w.nothingSwitched()
	return 0
}

func (w *wizard) printf(format string, a ...any) {
	fmt.Fprintf(w.out, format, a...)
}

// ask prints prompt and reads one line. ok is false when input has ended.
func (w *wizard) ask(prompt string) (answer string, ok bool) {
	w.printf("%s ", prompt)
	line, err := w.in.ReadString('\n')
	if err != nil && line == "" {
		w.printf("\n")
		return "", false
	}
	return strings.TrimSpace(line), true
}

// confirm waits for the operator to say a step they perform by hand is
// done. q, or the end of input, stops the wizard.
func (w *wizard) confirm(what string) error {
	answer, ok := w.ask(fmt.Sprintf("Press Enter when %s (q to stop here):", what))
	if !ok || strings.EqualFold(answer, "q") {
		return errStopped
	}
	return nil
}

// yes asks a yes/no question whose answer defaults to no.
func (w *wizard) yes(question string) bool {
	answer, ok := w.ask(question + " [y/N]")
	return ok && (strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"))
}

// until runs check until it passes, telling the operator why each time it
// does not, and refuses to move on: the only way past a failing check is
// to stop (q, or the end of input) and resume later.
func (w *wizard) until(what string, check func() error) error {
	for {
		w.printf("Checking that %s...\n", what)
		err := check()
		if err == nil {
			w.printf("  ok: %s\n", what)
			return nil
		}
		w.printf("  NOT YET: %v\n", err)
		answer, ok := w.ask("Fix it, then press Enter to check again (q to stop here):")
		if !ok || strings.EqualFold(answer, "q") {
			return errStopped
		}
	}
}

// nothingSwitched is the last word: every step is done and Resend still
// carries every message.
func (w *wizard) nothingSwitched() {
	w.printf(`
All %d steps are done. Nothing has switched.

Resend still carries all traffic. Both provider variables in
terraform/envs/prod are still "resend"; this wizard never changes them.
Switching is one variable per sending identity, set to "ses" and applied:

  transactional mail   email_provider = "ses"          (EMAIL_PROVIDER on the API)
  Follow Digest        digest_email_provider = "ses"   (DIGEST_EMAIL_PROVIDER on the API)

Setting one back to "resend" and applying is the rollback. Switch only once
AWS has granted production access (step 7).
`, len(steps))
}
