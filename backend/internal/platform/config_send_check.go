package platform

import (
	"fmt"
	"os"
	"strings"
)

// defaultEmailFrom is the transactional From a deployment that sets no
// EMAIL_FROM sends as, the API and the SES send check alike.
const defaultEmailFrom = "Multiticketing <noreply@send.multiticketing.com>"

// SendCheckConfig is what the SES send check (cmd/ses-send-check, #697) needs:
// the SES settings both identities share, and the From of each identity. It
// is read from the same variables the API reads, so the check proves the
// settings the API would send with.
type SendCheckConfig struct {
	SES               SESConfig
	TransactionalFrom string
	DigestFrom        string
}

// LoadSendCheckConfig reads the send check's settings. It needs no database,
// reads no provider switch and no Resend key: the check sends through SES
// whatever the switches say, which is the point of proving a dormant provider.
//
// The region and configuration set are required. The role to assume is never
// read: the check runs on the Platform Operator's own machine through the
// SDK's default credential chain (an Identity Center profile), and a role
// left in the environment would send it looking for a GCP metadata server
// that is not there.
//
// The Digest identity is held to the rules the API holds it to on SES: it
// needs its own From, on a domain of its own unless sharing the
// transactional one is declared (ADR 0030).
func LoadSendCheckConfig() (SendCheckConfig, error) {
	cfg := SendCheckConfig{
		SES: SESConfig{
			Region:           strings.TrimSpace(os.Getenv("SES_REGION")),
			ConfigurationSet: strings.TrimSpace(os.Getenv("SES_CONFIGURATION_SET")),
		},
		TransactionalFrom: envOrDefault("EMAIL_FROM", defaultEmailFrom),
		DigestFrom:        strings.TrimSpace(os.Getenv("DIGEST_EMAIL_FROM")),
	}
	var missing []string
	if cfg.SES.Region == "" {
		missing = append(missing, "SES_REGION")
	}
	if cfg.SES.ConfigurationSet == "" {
		missing = append(missing, "SES_CONFIGURATION_SET")
	}
	if len(missing) > 0 {
		return SendCheckConfig{}, fmt.Errorf("the SES send check needs SES settings, but %s not set", describeMissing(missing))
	}
	if cfg.DigestFrom == "" {
		return SendCheckConfig{}, fmt.Errorf("the SES send check needs DIGEST_EMAIL_FROM, the Follow Digest identity's From")
	}
	digest := DigestEmailConfig{
		Provider:                 EmailProviderSES,
		From:                     cfg.DigestFrom,
		AllowSharedSendingDomain: envIsTrue("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN"),
	}
	if reason := digest.UnconfiguredReason(cfg.TransactionalFrom); reason != "" {
		return SendCheckConfig{}, fmt.Errorf("the Follow Digest identity cannot send: %s", reason)
	}
	return cfg, nil
}
