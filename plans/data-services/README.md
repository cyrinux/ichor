# Data services: Longhorn, Garage and CloudNativePG health

Status: **planned**. Pick up from [HANDOFF.md](HANDOFF.md).

## Goal

When a cluster runs Longhorn, Garage or CloudNativePG, Ichor detects it and shows
their health:

- **Longhorn**: every volume's state and robustness, replica count against the desired
  count, rebuilds, and node/disk schedulability.
- **Garage**: whether the cluster is in sync: connected storage nodes, partitions with
  write quorum or with all nodes, the block resync queue and errored blocks.
- **CloudNativePG**: each Postgres cluster's phase, ready against desired instances,
  the primary (and whether a switchover or failover is running), WAL archiving, and the
  last backup.

Clusters without any of the three see nothing new and pay no extra cost.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| D1 | **Detection is two-stage.** (1) Free hint: the Overview already loads the app inventory from Talos container images (`AppsViewModel`, catalog ids `longhorn`, `garage`, `cloudnative-pg` in `go/talosmobile/appcatalog.json`). (2) Confirmation: Go checks the API groups (`GET /apis`) for `longhorn.io` and `postgresql.cnpg.io`, and finds Garage Services/StatefulSets. | Kubernetes is only called when a hint exists, so no admin kubeconfig is signed for clusters that don't need one. The `/apis` check covers operators whose images the catalog misses (mirrored registries). |
| D2 | **Read CRD `status` through the existing minimal REST client** (`kubeClient.get`), not client-go and not each product's own HTTP API, for Longhorn and CNPG. | Matches `kube_workloads.go`. The CRD status is authoritative and needs only the os:admin kubeconfig Ichor already gets. |
| D3 | **Garage goes through the API server's service proxy** to its admin port (3903): `GET /api/v1/namespaces/{ns}/services/{svc}:{port}/proxy/health` (no auth) and `/metrics` (no auth unless `metrics_token` is set). The admin API `GetClusterHealth` (bearer admin token) is an optional extra in phase 1b. | Garage has no CRDs. The phone can't reach pod IPs, but the API server can. `/health` alone already gives "healthy / degraded / unavailable" with a text reason. |
| D4 | **One aggregated Go call, `KubeDataServices`**, with an independent `error` per section. | One round trip for the Overview card and the background monitor. A missing RBAC verb or a down Garage doesn't hide Longhorn. |
| D5 | **UI: an Overview card** (only when something is detected) **plus a new "Data services" screen** with one tab per detected system. | The Overview is where problems should show up. Detail lives on its own screen. It does *not* go into the existing Kubernetes screen (workloads/pods), which stays generic. |
| D6 | **Gated by the existing `Feature.WORKLOADS`** (os:admin). | Same credential as the Kubernetes screen; no new role concept. |
| D7 | **Background alerts are a separate, opt-in phase.** | The monitor runs in WorkManager; a fresh process signs a new kubeconfig per run (`kubeClientTTL` is in-memory). That's acceptable, but it should be opt-in and is decoupled from the UI work. |
| D8 | **Read-only.** No actions (no volume salvage, no CNPG switchover) in this feature. | Smaller blast radius. Actions can follow later, using the `kubeMutationError` pattern. |

## Phases

