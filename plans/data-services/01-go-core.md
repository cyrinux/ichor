# Phase 1: Go core (`go/talosmobile`)

Read [README.md](README.md) first for the decisions (D1–D8) and the wire format.

## Files

| File | Content | Target size |
|------|---------|-------------|
| `kube_dataservices.go` | Exported `KubeDataServices`, detection (`/apis`), fan-out, result type | ~150 lines |
| `kube_longhorn.go` | Longhorn volume/replica/node objects, mapping, health derivation | ~250 |
| `kube_garage.go` | Garage pod discovery, CLI commands (`json-api` / `status` / `stats`) and parsers, `/health` fallback | ~300 |
| `kube_exec.go` | Minimal pods/exec over WebSocket (`x/net/websocket`), fixed argv only | ~150 |
| `kube_cnpg.go` | CNPG Cluster objects, mapping, health derivation | ~180 |
| `kube_dataservices_demo.go` | Demo data (one degraded Longhorn volume, a healthy Garage, a CNPG cluster mid-failover) | ~80 |
| `kube_dataservices_test.go`, `kube_longhorn_test.go`, `kube_garage_test.go`, `kube_cnpg_test.go` | Table tests on mapping, plus an `httptest` API server | — |
| `go/cmd/probe/main.go` | Add a `dataservices` subcommand | +15 |

Follow `kube_workloads.go` exactly for style: small typed structs holding only the read
fields, `wg.Go` fan-out, `errors.Join`, `toJSON`, comments explaining *why*.

## Exported API

```go
// KubeDataServices reports the health of the storage and database operators the cluster
// runs (Longhorn, Garage, CloudNativePG), through the Kubernetes API with the admin
// kubeconfig Talos issues (os:admin). hints is a comma-separated list of catalog app ids
// the app already saw in the inventory ("longhorn,garage,cloudnative-pg"); "" checks all.
// See plans/data-services/README.md for the JSON. kubeServer: see KubePods.
func KubeDataServices(configYAML, contextName, kubeServer, hints string) (out string, err error)
```

Shape (copy from `KubeWorkloads`):

```go
defer maskResult(&out, &err)
contextName = unmaskContext(configYAML, contextName)
if isDemoContext(configYAML, contextName) { return toJSON(demoDataServices()) }
res, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx, k) (dataServices, error) {
    return readDataServices(ctx, k, parseHints(hints))
})
```

`readDataServices` returns an error **only** when nothing at all could be read (for example
`/apis` fails). Then `withKube` handles the client the usual way. Per-system failures go
into that section's `error` string.

## Detection (`kube_dataservices.go`)

1. `GET /apis` → `{"groups":[{"name","preferredVersion":{"version"}}]}`. Keep, with
   their preferred versions: `longhorn.io`, `postgresql.cnpg.io`, `barmancloud.cnpg.io`
   (plugin backups) and `deuxfleurs.fr` (Garage `garagenodes`, a hint only).
2. Garage (no CRD of its own): when the hint includes `garage`, or there are no hints,
   find Garage **pods** by image (see §Garage, "Finding the pod"). Services are only
   needed for the `/health` fallback: there, pick the Service in the same namespace whose
   selector matches the pods and that exposes port `admin`/3903. In the user's cluster
   that's a dedicated admin/metrics Service, not the
   S3 service.
3. Fan out the three readers in parallel (`wg.Go`), each with its own error.

## Longhorn (`kube_longhorn.go`)

Paths (version `v` from detection, namespace-agnostic, so they list every namespace):

- `/apis/longhorn.io/{v}/volumes`
- `/apis/longhorn.io/{v}/replicas`
- `/apis/longhorn.io/{v}/nodes`
- `/apis/longhorn.io/{v}/backuptargets`: `status.available` (bool), `status.conditions`
  (`Unavailable` with message), and `spec.backupTargetURL` (shown as-is; secrets aren't
  part of the URL). The user's target is S3 on their standalone Garage
  instance, so an unavailable target often means Garage trouble. Show both together in the UI.

Target version: the user runs Longhorn **1.12.0**, so v1beta2.
Build the fixtures from that.

Fields to read:

