package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The send check's seam (#697): the whole command, run() with its flags,
// against a fake SES v2 endpoint. The tests assert what reaches SES and what
// the operator reads and gets as an exit code, never how the senders are
// built. The fake mirrors the platform package's SES sender tests.

const (
	checkRecipient     = "operator@example.com"
	checkTransactional = "Multiticketing <noreply@send.multiticketing.com>"
	checkDigest        = "Multiticketing <digest@digest.multiticketing.com>"
)

// fixedNow dates the fixtures a test lists to compare with what was sent;
// only their order, names, identities and locales are compared.
var fixedNow = time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)

type sesRequest struct {
	FromEmailAddress string
	Destination      struct {
		ToAddresses  []string
		CcAddresses  []string
		BccAddresses []string
	}
	ConfigurationSetName string
	Content              struct {
		Simple *struct {
			Subject     struct{ Data string }
			Attachments []struct {
				FileName    string
				ContentType string
				RawContent  []byte
			}
		}
	}
}

type fakeSES struct {
	mu       sync.Mutex
	requests []sesRequest
	reject   bool
}

func (f *fakeSES) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body sesRequest
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, body)
	reject := f.reject
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if reject {
		// The sandbox rejection, which quotes the recipient back.
		w.Header().Set("X-Amzn-ErrorType", "MessageRejected")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Email address is not verified. The following identities failed the check in region US-EAST-1: ` + strings.ToUpper(checkRecipient) + `"}`))
		return
	}
	_, _ = w.Write([]byte(`{"MessageId":"fake-message-id"}`))
}

func (f *fakeSES) sent() []sesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sesRequest(nil), f.requests...)
}

// checkSetup pins the SDK's default chain to static test keys, away from the
// machine's own AWS profile, and returns a fake SES and a loader pointing at
// it, as LoadSendCheckConfig would load with the endpoint overridden.
func checkSetup(t *testing.T) (*fakeSES, func() (platform.SendCheckConfig, error)) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDSENDCHECK")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "send-check-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	fake := &fakeSES{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return fake, func() (platform.SendCheckConfig, error) {
		return platform.SendCheckConfig{
			SES:               platform.SESConfig{Region: "us-east-1", ConfigurationSet: "prod-ticket-pos", Endpoint: srv.URL},
			TransactionalFrom: checkTransactional,
			DigestFrom:        checkDigest,
		}, nil
	}
}

// messageTypes is how many message types the platform sends: every method on
// the transactional EmailSender, the Follow Digest included.
func messageTypes() int {
	return reflect.TypeOf((*platform.EmailSender)(nil)).Elem().NumMethod()
}

