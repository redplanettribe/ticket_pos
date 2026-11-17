# Sentry reports errors, traces and job check-ins behind one seam, holding no personal data, and dormant until a DSN is set

Ends the deferral in `docs/technical-design.md`, which put metrics, distributed tracing and error reporting off "until production hosting is chosen".
Hosting was chosen in [ADR 0007](./0007-gcp-cloud-run-and-cloud-sql.md), and nothing replaced the deferral.

## Context

The platform runs in production with structured logs and a request id, and nothing else.
An unexpected error becomes a generic 500 whose cause is never written down, a recovered panic is logged without its stack, and a scheduled job that never runs leaves no trace at all.
That last failure is the one this system has actually had: the Follow Digest that never sent, and the first production invoice that sat pending.

Sentry is chosen because an account already exists.
It is a new third party in the United States, and the errors of a ticketing system naturally carry buyer emails, names, identity numbers and invoice data.
The deletion protocol for personal data is unsettled and with counsel, so anything Sentry holds about a person would become part of that question.

## Decision

**What is reported.**
Error monitoring, sampled tracing, a check-in from every scheduled job, and uptime checks.
Not session replay, which records screens full of buyer data.
Not profiling, until a trace shows a reason.
Not logs: Cloud Logging stays the log store, and the request id on every Sentry event is the join back to it.

**One Sentry project per deployable.**
`ticket-pos-api`, `ticket-pos-staff` and `ticket-pos-storefront`, under a `ticket-pos` team in the existing organization, which is shared with another product.
The Storefront is public and collects noise that must not share an inbox or a rate limit with the API.

**No personal data leaves the platform.**
Default PII collection is off: no request bodies, cookies, headers or IP addresses.
A person is identified only by an opaque internal id, and context travels as ids only: Organization, Event, Sale, route pattern, request id, job name, and whether the actor is staff or a customer.
Events are scrubbed in our own process before they are sent, and Sentry's server-side scrubbing is a second net, never the first.
Spans carry SQL statement text without arguments and outbound URLs without query strings.
An opaque id is not something an erasure has to reach, which is the reason for the rule.

**Dormant until a DSN is set.**
With no DSN the SDK does nothing, in every deployable.
The code ships dark, and the operator sets the DSNs after publishing a policy edition that names Sentry, the rule [ADR 0045](./0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md) set for ticket questions.
Only production reports.
A local stack, CI and unattended agent runs carry no DSN, which matters because a local database can hold a copy of production data.

**One seam in the backend.**
`sentry-go` is used directly, behind a single seam in `platform`, and no other package imports it.
The vendor is swappable at that one place.
Reporting happens at the two points every module already passes through: the mapping of an error to a response, and the `Logger` implementation.
A panic, a non-domain error that becomes a 500, and anything logged at Error are reported.
A domain error answered with a 4xx is the platform working, and is not.
A Warn is not, so a capacity refusal stays out.
One failed request produces one event: the request log line for a 5xx does not report a second time what the error mapping already reported.

**Reporting never costs a request.**
Delivery is asynchronous and off the request path, and a slow or unreachable Sentry can neither delay nor fail a request or a job.
A job flushes with a bound of two seconds before it answers.
Traces are sampled: one request in ten, every checkout, payment and SRI invoicing request, and no health checks or static assets.

**Every job checks in, and a monitor exists only for a job that runs.**
Each scheduled job's handler reports that it started and how it ended.
The monitor that expects those check-ins is declared in Terraform beside the Cloud Scheduler job and exists only while that job is unpaused, so a schedule cannot drift from its monitor and a paused job cannot alert forever.

**Browser events go through our own origin.**
Each Next app relays browser events through a route of its own.
Ad blockers drop requests to Sentry's hosts, and Storefront buyers are who runs them.
The relay also means a buyer's IP address never reaches Sentry.
The Storefront loads the SDK after hydration; the Staff app loads it normally.

**A release is a commit.**
The commit SHA identifies the release in all three deployables.
Source maps are uploaded during the build and removed from the image, so they are never served.
If Sentry is unreachable the deploy proceeds, and that release's browser stack traces stay minified.