```go
type lhVolume struct {
    Metadata struct{ Name, Namespace string } `json:"metadata"`
    Spec struct {
        NumberOfReplicas int   `json:"numberOfReplicas"`
        Size             string `json:"size"` // a string of bytes
    } `json:"spec"`
    Status struct {
        State         string `json:"state"`
        Robustness    string `json:"robustness"`
        CurrentNodeID string `json:"currentNodeID"`
        ActualSize    int64  `json:"actualSize"`
        LastBackupAt  string `json:"lastBackupAt"`
        KubernetesStatus struct {
            Namespace string `json:"namespace"`
            PVCName   string `json:"pvcName"`
        } `json:"kubernetesStatus"`
    } `json:"status"`
}
type lhReplica struct {
    Spec struct {
        VolumeName string `json:"volumeName"`
        NodeID     string `json:"nodeID"`
        HealthyAt  string `json:"healthyAt"`
        FailedAt   string `json:"failedAt"`
    } `json:"spec"`
    Status struct{ CurrentState string `json:"currentState"` } `json:"status"`
}
type lhNode struct {
    Metadata struct{ Name string } `json:"metadata"`
    Status struct {
        Conditions []struct{ Type, Status string } `json:"conditions"` // Ready, Schedulable
        DiskStatus map[string]struct {
            Conditions       []struct{ Type, Status string } `json:"conditions"`
            StorageAvailable int64 `json:"storageAvailable"`
            StorageMaximum   int64 `json:"storageMaximum"`
            StorageScheduled int64 `json:"storageScheduled"`
        } `json:"diskStatus"`
    } `json:"status"`
}
```

Note: on v1beta2, `healthyAt`/`failedAt` moved to `status` in recent releases. Decode both
spec and status locations and take whichever is set. Check this against a v1.7+ install.

Derivations:

- `replicasHealthy`: replicas of the volume with `currentState == "running"`, `healthyAt != ""`
  and `failedAt == ""`.
- `rebuilding`: running replicas with `healthyAt == ""` (rebuild in progress).
- `health`:
  - `critical`: `robustness == "faulted"`
  - `warning`: `robustness == "degraded"`, or `robustness == "unknown"` while attached
  - `idle`: detached and not faulted (normal for a volume whose workload is scaled down)
  - `ok`: otherwise
- Sort: critical, then warning, then ok, then idle; then by PVC namespace and name, then volume name.

Tests: table-driven `mapLonghornVolume` cases (healthy, degraded with 2/3 replicas,
rebuilding, faulted detached, detached idle, unbound PVC), node/disk mapping, and the v1beta1
field layout.

## Garage (`kube_garage.go`, `kube_exec.go`)

**Primary source: run the Garage CLI inside a Garage pod** with a Kubernetes exec. The CLI
talks to the cluster over Garage's own RPC with the `rpc_secret` already in the pod, so
it needs no admin token, no `metrics_token` and no reachable admin port, and it sees the
whole cluster from any one node. **Fallback:** the `/health` service proxy (step 3
below), used when exec is refused (RBAC, PodSecurity admission), when no Garage pod is
ready, or when the CLI output can't be read.

Talos has no container exec API, so this has to go through Kubernetes (`pods/exec`, which
the os:admin kubeconfig allows).

### Finding the pod

