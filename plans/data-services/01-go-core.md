# Phase 1: Go core (`go/talosmobile`)

Read [README.md](README.md) first for the decisions (D1–D8) and the wire format.

## Files

| File | Content | Target size |
|------|---------|-------------|
| `kube_dataservices.go` | Exported `KubeDataServices`, detection (`/apis`), fan-out, result type | ~150 lines |
| `kube_longhorn.go` | Longhorn volume/replica/node objects, mapping, health derivation | ~250 |
| `kube_garage.go` | Garage discovery, service-proxy calls, `/health` + `/metrics` parsing | ~250 |
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

1. `GET /apis` → `{"groups":[{"name","preferredVersion":{"version"}}]}`. Keep `longhorn.io`
   and `postgresql.cnpg.io` with their preferred version.
2. Garage (no CRD): when the hint includes `garage`, or there are no hints, list
   `GET /api/v1/services` and keep Services that either
   - carry the label `app.kubernetes.io/name=garage` (Helm chart), or
   - expose a port named `admin` or numbered `3903` **and** have a selector that matches
     pods running a Garage image. Reuse `loadAppCatalog()` image matching: list pods once
     with `labelSelector` from the service selector, or reuse `listPods` filtered by
     catalog id `garage`.

   Keep it simple first: label match plus port 3903/`admin`. Add the image fallback only
   if the user's cluster needs it (see the README risks).
3. Fan out the three readers in parallel (`wg.Go`), each with its own error.

## Longhorn (`kube_longhorn.go`)

Paths (version `v` from detection, namespace-agnostic, so they list every namespace):

- `/apis/longhorn.io/{v}/volumes`
- `/apis/longhorn.io/{v}/replicas`
- `/apis/longhorn.io/{v}/nodes`

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

## Garage (`kube_garage.go`)

For each discovered service `ns/svc:port`:

1. `GET /api/v1/namespaces/{ns}/services/{svc}:{port}/proxy/health`
   - 200 → `healthy`; 503 → `degraded`, or `unavailable` when the body says so; other →
     `unknown`. `message` is the trimmed text body (cap 300 chars).
   - The client's `do` currently JSON-decodes and treats non-2xx as an error. Add a small
     `getRaw(ctx, path) (status int, body []byte, err error)` to `kube_client.go` that
     returns the body for any HTTP status, still capped by `kubeMaxBody`. Proxy errors
     (service has no endpoints) come back as 503 with a JSON `Status` body from the API
     server, not from Garage. Tell them apart by `Content-Type: application/json` with
     `"kind":"Status"` → status `unavailable`, message "no ready Garage pod behind service".
2. `GET .../proxy/metrics`. When it returns 200, parse Prometheus text leniently
   (`name{labels} value` lines, ignore comments). Sum across label sets:
   - `block_resync_queue_length` → `resyncQueue`
   - `block_resync_errored_blocks` → `resyncErrors`
   - if present: `cluster_connected_nodes`, `cluster_known_nodes`, `cluster_storage_nodes`,
     `cluster_storage_nodes_ok`, `cluster_partitions`, `cluster_partitions_quorum`,
     `cluster_partitions_all_ok`
   - 401/403 → metrics protected: leave the counters at `-1` and set `source: "health"`.
3. Status refinement: `/health` 200 with `resyncErrors > 0` → `degraded`, message
   "N blocks failed to resync". A non-zero `resyncQueue` alone is normal (see the Garage
   monitoring docs) and only shown, not escalated.

Tests: `parsePromText` (labels, NaN, missing), the status matrix, and an `httptest` server
emulating the API server proxy paths (200/503/Status JSON/401 metrics).

### Phase 1b: Garage admin token (optional)

Richer data (exact partitions, per-node status, layout staging) needs the admin API with a
bearer token:

- Endpoint: `GET .../proxy/v2/GetClusterHealth`, falling back to `/v1/health` on 404.
  Fields: `status`, `knownNodes`, `connectedNodes`, `storageNodes`, `storageNodesOk`,
  `partitions`, `partitionsQuorum`, `partitionsAllOk`.
- The token comes from the user, entered per cluster in the Data services screen and
  stored like `KubeServers` but in `SecureStore` (Android) or Keychain (iOS). Pass it as an
  extra argument `garageToken`. **Do not** read it out of cluster Secrets automatically.
  That's a silent secret read; if it's wanted later, make it an explicit button with
  confirmation.
- Header forwarding: the API server strips `Authorization` only when it used it to
  authenticate (bearer token). Talos admin kubeconfigs authenticate with a **client
  certificate**, so a `Authorization: Bearer <garage token>` header should reach Garage
  through the service proxy. `kubeClient.do` must then let a per-request header override
  the kubeconfig token. That's a conflict for the rare token-based kubeconfig, so in that
  case refuse phase 1b with a clear message. **Verify on a real cluster first** with
  `just probe`. If it doesn't work, drop 1b; `/health`+`/metrics` stays the design.
  (This is why it's split off.)

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

Derivations:

- `archiving`: condition `ContinuousArchiving`. True → `ok`, False → `failing`, absent → `off`.
- `lastBackup`: condition `LastBackupSucceeded`. True → `ok`, False → `failed`, absent → `none`.
- `health`:
  - `critical`: `readyInstances == 0`, or phase contains "Failing over"/"failover"
  - `warning`: `readyInstances < spec.instances`, `currentPrimary != targetPrimary`
    (switchover), `archiving == failing`, `lastBackup == failed`, or the `Ready` condition
    is not True
  - `ok`: otherwise
- Sort: critical first, then by namespace and name.

Tests: healthy, a replica down, switchover, failover, archiving failing, backup failed,
a fresh cluster with no status.

## Demo (`kube_dataservices_demo.go`)

The demo must exercise every UI state: Longhorn with 4 volumes (ok, degraded 2/3 with
1 rebuilding, idle detached, faulted), 3 nodes with one unschedulable disk; Garage healthy
with resync queue 12 and errors 0; CNPG with 2 clusters (healthy, switchover in
progress). Add `TestKubeDataServicesDemo` like `TestKubeDemo` (`kube_test.go:391`). The demo
inventory (`demo_inventory.go`) already lists longhorn and cloudnative-pg images; add a
garage image so the hint path is covered.

## Probe

`go/cmd/probe`: add `dataservices [hints]`, printing the JSON. This is how a human checks
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
- [ ] Privacy test: with the mask on, the output contains no unmasked context or node names
- [ ] gomobile binding builds (`just build` builds the AAR)