**Sentry is declared in Terraform.**
Projects, keys, alert rules, inbound filters and monitors live in the `ticket-pos` module, applied by hand like the rest.
The CLI is used only for what happens per build.

## Considered options

- **OpenTelemetry exporting to Sentry.**
  Rejected: errors and check-ins would still need `sentry-go`, so it adds a second system and several dependencies to gain a neutrality the seam already gives.
- **One Sentry project for everything, split by tag.**
  Rejected: one rate limit and one alert stream for a public site and the API.
- **The buyer's email on the event, to search by person.**
  Rejected: the internal id finds the same person in the Customer Dossier, and an email in Sentry is personal data held by a processor we would then have to erase from.
- **Browser events sent straight to Sentry.**
  Rejected for the ad-blocker loss and the IP address.
- **Check-ins only from jobs that have a monitor.**
  Rejected: the handler would have to know what is provisioned.
  A check-in nobody expects is dropped.
- **Provisioning with one-off CLI calls.**
  Rejected: state nobody can read or repeat.

## Consequences

- The Storefront gives up page-load traces and Web Vitals from real buyers.
  Buyer page speed outranks measuring it, and the server side of the same requests is still traced.
- "Never reported" now has two meanings: nothing went wrong, or no DSN is set.
  Until the operator sets the DSNs, production is exactly as blind as before.
- A state that is stuck without failing, such as an invoice pending for hours, is still invisible.
  The only bridge is the rule that a sweep which gives up on something logs it at Error.
- Promoting a log line to Error now pages someone, and demoting one silences it.
  The log level has become an alerting decision.
- Monitoring nine jobs needs a paid Sentry plan.
  On the free plan one monitor exists and the other jobs are covered only when they fail, not when they never run.
- The image build now talks to Sentry, and a Sentry token lives in GitHub Actions and in Terraform's inputs.
- Each Next app gains error screens it did not have, showing the request id as a reference a person can quote.

## Notes

- **2026-11-16: the Storefront has no per-key rate limit (#783).**
  The organization is on Sentry's Team plan, which lacks per-key rate limits, and Sentry refused to create a Storefront key that set one.
  The limit is dropped rather than the plan raised, so the Decision's "must not share an inbox or a rate limit with the API" now holds for the inbox only: every project draws on the one organization quota.
  The Storefront's noise is bounded only by its own before-send filtering, its three inbound data filters, the organization's error quota and Sentry's spike protection.
  The separate project still gives the Storefront its own inbox and its own alert rules.
- **2026-11-17: the Storefront starts each client navigation's trace itself (#792).**
  The SDK's Next navigation tracing is off in the Storefront's browser (`instrumentNavigation: false`), and the handler the Storefront hands Next's `onRouterTransitionStart` starts every navigation with `startBrowserTracingNavigationSpan`: a `<Link>` click, `router.push` and `router.replace`, and back or forward, which Next reports from its own popstate listener.
  The SDK's tracing also listened to popstate, so each back or forward started two navigations, and the next navigation Next reported only renamed the second of them: it started no trace, and its requests carried the trace and the sampling decision of the navigation before it.
  Each navigation now starts a trace of its own with its own one in ten decision.
  Requests a page makes after its navigation has ended and before the next one (prefetches, API calls) still join the trace of the navigation that brought it, as the SDK does: they are that page's work, and the trace ends at the next navigation, so it never spans a session.
- **2026-11-17: the Storefront has five inbound data filters, not three (#798, #805).**
  The note of 2026-11-16 counts three, the ones the module declares: browser extensions, web crawlers and legacy browsers.
  Sentry also turns on `react-hydration-errors` and `chunk-load-error` by default for a `javascript-nextjs` project, and both are on for `ticket-pos-storefront`; a React hydration error was found dropped at intake by the first.
  Both stay on.
  The browser SDK loads after hydration, so a real first-load hydration error mostly never reaches Sentry anyway and what does arrive is mostly extension noise, and a chunk load error after a deploy is expected noise.
  The Terraform provider cannot declare either, so they are left at Sentry's defaults, written down in `sentry.tf` and on the operator's checklist in `terraform/README.md`.