// One invocation, one SES request per message type, each to the recipient
// alone through the configuration set; the Follow Digest from the Digest's
// From and everything else from the transactional From. Every type gets its
// own "ok" line and the exit code is 0.
func TestSendCheckSendsOneRequestPerMessageType(t *testing.T) {
	fake, load := checkSetup(t)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-to", checkRecipient, "-pace", "0"}, &stdout, &stderr, load)
	if code != 0 {
		t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	sent := fake.sent()
	if len(sent) != messageTypes() {
		t.Fatalf("fake SES saw %d requests, want one per message type (%d)", len(sent), messageTypes())
	}
	all, err := messages(checkRecipient, fixedNow, nil, nil)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	names := map[string]bool{}
	locales := map[platform.Locale]bool{}
	for i, m := range all {
		if names[m.name] {
			t.Fatalf("message type %q is sent twice", m.name)
		}
		names[m.name] = true
		locales[m.locale] = true

		req := sent[i]
		wantFrom := checkTransactional
		if m.identity == digestIdentity {
			wantFrom = checkDigest
		}
		if req.FromEmailAddress != wantFrom {
			t.Errorf("%s: From = %q, want %q", m.name, req.FromEmailAddress, wantFrom)
		}
		if got := req.Destination.ToAddresses; len(got) != 1 || got[0] != checkRecipient || len(req.Destination.CcAddresses)+len(req.Destination.BccAddresses) != 0 {
			t.Errorf("%s: destination = %+v, want %s alone", m.name, req.Destination, checkRecipient)
		}
		if req.ConfigurationSetName != "prod-ticket-pos" {
			t.Errorf("%s: configuration set = %q", m.name, req.ConfigurationSetName)
		}
		if !strings.Contains(stdout.String(), "ok    "+string(m.identity)) || !strings.Contains(stdout.String(), m.name+"\n") {
			t.Errorf("%s: no ok line in output:\n%s", m.name, stdout.String())
		}
	}
	if digests := countFrom(sent, checkDigest); digests != 1 {
		t.Errorf("%d requests from the Digest's From, want 1 (the Follow Digest)", digests)
	}
	if !locales[platform.LocaleES] || !locales[platform.LocaleEN] {
		t.Errorf("fixtures use locales %v, want both Mail Locales", locales)
	}
	if summary := fmt.Sprintf("%d of %d message types sent, 0 failed", messageTypes(), messageTypes()); !strings.Contains(stdout.String(), summary) {
		t.Errorf("no summary line in output:\n%s", stdout.String())
	}
}

func countFrom(sent []sesRequest, from string) int {
	n := 0
	for _, req := range sent {
		if req.FromEmailAddress == from {
			n++
		}
	}
	return n
}

// The Tax Document carries the two files the delivery step attaches, and
// both open: the factura XML parses with a <factura> root, and the RIDE is a
// whole PDF. No other message carries an attachment.
func TestSendCheckTaxDocumentCarriesAttachmentsThatOpen(t *testing.T) {
	fake, load := checkSetup(t)
	if code := run(context.Background(), []string{"-to", checkRecipient, "-pace", "0"}, io.Discard, io.Discard, load); code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	all, _ := messages(checkRecipient, fixedNow, nil, nil)
	for i, req := range fake.sent() {
		attachments := req.Content.Simple.Attachments
		if all[i].name != "tax document delivery" {
			if len(attachments) != 0 {
				t.Errorf("%s carries %d attachments, want none", all[i].name, len(attachments))
			}
			continue
		}
		if len(attachments) != 2 {
			t.Fatalf("tax document delivery carries %d attachments, want 2", len(attachments))
		}
		xmlFile, pdfFile := attachments[0], attachments[1]
		if !strings.HasSuffix(xmlFile.FileName, ".xml") || !strings.HasPrefix(xmlFile.ContentType, "application/xml") {
			t.Errorf("first attachment = %s (%s), want the factura XML", xmlFile.FileName, xmlFile.ContentType)
		}
		var root struct{ XMLName xml.Name }
		if err := xml.Unmarshal(xmlFile.RawContent, &root); err != nil || root.XMLName.Local != "factura" {
			t.Errorf("factura XML does not parse to <factura>: root %q, err %v", root.XMLName.Local, err)
		}
		if strings.TrimSuffix(xmlFile.FileName, ".xml") != strings.TrimSuffix(pdfFile.FileName, ".pdf") {
			t.Errorf("attachments %s and %s are not one document's", xmlFile.FileName, pdfFile.FileName)
		}
		if pdfFile.ContentType != "application/pdf" || !bytes.HasPrefix(pdfFile.RawContent, []byte("%PDF-")) ||
			!bytes.Contains(pdfFile.RawContent[len(pdfFile.RawContent)-32:], []byte("%%EOF")) {
			t.Errorf("RIDE %s (%s, %d bytes) is not a whole PDF", pdfFile.FileName, pdfFile.ContentType, len(pdfFile.RawContent))
		}
	}
}

// A failure on every send is reported per type and the run goes on to the
// end; the exit code is 1. The recipient SES quotes back is redacted.
func TestSendCheckReportsEveryFailureAndExitsNonZero(t *testing.T) {
	fake, load := checkSetup(t)
	fake.reject = true
	var stdout bytes.Buffer

	code := run(context.Background(), []string{"-to", checkRecipient, "-pace", "0"}, &stdout, io.Discard, load)
	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if got := len(fake.sent()); got != messageTypes() {
		t.Fatalf("fake SES saw %d requests, want every type tried (%d)", got, messageTypes())
	}
	if got := strings.Count(stdout.String(), "FAIL  "); got != messageTypes() {
		t.Fatalf("%d FAIL lines, want %d:\n%s", got, messageTypes(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "MessageRejected") && !strings.Contains(stdout.String(), "not verified") {
		t.Errorf("output does not carry the provider's error:\n%s", stdout.String())
	}
	if strings.Contains(strings.ToLower(stdout.String()), checkRecipient) {
		t.Errorf("output names the recipient:\n%s", stdout.String())
	}
}

// No recipient, no run: nothing is loaded and nothing sent, exit code 2.
// There is no default recipient and no other sender to fall back on.
func TestSendCheckRefusesToRunWithoutARecipient(t *testing.T) {
	for name, args := range map[string][]string{
		"none":         {},
		"empty":        {"-to", " "},
		"not an email": {"-to", "operator"},
		"display name": {"-to", "Operator <operator@example.com>"},
		"positional":   {checkRecipient},
		"unknown flag": {"-recipient", checkRecipient},
	} {
		t.Run(name, func(t *testing.T) {
			fake, load := checkSetup(t)
			loaded := false
			var stderr bytes.Buffer

			code := run(context.Background(), args, io.Discard, &stderr, func() (platform.SendCheckConfig, error) {
				loaded = true
				return load()
			})
			if code != 2 {
				t.Fatalf("exit code %d, want 2", code)
			}
			if loaded || len(fake.sent()) != 0 {
				t.Fatalf("loaded = %v, requests = %d; want neither", loaded, len(fake.sent()))
			}
			if !strings.Contains(stderr.String(), "-to") {
				t.Errorf("stderr does not say how to give the recipient:\n%s", stderr.String())
			}
		})
	}
}

// Settings the check cannot load stop it before anything is sent: it never
// falls back to another sender.
func TestSendCheckRefusesToRunWithoutItsSettings(t *testing.T) {
	fake, _ := checkSetup(t)
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"-to", checkRecipient}, io.Discard, &stderr, func() (platform.SendCheckConfig, error) {
		return platform.SendCheckConfig{}, errors.New("the SES send check needs SES settings, but SES_REGION is not set")
	})
	if code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
	if len(fake.sent()) != 0 {
		t.Fatalf("fake SES saw %d requests, want none", len(fake.sent()))
	}
	if !strings.Contains(stderr.String(), "SES_REGION") {
		t.Errorf("stderr does not name the missing setting:\n%s", stderr.String())
	}
}
