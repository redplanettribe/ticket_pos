# Amazon SES stands beside Resend, dormant behind an explicit provider switch

Amends [ADR 0009](./0009-transactional-email-via-resend.md): the email provider is no longer selected by the presence of a Resend key, and Resend is no longer the only provider the binary can send through.
Leaves [ADR 0030](./0030-follows-notify-as-a-weekly-digest.md)'s two sending identities exactly as they stand: SES is a second provider for both, not a third identity.

## Context

ADR 0009 chose Resend over SES for cost and setup at the time, and called the choice cheap to reverse because it is one implementation of `EmailSender`.
We now want SES ready to take over, without taking over yet.
Resend keeps carrying every message until a deliberate switch day, and nothing is removed.

Two things in the current wiring stop a second provider from simply being added.
The provider is selected by credential presence (`RESEND_API_KEY` set means Resend), so any deployment that happened to carry AWS configuration would start sending through SES by accident.
And the platform runs on Cloud Run in GCP, so SES is reached across clouds and needs a way to authenticate to AWS.

## Decision

**An explicit switch per sending identity.**
`EMAIL_PROVIDER` selects the transactional sender and `DIGEST_EMAIL_PROVIDER` the Follow Digest's, each `resend` or `ses`, each defaulting to `resend`.
SES configuration is read and validated only when SES is selected.
In production, selecting `ses` with incomplete SES configuration fails startup rather than falling back to another provider.
A deployment with no provider credentials at all keeps the logging sender, as today.
"Dormant" therefore means the code is merged and the AWS resources are applied, and switching (or rolling back) is one Terraform variable.

**Same sending domains, verified in both providers at once.**
SES verifies `send.multiticketing.com` and `digest.multiticketing.com` with Easy DKIM beside Resend's records, on a custom MAIL FROM name that does not collide with Resend's.
The switch changes neither a From address nor a DNS record.
DNS stays at Namecheap and is added by hand (ADR 0007); Terraform outputs the records as a checklist.

**Workload Identity Federation, no stored AWS keys.**
The API's Cloud Run service account presents a Google-signed OIDC token to `AssumeRoleWithWebIdentity` for an IAM role that trusts exactly that service account's unique ID, and may only call `ses:SendEmail` on the two identities.
Outside Cloud Run (a developer's machine running the send check) the AWS SDK's default credential chain is used instead, i.e. an Identity Center profile.
In production, selecting `ses` without a role to assume is a startup failure, so a Cloud Run service never silently picks up some other credential.

**A dedicated AWS member account.**
The personal AWS account becomes an Organization's management account and does nothing else.
SES runs in a member account of its own, in `us-east-1`, reached by humans through IAM Identity Center.
SES reputation, sandbox status, quotas and suspension are per account and region, so this keeps an SES review or a runaway bill from landing on anything else.

**One Terraform root and state.**
The `aws` provider joins `envs/prod`, the SES resources live in the `ticket-pos` module beside the Resend ones, and the state stays in the existing GCS bucket.
The role's trust policy references the API service account directly, so the two cannot drift.
Terraform is still applied by hand; CI does not apply it.

**Bounces and complaints: account-level suppression only.**
SES suppresses bounced and complaining addresses at the account level and sends through a configuration set.
There is no event pipeline into our database, as there is none for Resend today.

**The AWS SDK, not a raw POST.**
ADR 0009's reason for a hand-written `net/http` call, that Resend is one authenticated POST, does not carry over: SES needs SigV4 signing and a credential exchange, which are not code to write by hand.
The SES sender uses `aws-sdk-go-v2`.

## Considered options

- **Static IAM access keys in Secret Manager**, the same shape as the Resend key.
  Rejected: a long-lived credential to leak and rotate, where federation needs none and lives entirely in Terraform.
- **Keeping presence-based selection and simply not mounting the AWS settings.**
  Rejected: "inactive" would then depend on something being absent, which is exactly how a provider gets turned on by accident.
- **New sending subdomains for SES.**
  Rejected: the switch would then change the From address and need fresh reputation on day one.
- **SES in the personal root account.**
  Rejected for the isolation reasons above; an Organization and Identity Center cost nothing.

## Consequences

- Sweep pacing stays tuned to Resend's rate limit.
  That is slower than SES allows in production, which is safe; retuning belongs to the switch, not to dormant code.
- A new SES account starts in the sandbox, and production access is a manual request AWS can refuse.
  It is requested while Resend still carries the load, so a refusal is learned early.
- Until the switch, nothing exercises SES in production traffic.
  A send check run by an operator against the real account is the only end-to-end evidence that the dormant provider delivers.
