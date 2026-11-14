package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Workload Identity Federation from Cloud Run to AWS (ADR 0077). The API on
// Cloud Run holds no AWS key: it asks the GCP metadata server for a
// Google-signed ID token naming its own service account, and trades it at STS
// with AssumeRoleWithWebIdentity for short-lived credentials of the one role
// Terraform lets that service account assume.

// GCPMetadataEndpoint is the metadata server every Cloud Run instance reaches
// for its service account's tokens. SESConfig.MetadataEndpoint overrides it in
// tests only.
const GCPMetadataEndpoint = "http://metadata.google.internal"

// webIdentityRoleSessionName names the STS session, so AWS CloudTrail says
// which workload assumed the role.
const webIdentityRoleSessionName = "ticket-pos-api-ses"

// webIdentityExpiryWindow is how long before they expire the cached
// credentials are replaced. STS issues them for an hour; refreshing five
// minutes early means a send never signs with credentials that lapse while
// its request, retries included, is still in flight.
const webIdentityExpiryWindow = 5 * time.Minute

// metadataTimeout bounds one ID-token fetch. The metadata server is local to
// the instance and answers in milliseconds; one that does not answer is down.
const metadataTimeout = 5 * time.Second

// webIdentityCredentials is the aws.CredentialsProvider behind the
// federation: each Retrieve fetches a fresh ID token and exchanges it. It is
// always used through an aws.CredentialsCache, which reuses the credentials
// across sends and calls Retrieve again only inside the expiry window.
type webIdentityCredentials struct {
	roleARN     string
	audience    string
	metadataURL string
	metadata    *http.Client
	sts         *sts.Client
}

// newWebIdentityCredentials builds the cached federation credentials for
// roleARN, with ID tokens minted for audience. stsOptions are the STS
// client's options (region, HTTP client, retryer, endpoint override);
// AssumeRoleWithWebIdentity is unsigned, so the client is given no
// credentials of its own. Nothing reaches the metadata server or STS until
// the first send.
func newWebIdentityCredentials(settings SESConfig, stsOptions sts.Options) *aws.CredentialsCache {
	metadataEndpoint := settings.MetadataEndpoint
	if metadataEndpoint == "" {
		metadataEndpoint = GCPMetadataEndpoint
	}
	stsOptions.Credentials = aws.AnonymousCredentials{}
	if settings.STSEndpoint != "" {
		stsOptions.BaseEndpoint = aws.String(settings.STSEndpoint)
	}
	provider := &webIdentityCredentials{
		roleARN:  settings.RoleARN,
		audience: settings.WebIdentityAudience,
		metadataURL: strings.TrimRight(metadataEndpoint, "/") +
			"/computeMetadata/v1/instance/service-accounts/default/identity?audience=" +
			url.QueryEscape(settings.WebIdentityAudience),
		metadata: &http.Client{Timeout: metadataTimeout},
		sts:      sts.New(stsOptions),
	}
	return aws.NewCredentialsCache(provider, func(o *aws.CredentialsCacheOptions) {
		o.ExpiryWindow = webIdentityExpiryWindow
	})
}

// Retrieve fetches an ID token and exchanges it for the role's credentials.
func (p *webIdentityCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	token, err := p.idToken(ctx)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("fetch gcp metadata id token: %w", err)
	}
	out, err := p.sts.AssumeRoleWithWebIdentity(ctx, &sts.AssumeRoleWithWebIdentityInput{
		RoleArn:          aws.String(p.roleARN),
		RoleSessionName:  aws.String(webIdentityRoleSessionName),
		WebIdentityToken: aws.String(token),
	})
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("assume role with web identity: %w", err)
	}
	c := out.Credentials
	if c == nil || c.AccessKeyId == nil || c.SecretAccessKey == nil || c.SessionToken == nil || c.Expiration == nil {
		return aws.Credentials{}, fmt.Errorf("assume role with web identity: response carries incomplete credentials")
	}
	return aws.Credentials{
		AccessKeyID:     *c.AccessKeyId,
		SecretAccessKey: *c.SecretAccessKey,
		SessionToken:    *c.SessionToken,
		Source:          "GCPWebIdentity",
		CanExpire:       true,
		Expires:         *c.Expiration,
	}, nil
}

// idToken asks the metadata server for a Google-signed ID token for the
// instance's service account, minted for the configured audience.
func (p *webIdentityCredentials) idToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.metadataURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := p.metadata.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// A token is about a kilobyte; anything past 16 KiB is not one.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 200 {
			detail = detail[:200]
		}
		return "", fmt.Errorf("metadata server answered %d: %s", resp.StatusCode, detail)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("metadata server answered with an empty token")
	}
	return token, nil
}
