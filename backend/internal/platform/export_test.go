package platform

import (
	"net/http"
	"testing"
)

// FederatedSESSender is newFederatedSESSender for the external tests: an SES
// sender federating through fresh fake metadata, STS and SES endpoints.
func FederatedSESSender(t *testing.T) *SESEmailSender {
	t.Helper()
	sender, _, _, _ := newFederatedSESSender(t, discardLogger())
	return sender
}

// ResendSenderReaching is NewResendEmailSender whose calls reach handler: the
// client the constructor built, its outbound transport included, with only
// the transport under it rewriting Resend's host to the stand-in's.
func ResendSenderReaching(t *testing.T, handler http.Handler) *ResendEmailSender {
	t.Helper()
	stub := newTestSender(t, handler.ServeHTTP)
	sender := NewResendEmailSender("test-key", "Multiticketing <noreply@send.multiticketing.com>", noopLogger{})
	traced, ok := sender.client.Transport.(outboundTransport)
	if !ok {
		t.Fatalf("the Resend client's transport is %T, not the shared outbound transport", sender.client.Transport)
	}
	traced.base = stub.client.Transport
	sender.client.Transport = traced
	return sender
}
