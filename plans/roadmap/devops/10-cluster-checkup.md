# D10. Cluster checkup: the blind spots, on one screen

Status: **implemented** (Go, Android, iOS). Size L (many small checks, one screen).

## Goal

A cluster can look green everywhere Ichor looks (nodes Ready, etcd healthy, GitOps synced) and
still be one step from an outage. The checkup reads what nobody opens a screen for and says, in
plain words, what is wrong and what to change:

| Section | What it catches | Reads |
|---|---|---|
| `workloads` | crash loops, OOM kills, image pulls that fail, pods Pending or not ready for minutes, failed Jobs | pods, jobs |
| `events` | the Warning events of the last hour, grouped by object and reason | events |
| `storage` | PVCs Pending or Lost, volumes nearly full (bytes and inodes), PVs Failed or Released | PVCs, PVs, kubelet `stats/summary` through the node proxy |
| `upgrade` | API versions still requested that a coming Kubernetes release removes | API server `/metrics` (`apiserver_requested_deprecated_apis`) |
| `webhooks` | admission webhooks whose Service has no ready endpoint: `Fail` blocks every write, `Ignore` is silently skipped | webhook configurations, EndpointSlices |
| `capacity` | nodes whose requests fill them, a node whose pods would not fit elsewhere, quotas at their limit | nodes, pods, ResourceQuotas |
| `nodes` | a node left cordoned, pressure conditions, taints and labels | nodes |
| `loadbalancers` | Services of type LoadBalancer without an address, Cilium IP pools with nothing left | services, `CiliumLoadBalancerIPPool` |
| `terminating` | namespaces, pods and PVCs stuck in Terminating, with the finalizer or content in the way | namespaces, pods, PVCs |
| `certificates` | CertificateSigningRequests nobody approved (kubelet serving certificates) | CSRs |
| `secrets` | ExternalSecrets and SecretStores that do not sync | `external-secrets.io` |
| `helm` | Helm releases failed, or stuck in `pending-*` (a lock nobody releases), outside Flux and Argo CD | Helm release Secrets, metadata only |

Not in the checkup, but part of the same work:

- **Events of one object** (`kubectl describe`'s last lines) in the pod and workload sheets.
- **Background alerts** for the checkup's critical findings (opt-in, like data services and GitOps).
- **etcd disk latency**: Prometheus panels (WAL fsync, backend commit, leader changes). etcd's own
  metrics are not served by the API server, so this needs the Prometheus source.
- **Talos rollback** (`talosctl rollback`) for an upgrade that boots but misbehaves.

## Key decisions

1. **One Go call, one report.** `KubeCheckup(cfg, ctx, server)` returns
   `{status, kubeVersion, sections:[{id, status, error, checked, findings}], nodes, volumes, releases}`.
   Sections are read in parallel; one that fails carries its `error` and the others still show
   (as `KubeDataServices`). A section whose API is not served is `absent` and hidden.
2. **Findings are codes, the apps word them** (as the audit analysis): `{kind, severity,
   namespace, name, node, reason, message, value, limit, count, since, extra}`. Each kind has a
   title and a fix in the six languages. Kubernetes' own text (an event message, a condition) is
   shown as is, muted.
3. **Shared reads.** Pods and nodes are listed once and feed workloads, capacity, nodes and
   terminating. The pod list is whole (requests need every pod), page by page.
4. **Volume fill without Prometheus**: the kubelet's `stats/summary` of each node
   (`/api/v1/nodes/<n>/proxy/stats/summary`) carries used, capacity and inodes per PVC. A node
   that does not answer leaves its volumes without a level; it is not an error.
5. **Thresholds** are constants next to each check (volume 85 % warning / 95 % critical, node
   requests 90 %, quota 90 % / 100 %, Pending 5 min, Terminating 10 min, CSR 10 min, Helm
   `pending-*` 10 min). No settings screen.
6. **Deprecated APIs**: critical when the release that removes the API is the next minor of the
   running server, a warning otherwise. The metric says the API was requested since the API
   server started, not by whom; the audit analysis names the client.
7. **Read-only.** The checkup changes nothing. Fixing goes through the screens that exist
   (pods, workloads, node maintenance) or `kubectl`.
8. **Helm releases are read from Secret metadata only** (`owner=helm` labels: name, status,
   version), never their payload. A release opens its detail, where it can be rolled back (D11,
   [11-helm.md](11-helm.md)).

## Go (`go/ichorgo`)

- `kube_checkup.go`: the report, `KubeCheckup`, the parallel reader, the verdict.
- `kube_checkup_workloads.go`, `_storage.go`, `_upgrade.go`, `_webhooks.go`, `_capacity.go`,
  `_network.go` (load balancers), `_lifecycle.go` (terminating, CSRs), `_addons.go`
  (external secrets, Helm), `kube_quantity.go` (Kubernetes quantities).
- `kube_events.go`: `KubeEvents(cfg, ctx, server, namespace, kind, name)`.
- `kube_checkup_demo.go`: a demo cluster with one finding of every kind.
- `rollback.go`: `Rollback(cfg, ctx, node)` (os:admin).
- `prom_presets.go`: three etcd panels.
- `just probe checkup`, `probe kube-events <ns> [<kind>] <name>`. Rollback has no probe command:
  it reboots the node.

## UI (both apps)

- **Checkup screen**, opened from the Kubernetes screen's top bar and from Cluster insights: a
  verdict header (critical / warnings / all clear, with counts), then one card per section with
  its icon, verdict colour and count. A section opens in place: findings as cards (subject,
  what happens, Kubernetes' own message, what to do), worst first. `capacity` shows a bar per node
  (CPU and memory requested against allocatable), `storage` a bar per volume, worst first,
  `nodes` the taints, `helm` the releases.
- **Events** section at the bottom of the pod sheet and the workload sheet.
- **Alerts**: Settings → Alerts gets "Cluster checkup" next to data services and GitOps apps: a
  third track with the same rules (critical at once, warning on two checks in a row, cleared once).
- **Rollback**: in the node's upgrade screen, with a confirmation naming the node.

## Phases

1. Go core, demo, tests, probe. (done)
2. Android: screen, events, alerts, rollback. (done)
3. iOS: the same; a tapped checkup notification opens the app, not the screen yet. (done)
4. README. (done; the changelog comes from the commits)

## Out of scope, kept for later

- ~~**Port-forward from the phone**~~: shipped since (`kube_portforward.go`).
- **Editing taints and labels**: shown here, edited later with node maintenance (D1).
- ~~**Helm rollback**~~ shipped since ([11-helm.md](11-helm.md)); **MetalLB pools** (no status to read), **BGP peers** (needs `cilium bgp
  peers` in each agent).
