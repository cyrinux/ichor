# Security Policy

## Supported versions

Only the latest release of Ichor gets security fixes. Fixes ship in a new release on
GitHub, Google Play and the App Store; there are no backports to older versions.

| Version          | Supported |
|------------------|-----------|
| Latest release   | Yes       |
| Older releases   | No        |

## Reporting a vulnerability

Please don't open a public issue, discussion or pull request for a vulnerability.
Report it privately through
[GitHub's private vulnerability reporting](https://github.com/cyrinux/ichor/security/advisories/new).

Include as much of this as you can:

- the affected version, platform (Android or iOS) and OS version;
- a description of the issue and its impact;
- steps to reproduce, or a proof of concept;
- any suggested fix or mitigation.

Never include real cluster credentials (talosconfig, kubeconfig, certificates or keys) in a
report; redact them or use a throwaway cluster.

## What to expect

- An acknowledgement within 7 days.
- An initial assessment, and a severity, within 14 days.
- Updates as the fix progresses, and a coordinated disclosure date agreed with you.
  Ichor is maintained by volunteers, so the timeline depends on the issue's severity
  and complexity.
- Credit in the advisory and release notes, unless you prefer to stay anonymous.

## Scope

In scope:

- the Android and iOS apps, and the shared Go core in this repository;
- how the apps store and handle cluster credentials and other secrets;
- the in-cluster components and manifests shipped from this repository;
- the build and release pipeline in this repository.

Out of scope:

- vulnerabilities in Talos Linux, Kubernetes or other upstream projects; report those to
  their maintainers (for Talos, [Sidero Labs](https://github.com/siderolabs/talos/security));
- attacks that need an already unlocked, rooted or jailbroken device, or physical access
  with the device's credentials;
- issues in third-party dependencies with no demonstrated impact on Ichor;
- denial of service against your own cluster through valid credentials.

## Safe harbor

Good-faith research that follows this policy, avoids privacy violations and service
disruption, and only tests against clusters and devices you own or have permission to test,
is welcome; we won't pursue legal action over it.
