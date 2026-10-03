# Security policy

## Supported versions

Alcubi Delegator is in pre-1.0 development. Published releases are available,
but no version is considered stable or production-ready.

| Version | Security support |
| --- | --- |
| Latest published release | Best-effort security fixes |
| Older public releases | No security fixes; upgrade to the latest release |
| `main` and other development builds | No release support; fixes are made on a best-effort basis |

When pre-1.0 releases begin, only the latest published release will be eligible
for security fixes unless this table says otherwise. Pre-1.0 fixes can require
upgrading to a release that contains breaking changes. This policy and the
table will be updated when the first release is published.

## Report a vulnerability privately

Send a suspected vulnerability to
[support@alcubi.ai](mailto:support@alcubi.ai) with `[SECURITY]` in the subject.
This private mailbox is monitored by Matthew McCormick, Delegator's sole
maintainer.

Do not open a public GitHub issue, discussion, or pull request containing a
vulnerability, credential, exploit, or private data. If the report must refer
to a live secret or user data, do not copy that material into the message.
Describe its type and potential exposure, revoke or rotate a secret when that
is safe to do, and ask how to provide any evidence that cannot safely travel
by ordinary email.

A useful report includes:

- the affected Delegator version from `dg version`, or the commit hash;
- the operating system, architecture, and other relevant platform details;
- the configuration and third-party agent or integration involved;
- clear reproduction steps or a minimal proof of concept;
- the observed result, expected result, and security impact;
- whether credentials, secrets, source code, tickets, logs, or other user data
  may have been exposed; and
- any known mitigations, disclosure deadline, or prior public disclosure.

Remove unrelated repository content, personal data, credentials, and secrets
from screenshots, logs, and examples. A report can be made without testing a
live system.

## What happens after a report

The maintainer aims to acknowledge a report within 7 calendar days and to send
an initial assessment or request for more information within the following 14
calendar days. If a confirmed report remains unresolved, the maintainer aims
to send an update at meaningful changes and at least once every 30 calendar
days while work continues. If no acknowledgement arrives, resend the message
and confirm that the address was entered correctly.

These are targets for a solo maintainer, not a service-level agreement or a
guaranteed response or remediation time. Validation, a fix, a release, or an
advisory can take longer based on severity, complexity, upstream coordination,
and release readiness. The maintainer will say when a report is a duplicate,
cannot be reproduced, is out of scope, or has moved into remediation, and will
share the next step when one is known.

## Scope

Report here when the security impact comes from:

- code or documentation maintained in this repository;
- the `dg` binary's handling of local data, permissions, processes, Git
  worktrees, agent commands, or trust boundaries;
- code bundled into an official Delegator artifact;
- an official Alcubi Delegator installer, release artifact, or publishing
  configuration when one exists; or
- the first-party telemetry collector, its validation and retention behavior,
  and protected administrative access to telemetry data; or
- Delegator's use or configuration of a dependency or integration.

The following boundaries determine where other reports belong:

- **Third-party ACP agents and model providers.** Agents are separate programs
  selected by the user and are not bundled with Delegator. Report a defect
  wholly within an agent, its service, or its model provider to that vendor.
  Report it here too if Delegator exposes data or crosses a security boundary
  incorrectly regardless of which agent is selected.
- **Integrations and external services.** Report a defect in Delegator's own
  integration code here. Report a defect wholly within GitHub, a package host,
  an email provider, or another external service to that service's security
  team.
- **Telemetry collector.** Report bypasses of consent, field allowlists,
  retention, deletion, or instance revocation here. Do not probe the live
  collector, attempt to access another instance's data, or include captured
  telemetry in a report. The collector has no public administrative route;
  queries and deletion use protected Cloudflare access.
- **Installers and distribution.** Official distribution code, public Alcubi
  installer redirects, and GitHub release artifacts are in scope. Unrelated
  packages, mirrors, websites, and installers that claim to distribute Delegator
  are not controlled by the maintainer.
- **Dependencies and bundled code.** Report an issue caused by Delegator's use
  of a dependency here. A defect solely in an upstream dependency should also
  go to its maintainer. The Delegator maintainer can update, remove, or mitigate
  a dependency but cannot set the upstream project's response or release
  schedule.

## Testing boundaries

This policy provides a reporting route. It is not authorization to access or
test any system, account, repository, or data, and it does not provide a legal
safe harbor or waive any rights. Test only systems and data you own or have
explicit permission to test, and comply with applicable law and third-party
terms.

Do not use denial of service, high-volume automated scanning, social
engineering, phishing, malware, persistence, credential attacks, or attempts
to access another person's data. If testing unexpectedly reaches a credential
or data that is not yours, stop, do not retain or share it, and report the
minimum information needed to identify the exposure.

There is no bug-bounty program and no payment or other reward is offered or
implied.

## Coordinated disclosure and credit

Please give the maintainer a reasonable opportunity to investigate and, when
appropriate, prepare a fix or advisory before publishing details. Disclosure
timing will be coordinated case by case according to the likely impact, the
work needed, upstream involvement, and release availability. State any
deadline in the initial report; the policy does not require a reporter to
accept an indefinite embargo.

With the reporter's permission, a published advisory or release note can give
credit by an agreed name or handle. A reporter can instead remain anonymous.
Credit is not compensation and may be omitted for reports that cannot be
verified, are out of scope, or disregard the testing boundaries above.
