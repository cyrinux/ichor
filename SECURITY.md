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
- how the apps store and handle cluster credentials and other secrets (a PagerDuty token, for
  example, stays in the sealed per-cluster store and out of backups, logs and support bundles);
- what the AI diagnosis sends: an anonymized report masks node, namespace and pod names, every
  report has secrets-like strings stripped, and plain http is refused off the local network;
- the in-cluster components and manifests shipped from this repository;
- the build and release pipeline in this repository.

Out of scope:

- vulnerabilities in Talos Linux, Kubernetes or other upstream projects; report those to
  their maintainers (for Talos, [Sidero Labs](https://github.com/siderolabs/talos/security));
- attacks that need an already unlocked, rooted or jailbroken device, or physical access
  with the device's credentials;
- issues in third-party dependencies with no demonstrated impact on Ichor;
- denial of service against your own cluster through valid credentials.

## Trust model of ichor-relay

ichor-relay is optional. It runs in your cluster, receives Alertmanager's webhook and pushes
the alerts to the phones paired with it, so they hear about an alert within seconds instead
of at the next background check. It is the only part of Ichor that runs on a server, and the
only path where alert data leaves the cluster through a third party. This section says who
sees what, what each secret protects, and what to do when one of them leaks.

### Parties

- **The cluster:** Alertmanager, the relay (one pod, one Secret) and the Kubernetes API
  server, whose service proxy the phones go through to pair.
- **The push server:** the server of your UnifiedPush distributor (ntfy.sh, a self-hosted
  ntfy, or another distributor's server), or the ntfy server of an ntfy topic.
- **The phone:** the Ichor app and the distributor or ntfy app installed next to it.
- **The project:** builds and publishes the relay's image. It runs no server and receives
  nothing.

### Data flow

1. Alertmanager posts its webhook to the relay inside the cluster, with
   `Authorization: Bearer <webhookToken>`. Anything else gets a 401.
2. The relay keeps, for each alert: its name, severity, firing or resolved, namespace,
   `summary` annotation, start time and fingerprint. Other labels and annotations are dropped
   there.
3. The relay sends one push per paired phone:
   - **UnifiedPush:** the alerts as JSON, encrypted for that phone with Web Push
     ([RFC 8291](https://datatracker.ietf.org/doc/html/rfc8291)) and signed with the relay's
     VAPID key ([RFC 8292](https://datatracker.ietf.org/doc/html/rfc8292)). The push server
     sees the endpoint, the size, the time, and the urgency header: `high` when a critical
     alert fires, `normal` otherwise.
   - **ntfy** (the path for iOS, where UnifiedPush does not exist): a plain-text publish to
     the phone's topic. The title has the label the phone chose and the counts; the body has
     one line per alert (severity, name, namespace), and the `summary` too if the phone paired
     as verbose. The `Click` header is a link to the cluster's Alerts screen in Ichor; its
     cluster id is a hash, not a name.
4. The phone decrypts the UnifiedPush message itself. With ntfy, the ntfy app shows the
   message and opens Ichor when tapped.

A Web Push message waits 24 hours at most on the push server for a phone that is offline;
an ntfy server keeps its messages for as long as its cache is configured to.

### Who sees what

| Party | Alert names | Namespaces | Summaries | Other labels and annotations | Which cluster | When alerts fire |
|---|---|---|---|---|---|---|
| Relay pod | Yes | Yes | Yes | Yes (dropped on arrival) | Yes | Yes |
| UnifiedPush push server | No | No | No | No | No | Yes (time, size, urgency) |
| ntfy server (ntfy path) | Yes | Yes | Only if verbose | No | The label the phone chose | Yes |
| The project | No | No | No | No | No | No |

The relay never sees the phone's kubeconfig or talosconfig: they stay on the phone, as
without the relay. The relay's logs hold device ids and HTTP statuses, never a token, a push
endpoint or an alert's labels.

### Secrets and where they live

The relay keeps all its state in one Secret, `ichor-relay` in its namespace, created on its
first start:

- `webhookToken`: what Alertmanager sends to post alerts. Alertmanager's own configuration
  holds a copy.
- `pairingToken`: what the phone sends (header `X-Ichor-Pairing`) to read the relay's VAPID
  public key and to add or remove a device. The phone reads it from the Secret with its own
  Kubernetes credentials while pairing and does not keep it. **Whoever may `get` this Secret
  may pair a phone: RBAC is the pairing policy.**
- `vapidPrivateKey`, `vapidPublicKey`: the key pair that signs the Web Push messages.
- `devices.json`: the paired phones: an id, the push endpoint, a label, the cluster's
  fingerprint, and for UnifiedPush the phone's public key and auth secret. These two let the
  relay encrypt for the phone; the key that decrypts never leaves the phone's distributor.
  An ntfy endpoint is the topic URL: whoever reads it can subscribe to the topic.

The relay's ServiceAccount needs `get`, `create` and `update` on that Secret and nothing
else. Bound to a Role limited to that, the relay cannot read or change anything else in the
cluster.

### If something is compromised

- **The push server:** it can delay or drop pushes, and see when alerts fire. It cannot read
  a UnifiedPush message. An ntfy server reads ntfy messages: use a topic name nobody can guess,
  or your own ntfy server with access control.
- **The relay pod:** it can read the alerts Alertmanager sends and push fake alerts to the
  paired phones. It cannot act on the cluster: its only permissions are on its own Secret.
- **The pairing token:** whoever has it and can reach the relay's Service can add a device and
  receive the alerts, or remove a device. Rotate everything (below).
- **The webhook token:** whoever has it and can reach the relay's Service can push fake alerts
  to every paired phone. Rotate everything.
- **A lost phone:** revoke its kubeconfig and talosconfig first; that is the larger risk. Then
  remove its entry from `devices.json` in the Secret and restart the relay, or rotate
  everything.

A paired device can name any https address as its endpoint, and the relay will post the
alerts there (plain http only when the relay runs with `RELAY_ALLOW_HTTP=true`, for an
in-cluster ntfy). Limit the relay's outgoing traffic with a NetworkPolicy if your cluster
allows it.

### Rotation

Delete the Secret, then restart the relay
(`kubectl -n <namespace> delete secret ichor-relay`, then delete the relay's pod). The relay
creates a new Secret with new tokens, a new VAPID key pair and no device. Then:

- put the new `webhookToken` in Alertmanager's `webhook_configs`;
- pair every phone again.

The relay reads the Secret only when it starts, so delete it before the restart, not after.

### Not covered

- **End-to-end encryption on iOS:** UnifiedPush does not exist on iOS and Ichor runs no
  push service of its own, so the iOS path goes through the ntfy app, which shows the plain
  text it receives.
- **Alerts that do not come from Alertmanager:** Ichor's own checks (node readiness, storage,
  GitOps) still come from the app's background checks only.
- **Delivery:** push is best effort. The app's background checks keep running and still
  alert when the relay or the push server is down.

## Safe harbor

Good-faith research that follows this policy, avoids privacy violations and service
disruption, and only tests against clusters and devices you own or have permission to test,
is welcome; we won't pursue legal action over it.
