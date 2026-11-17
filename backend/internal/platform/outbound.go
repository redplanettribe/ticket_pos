package platform

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
)

// NewOutboundClient is the one constructor of an HTTP client that calls a
// third party (PayPhone, the SRI, Resend, Google): the standard transport
// behind OutboundTransport, bounded by timeout as the client was before it.
// Every outbound client is built here or by NewOutboundAWSClient, so a call
// to a third party is a span in the request's trace (ADR 0079).
func NewOutboundClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: OutboundTransport(http.DefaultTransport)}
}

// NewOutboundAWSClient is NewOutboundClient for the AWS SDK (SES, STS and the
// S3-compatible storage), given to it through its HTTPClient option: the
// SDK's own default client, timeout included (zero is none, the SDK's
// default), with its transport behind OutboundTransport. The SDK's client is
// kept rather than the standard one because it differs in ways a caller can
// see: it does not follow a redirect that changes the method, and its
// transport sets a minimum TLS version and its own pool sizes.
//
// The client is frozen: the SDK no longer adjusts it for a defaults mode,
// which no deployment sets (AWS_DEFAULTS_MODE unset is "legacy", which
// adjusts nothing).
func NewOutboundAWSClient(timeout time.Duration) *http.Client {
	frozen, ok := awshttp.NewBuildableClient().WithTimeout(timeout).Freeze().(*http.Client)
	if !ok {
		panic(fmt.Sprintf("aws sdk: a frozen buildable client is %T, not *http.Client", frozen))
	}
	frozen.Transport = OutboundTransport(frozen.Transport)
	return frozen
}

// OutboundTransport wraps base, the transport that makes the call, so that a
// call made during a sampled request is a child span of the request span:
// op "http.client", named by the method, scheme, host and path, never the
// query string, fragment, credentials in the URL, headers or body. The span
// records the status the call received, and ends when its response headers
// arrive, or it fails.
//
// Outside a request, in a request the head decided not to trace, and with no
// DSN, it hands the call to base and adds nothing. It never adds a header:
// our trace headers are not propagated to third parties.
func OutboundTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return outboundTransport{base: base}
}

type outboundTransport struct {
	base http.RoundTripper
}

func (t outboundTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_, span := startChildSpan(req.Context(), SpanOptions{Op: "http.client", Name: outboundSpanName(req)})
	if span == nil {
		return t.base.RoundTrip(req)
	}
	defer span.Finish()
	span.SetTag("http.request.method", req.Method)
	span.SetTag("server.address", req.URL.Host)
	resp, err := t.base.RoundTrip(req)
	if err == nil {
		span.SetHTTPStatus(resp.StatusCode)
	}
	return resp, err
}

// outboundSpanName is "<METHOD> <scheme>://<host><path>": the URL rebuilt
// from those three parts alone, so nothing else in it can reach the span.
func outboundSpanName(req *http.Request) string {
	where := url.URL{Scheme: req.URL.Scheme, Host: req.URL.Host, Path: req.URL.Path}
	return req.Method + " " + where.String()
}