| Phase | Plan | Depends on | Size |
|-------|------|------------|------|
| 1  | [01-go-core.md](01-go-core.md): Go detection and status for the 3 systems, demo data, tests, probe command | — | L |
| 1b | [01-go-core.md §Garage admin token](01-go-core.md#phase-1b-garage-admin-token-optional): optional Garage admin token for richer status | 1 | S |
| 2  | [02-android.md](02-android.md): models, repository, Overview card, Data services screen, i18n | 1 | L |
| 3  | [03-ios.md](03-ios.md): the same on iOS (IchorCore models plus SwiftUI views) | 1 (can run parallel to 2) | L |
| 4  | [04-monitoring.md](04-monitoring.md): background alerts on both platforms | 2, 3 | M |
| 5  | (optional) Include a data-services summary in the support bundle and AI diagnosis context (`support_collect.go`, `diagnose_collect.go`) | 1 | S |

## Wire format (shared contract between Go, Kotlin and Swift)

```jsonc
{
  "longhorn": {               // null when not detected
    "version": "v1beta2",     // API version actually read
    "error": "",              // set when detection succeeded but reading failed
    "volumes": [{
      "name": "pvc-1234", "namespace": "longhorn-system",
      "pvcNamespace": "db", "pvcName": "data-pg-1",   // from status.kubernetesStatus, "" when unbound
      "state": "attached",          // creating|attached|detached|attaching|detaching|deleting
      "robustness": "healthy",      // healthy|degraded|faulted|unknown
      "health": "ok",               // derived: ok|warning|critical|idle (see 01 §Longhorn)
      "replicasDesired": 3, "replicasHealthy": 3, "rebuilding": 0,
      "node": "worker-1",           // status.currentNodeID
      "size": 10737418240, "actualSize": 2147483648,
      "lastBackupAt": 0             // unix ms, 0 = never
    }],
    "nodes": [{ "name": "worker-1", "ready": true, "schedulable": true,
                "disks": [{ "path": "/var/lib/longhorn", "schedulable": true,
                            "available": 0, "maximum": 0, "scheduled": 0 }] }]
  },
  "garage": {
    "error": "",
    "instances": [{                 // one per Garage deployment found (usually 1)
      "namespace": "garage", "service": "garage", "port": 3903,
      "status": "healthy",          // healthy|degraded|unavailable|unknown
      "message": "Garage is fully operational",   // /health body
      "connectedNodes": 3, "knownNodes": 3,
      "storageNodes": 3, "storageNodesOk": 3,
      "partitions": 256, "partitionsQuorum": 256, "partitionsAllOk": 256,
      "resyncQueue": 0, "resyncErrors": 0,         // -1 when unknown (metrics protected)
      "source": "health+metrics"    // health|health+metrics|admin
    }]
  },
  "cnpg": {
    "error": "",
    "clusters": [{
      "namespace": "db", "name": "pg",
      "phase": "Cluster in healthy state", "phaseReason": "",
      "health": "ok",               // ok|warning|critical (see 01 §CNPG)
      "instances": 3, "readyInstances": 3,
      "currentPrimary": "pg-1", "targetPrimary": "pg-1",
      "archiving": "ok",            // ok|failing|off (ContinuousArchiving condition)
      "lastBackup": "ok",           // ok|failed|none (LastBackupSucceeded condition)
      "lastSuccessfulBackupAt": 0, "firstRecoverabilityAt": 0
    }]
  }
}
```

A section key is **absent/null** when the system isn't installed. An **empty list with no
error** means it's installed but has nothing in it. A **non-empty `error`** means the
system was detected but couldn't be read; the UI shows it inline in that tab only.

## Risks and open questions

- **Garage admin port and service name vary.** The Helm chart (`garage` in
  `deuxfleurs/garage/script/helm`) names the admin port `3903`/`admin`. Others may differ.
  Phase 1 discovers the port by name (`admin`) or number (3903) on Services that select
  Garage pods. → Verify against the user's own cluster with `just probe`.
- **Garage `/metrics` names.** `block_resync_queue_length` and `block_resync_errored_blocks`
  are documented. The `cluster_*` gauges (`cluster_healthy`, `cluster_connected_nodes`,
  `cluster_storage_nodes_ok`, `cluster_partitions_quorum`, …) need to be checked against the
  deployed Garage version before relying on them. Parse leniently: a missing metric means unknown.
- **Garage admin API version.** Garage ≥ 2.0 uses `/v2/GetClusterHealth`; 1.x uses `/v1/health`.
  Phase 1b tries v2 then falls back to v1.
- **Longhorn API version.** Use the group's `preferredVersion` from `/apis` (v1beta2 on
  Longhorn ≥ 1.3, v1beta1 before). Fields read are present in both.
- **Response size.** Large Longhorn installs have hundreds of replicas. `kubeMaxBody` is
  32 MiB, so it's fine. Request only what's needed; there's no field selector for CRDs.
- **Privacy/screenshot mode.** Output passes through `maskResult` like the other Kube*
  calls. Check that PVC names and namespaces don't leak context names (add a privacy test).