0. If `/apis` lists the group `deuxfleurs.fr`, list `garagenodes` (preferred version) in
   every namespace. Garage with `kubernetes-discovery` (the user's build has it) registers
   one object per node, named after the node's public key, labelled
   `garage.deuxfleurs.fr/service=<kubernetes_service_name>`, in `kubernetes_namespace`.
   That gives the namespace and expected node count without guessing. The CRD isn't
   always there (`kubernetes_skip_crd`, or discovery not configured), so it's only a hint.
1. List pods (`/api/v1/pods`; reuse `listPods` from `kube_pods.go`). Keep those with a
   container whose image the catalog maps to `garage` (`loadAppCatalog()`; images
   `dxflrs/garage`, `dxflrs/amd64_garage`, `dxflrs/arm64_garage`). The
   `app.kubernetes.io/name=garage` label is a second signal.
2. Group them into Garage clusters (an `instance` in the wire format) by **namespace + the
   pod's `app.kubernetes.io/name` label** (or the controller owner when the label is
   missing). Don't assume the name `garage`, the namespace, or the controller kind:
   - the user's main cluster is a **DaemonSet** (7 pods, random suffixes, replication 3), and
   - a second, standalone **StatefulSet** in another namespace (1 pod, replication 1).

   Pick the first pod (sorted by name) that is `Running` with its Garage container ready.
   With a DaemonSet there's no ordinal; any node's CLI sees the whole cluster. If none is
   ready, set status `unavailable` with "no ready Garage pod" and still report the pod
   counts. The container is the one whose image matched (named `garage` in both of the
   user's deployments).
3. A single-node Garage (`storageNodes == 1`) is normal for its kind: don't flag "1/1
   nodes" or the replication factor.

### Exec (`kube_exec.go`, new, ~150 lines)

`func (k *kubeClient) exec(ctx, namespace, pod, container string, argv []string) (stdout, stderr []byte, err error)`

- `GET {base}/api/v1/namespaces/{ns}/pods/{pod}/exec?container=..&command=a&command=b&stdout=true&stderr=true`,
  upgraded to WebSocket with subprotocol `v5.channel.k8s.io`, falling back to
  `v4.channel.k8s.io`, which every supported Kubernetes version accepts.
- Use `golang.org/x/net/websocket`, already a direct dependency in `go/go.mod`, so no new
  module. Dial with the client's own `*tls.Config` (client certificate) and the base
  address `openKubeClient` picked. For a token kubeconfig, set `Authorization` on the
  handshake.
- Frames: the first byte is the channel. 1 = stdout, 2 = stderr, 3 = error/status (JSON
  `metav1.Status`: `Success`, or `Failure` with reason `NonZeroExitCode`).
- Bounds: 10 s per command and 1 MiB per stream (Garage output is a few KB).
- **Safety:** `exec` is only ever called with fixed argv from an allow-list in
  `kube_garage.go`; nothing the user types reaches the command. Namespace, pod and
  container go through `validateKubeName`. Read-only commands only: never `garage layout`,
  `repair`, `key` or `bucket`.
- The binary is `/garage`. The official image is a static binary on `scratch` (no shell,
  no `$PATH`), so always use the absolute path and never `sh -c`.

### Commands

Target: **Garage v2.3.0** (the user's cluster). Step 1 is what gets built now. Step 2
(1.x) is deferred until someone running 1.x asks for it: on a 1.x pod `json-api` fails
with "unrecognized subcommand", and the code goes straight to step 3.

1. **Garage ≥ 2.0: `garage json-api`** (added in v2.0.0; it calls admin API endpoints from
   the CLI and prints JSON):
   - `/garage json-api GetClusterHealth` → `status` (healthy/degraded/unavailable),
     `knownNodes`, `connectedNodes`, `storageNodes`, `storageNodesOk`, `partitions`,
     `partitionsQuorum`, `partitionsAllOk`.
   - `/garage json-api GetClusterStatus` → nodes (id, hostname, `isUp`, `lastSeenSecsAgo`,
     zone/capacity from the layout, data partition free/total) and the layout version.
     Staged layout changes become the warning "layout changes not applied".
   - `/garage json-api GetNodeStatistics` with `node` = `*` (all nodes) → per node
     `blockManager.resyncQueueLen`, `blockManager.resyncErrors` (`NodeBlockManagerStats`).
     Sum them for the instance, and keep a per-node breakdown for the UI.
     - Multi-node endpoints answer `{"success": {nodeId: …}, "error": {nodeId: "msg"}}`.
       A node in `error` counts as unreachable and makes the status `degraded`.
     - **Verify on v2.3.0** how `json-api` takes the `node` query parameter (positional
       JSON `'{"node":"*"}'` or a flag) with `/garage json-api --help`; capture the output
       as a fixture.
   - Allow-list (exact argv, nothing else):
     `["/garage","json-api","GetClusterHealth"]`, `["/garage","json-api","GetClusterStatus"]`,
     and the `GetNodeStatistics` form found above.
   - Run the three in parallel (three execs on the same pod), and each one's failure is
     independent: health alone is enough for a status.
   - Set `source: "cli-json"`.
   - Fixtures: the three outputs from v2.3.0 go in `testdata/garage/v2.3.0/`, captured from
     the user's cluster with `just probe dataservices garage --raw`.
2. **(Deferred) Garage 1.x: `/garage status` and `/garage stats`**, parsed leniently as text:
   - `status`: count the rows under the `==== HEALTHY NODES ====` and
     `==== FAILED NODES ====` headers → `storageNodesOk` / `storageNodes`. Rows under
     FAILED go into `failedNodes` (hostname, last seen).
   - `stats`: the "resync queue length" and "blocks with resync errors" lines. Match the
     label case-insensitively and take the trailing integer. Partition and table counts
     are optional.
   - Anything unparsed stays unknown (`-1`). The raw output, capped at 4 KB, goes in `raw`
     so the UI can show it under "Details". Set `source: "cli-text"`.
   - Needs 1.x fixtures from a real 1.x install before it's written.
3. **Fallback: the service proxy.** If exec fails or no pod is ready, find a Service in
   that namespace exposing port `admin`/3903 and call
   `GET /api/v1/namespaces/{ns}/services/{svc}:{port}/proxy/health`.
   - 200 → `healthy`; 503 → `degraded`/`unavailable`, with the text body as `message`.
   - Proxy errors (no endpoints) arrive as a JSON `Status` from the API server; treat
     them as `unavailable`.
   - This needs a small `getRaw(ctx, path) (status int, body []byte, err error)` in
     `kube_client.go`.
   - Set `source: "health"`, with the counters unknown. `exec` failing is noted in
     `message` ("exec refused: …") so the user knows why the details are missing.

### Status rules

- `unavailable`: health says so, or `partitionsQuorum < partitions`, or no ready pod
- `degraded`: `storageNodesOk < storageNodes`, `partitionsAllOk < partitions`,
  `resyncErrors > 0` (blocks that failed to resync: risk of data loss), or staged layout
  changes
- `healthy`: otherwise. A non-zero `resyncQueue` on its own is normal (Garage monitoring
  docs), so it's shown but not escalated.

Tests: the argv allow-list, frame demux (stdout/stderr/status, non-zero exit), the
`json-api` JSON mapping against the v2.3.0 fixtures (including a partial `error` map),
the status matrix, the
fallback path, and an `httptest` WebSocket server emulating the API server exec endpoint.

## CloudNativePG (`kube_cnpg.go`)

Path: `/apis/postgresql.cnpg.io/{v}/clusters`.

```go
type cnpgCluster struct {
    Metadata struct{ Name, Namespace string } `json:"metadata"`
    Spec struct{ Instances int `json:"instances"` } `json:"spec"`
    Status struct {
        Instances               int    `json:"instances"`
        ReadyInstances          int    `json:"readyInstances"`
        Phase                   string `json:"phase"`
        PhaseReason             string `json:"phaseReason"`
        CurrentPrimary          string `json:"currentPrimary"`
        TargetPrimary           string `json:"targetPrimary"`
        LastSuccessfulBackup    string `json:"lastSuccessfulBackup"`
        FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint"`
        Conditions []struct{ Type, Status, Reason, Message string } `json:"conditions"`
    } `json:"status"`
}
```

Also read `spec.plugins[] {name, isWALArchiver, parameters.barmanObjectName}` and
`spec.backup.barmanObjectStore` (presence only).

**Backups: plugin first.** The user's 29 clusters *all* back up through the
barman-cloud plugin (CNPG chart 0.28.3, plugin v0.13.0): `spec.plugins` with
`barman-cloud.cloudnative-pg.io` and an `ObjectStore` (`barmancloud.cnpg.io/v1`).
There are 28 `ScheduledBackup`s with `method: plugin`, and no in-tree `barmanObjectStore`.
In that setup the Cluster's own backup fields can be empty, so:

- When `/apis` lists `barmancloud.cnpg.io`, also list
  `/apis/barmancloud.cnpg.io/{v}/objectstores`. For each cluster, take the ObjectStore
  named by its plugin's `barmanObjectName` (same namespace), then
  `status.serverRecoveryWindow[<serverName>]`, where `serverName` defaults to the cluster
  name (the plugin parameter `serverName` overrides it). Read `firstRecoverabilityPoint`,
  `lastSuccessfulBackupTime` and `lastFailedBackupTime`.
- List `/apis/postgresql.cnpg.io/{v}/scheduledbackups` to know which clusters are
  *expected* to have backups (`spec.cluster.name`, `spec.suspend`).
- Fallback for in-tree setups: the Cluster's `status.lastSuccessfulBackup` /
  `firstRecoverabilityPoint` and the `LastBackupSucceeded` condition.

Derivations:

- `archiving`: is a WAL archiver configured? Either a plugin with `isWALArchiver: true`,
  or in-tree `barmanObjectStore`.
  - Not configured → `off`. This is **neutral**, not a warning: two of the user's clusters
    deliberately set `isWALArchiver: false`.
  - Configured: the condition `ContinuousArchiving` True → `ok`, False → `failing`
    (with its message), absent → `unknown`.
- `lastBackup`:
  - `failed`: `lastFailedBackupTime` is newer than `lastSuccessfulBackupTime`, or the
    condition `LastBackupSucceeded` is False
  - `stale`: a non-suspended ScheduledBackup exists, but the last success is older than
    the expected interval plus a margin. Keep it simple: parse the 6-field cron for "daily"
    vs "weekly" (the user's schedules look like `0 0 3 * * 0`, i.e. weekly) and allow
    ×1.5. Unknown schedule → 8 days.
  - `none`: no backup ever and none expected
  - `ok`: otherwise
- `health`:
  - `critical`: `readyInstances == 0`, or phase contains "Failing over"/"failover"
  - `warning`: `readyInstances < spec.instances`, `currentPrimary != targetPrimary`
    (switchover), `archiving == failing`, `lastBackup` `failed`/`stale`, or the `Ready`
    condition is not True
  - `ok`: otherwise
- Sort: critical first, then warning, then by namespace and name.

Scale: 29 clusters means one `clusters` list, one `objectstores` list and one
`scheduledbackups` list (3 requests, fanned out). There's no per-cluster call.

Tests: healthy; a replica down; switchover; failover; plugin with a recent success;
plugin with a failure after the success; stale weekly backup; `isWALArchiver: false`
(neutral); in-tree fallback; a fresh cluster with no status; an ObjectStore missing.

## Demo (`kube_dataservices_demo.go`)

The demo must exercise every UI state: Longhorn with 4 volumes (ok, degraded 2/3 with
1 rebuilding, idle detached, faulted), 3 nodes with one unschedulable disk, and an available backup target; Garage with two
instances (a 3-node DaemonSet cluster with resync queue 12 and errors 0, `source: cli-json`,
and a standalone 1-node one); CNPG with 4 clusters (healthy with a plugin backup,
switchover in progress, a stale weekly backup, archiving off). Add `TestKubeDataServicesDemo` like `TestKubeDemo` (`kube_test.go:391`). The demo
inventory (`demo_inventory.go`) already lists longhorn and cloudnative-pg images; add a
garage image so the hint path is covered.

## Probe

`go/cmd/probe`: add `dataservices [hints] [--raw]`, printing the JSON (`--raw` also prints the raw Garage CLI output, for test fixtures). This is how a human checks
against a real cluster before any UI exists:

```sh
just probe dataservices            # all
just probe dataservices garage     # Garage only
```

## Done when

- [ ] `just test` (Go tests) green; new files ≥ 80% coverage (`go test -cover ./go/talosmobile/`)
- [ ] `golangci-lint` clean, if CI runs it (check `.github/workflows/android.yml`)
- [ ] `just probe dataservices` against the user's real cluster shows sensible Longhorn,
      Garage and CNPG output; README risks updated with what was learned
- [ ] Garage CLI fixtures from the real cluster committed in `testdata/garage/`
- [ ] Privacy test: with the mask on, the output contains no unmasked context or node names
- [ ] gomobile binding builds (`just build` builds the AAR)
