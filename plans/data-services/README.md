# Data services: Longhorn, Garage and CloudNativePG health

Status: **phases 1 (Go core), 2 (Android) and 3 (iOS) done**; phase 4 planned. Pick up from [HANDOFF.md](HANDOFF.md).

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
| D3 | **Garage: run its CLI inside a Garage pod** through a Kubernetes exec: `/garage json-api GetClusterHealth` / `GetClusterStatus` / `GetNodeStatistics` (Garage ≥ 2.0; the user runs v2.3.0). 1.x text parsing (`garage status`/`stats`) is deferred. Fallback: `/health` through the API server's service proxy to the admin port. Only fixed, read-only commands are run. | Garage has no CRDs. The CLI uses the RPC secret already in the pod, so it needs no admin token or `metrics_token` and gives the full sync picture (nodes, partitions, resync queue and errors, staged layout). Talos has no container exec, so this goes through Kubernetes `pods/exec`. |
| D4 | **One aggregated Go call, `KubeDataServices`**, with an independent `error` per section. | One round trip for the Overview card and the background monitor. A missing RBAC verb or a down Garage doesn't hide Longhorn. |
| D5 | **UI: an Overview card** (only when something is detected) **plus a new "Data services" screen** with one tab per detected system. | The Overview is where problems should show up. Detail lives on its own screen. It does *not* go into the existing Kubernetes screen (workloads/pods), which stays generic. |
| D6 | **Gated by the existing `Feature.WORKLOADS`** (os:admin). | Same credential as the Kubernetes screen; no new role concept. |
| D7 | **Background alerts are a separate, opt-in phase** (off by default; confirmed by the user). | The monitor runs in WorkManager; a fresh process signs a new kubeconfig per run (`kubeClientTTL` is in-memory). That's acceptable, but it should be opt-in and is decoupled from the UI work. |
| D9 | **Point to the likely cause.** Every problem item carries the Kubernetes node(s) involved: Longhorn replica `nodeID`, CNPG instance pods' `spec.nodeName`, Garage `role.zone`/hostname mapped to pods. The UI joins that with the node health Ichor already has from Talos/Kubernetes and shows "likely cause: node X NotReady" once, at the top, instead of 40 separate red rows. | The first live run showed exactly this: one cordoned, NotReady node caused a degraded Garage, a faulted Longhorn volume, 15 CNPG clusters short of instances and 2 with none. |
| D8 | **Read-only.** No actions (no volume salvage, no CNPG switchover) in this feature. | Smaller blast radius. Actions can follow later, using the `kubeMutationError` pattern. |

## Phases

| Phase | Plan | Depends on | Size |
|-------|------|------------|------|
| 1  | [01-go-core.md](01-go-core.md): Go detection and status for the 3 systems, demo data, tests, probe command | — | L |
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
      "replicaNodes": ["worker-1", "worker-2"],    // D9: nodes holding replicas (failed ones too)
      "node": "worker-1",           // status.currentNodeID
      "size": 10737418240, "actualSize": 2147483648,
      "lastBackupAt": 0             // unix ms, 0 = never
    }],
    "backupTargets": [{ "name": "default", "url": "s3://longhorn-backups@garage/",
                        "available": true, "message": "" }],
    "nodes": [{ "name": "worker-1", "ready": true, "schedulable": true,
                "disks": [{ "path": "/var/lib/longhorn", "schedulable": true,
                            "available": 0, "maximum": 0, "scheduled": 0 }] }]
  },
  "garage": {
    "error": "",
    "instances": [{                 // one per namespace + app name running Garage pods
      "namespace": "garage", "name": "garage",     // name: the pods' app.kubernetes.io/name (or controller)
      "pod": "garage-0",            // pod the CLI ran in ("" when none was ready)
      "pods": 3, "podsReady": 3,
      "version": "v2.3.0",          // from GetClusterStatus, "" when unknown
      "status": "healthy",          // healthy|degraded|unavailable|unknown
      "message": "",                // English, cause first: "1 node down (zone z, tags h); 128/256 partitions not fully replicated; …"
                                    //   or the /health body + "(garage json-api: <why>)" on fallback
      "connectedNodes": 3, "knownNodes": 3,
      "storageNodes": 3, "storageNodesUp": 3,      // Garage v2.3.0 field name
      "partitions": 256, "partitionsQuorum": 256, "partitionsAllOk": 256,
      "resyncQueue": 0, "resyncErrors": 0,         // -1 when unknown
      "tableSyncQueue": 0,          // sum of tableStats insert/merkle/gc queues (metadata sync), -1 when unknown
      "layoutVersion": 47,
      "nodes": [{                   // GetClusterStatus + GetNodeStatistics, cli-json only; down nodes first
        "id": "3f2a…", "hostname": "garage-0", "zone": "dc1", "tags": ["host-1"],
        "kubeNode": "worker-1",     // D9: node of the pod with that hostname, "" when none
        "storage": true,            // holds a layout role; only those count as "down"
        "up": true, "lastSeenSecs": -1,            // -1 when up or unknown (Garage forgets long-gone nodes)
        "draining": false,
        "dataAvail": 0, "dataTotal": 0,            // bytes, 0 when unknown
        "resyncQueue": 0, "resyncErrors": 0,        // -1 when that node's stats failed
        "tableSyncQueue": 0, "statsError": ""        // GetNodeStatistics error map entry
      }],
      "source": "cli-json"          // cli-json|health (cli-text is deferred with 1.x support)
    }]
  },
  "cnpg": {
    "error": "",
    "clusters": [{
      "namespace": "db", "name": "pg",
      "phase": "Cluster in healthy state", "phaseReason": "",
      "health": "ok",               // ok|warning|critical (see 01 §CNPG)
      "reasons": [],                // why not ok, for the app to word: noInstance|failover|instances|
                                    //   switchover|notReady|archiving|backupFailed|backupStale
      "instances": 3, "readyInstances": 3,
      "currentPrimary": "pg-1", "targetPrimary": "pg-1",
      "hibernated": false,          // cnpg.io/hibernation=on: health "idle", no reasons
      "instancePods": [{ "name": "pg-1", "node": "worker-1", "phase": "Running", "role": "primary", "ready": true }],
                                    // D9: instance pods (cnpg.io/podRole=instance); node "" while Pending
      "archiving": "ok",            // ok|failing|off|unknown (off = no WAL archiver configured: neutral)
      "lastBackup": "ok",           // ok|failed|stale|none (plugin ObjectStore first, see 01 §CNPG)
      "backupMethod": "plugin",     // plugin|in-tree|none
      "objectStore": "garage-store",// plugin ObjectStore name, "" otherwise
      "scheduled": true,            // a non-suspended ScheduledBackup targets it
      "lastSuccessfulBackupAt": 0, "lastFailedBackupAt": 0, "firstRecoverabilityAt": 0
    }]
  }
}
```

A section key is **absent/null** when the system isn't installed. An **empty list with no
error** means it's installed but has nothing in it. A **non-empty `error`** means the
system was detected but couldn't be read; the UI shows it inline in that tab only.

## Reference cluster (the user's)

The plans are designed against the user's real setup. Its GitOps config lives in a
private repo on the user's machine; HANDOFF.md says where. Specific names are left out
because this repo is public. Shape, from a read-only survey on 2026-10-03:

- **One Talos cluster**, with apps deployed by ArgoCD.
- **Garage v2.3.0, two independent clusters in two namespaces:**
  - a Helm-chart **DaemonSet** on 7 nodes (replication 3, lmdb), with the S3 and admin
    ports on *different* Services (the admin one is not the S3 one)
  - a standalone single-node **StatefulSet** (replication 1) on NFS, which is the Longhorn
    backup target
  - container `garage`, image `dxflrs/amd64_garage:v2.3.0`, binary `/garage` (the user's
    own runbook execs `/garage …`)
- **Longhorn 1.12.0** (default StorageClass), with an S3 backup target on Garage.
- **CloudNativePG** chart 0.28.3 plus plugin-barman-cloud v0.13.0.
  - **29 Clusters**, 2–3 instances each, *all* with plugin backups (one ObjectStore
    each), 28 weekly ScheduledBackups. No Poolers.
  - 2 clusters set `isWALArchiver: false` on purpose.
- **No exec/proxy blockers**: no Kyverno, Gatekeeper or NetworkPolicy in the Garage
  namespaces; PodSecurity is `privileged` there; the Cilium host firewall allows ingress
  from the cluster.
- Possible next targets (not in scope): the Dragonfly operator, the CSI
  snapshot-controller, an offline-backup CronJob.

## Risks and open questions

- **Exec from the phone.** Ichor has no exec code yet. A minimal WebSocket exec client
  (`kube_exec.go`, on `golang.org/x/net/websocket`, already a dependency) has to be
  written and tested. It's the biggest new piece in phase 1, so build it first.
- **Garage CLI version.** The user runs v2.3.0, so `garage json-api` (JSON, since v2.0.0)
  is the target, and it's verified live: no token needed, logs on stderr, and
  `GetNodeStatistics` takes `{"node":"*","body":null}`. 1.x support (text parsing) is
  deferred.
- **Exec can be refused** (RBAC, PodSecurity admission, a policy engine such as Kyverno).
  The `/health` service-proxy fallback still gives healthy/degraded/unavailable.
- **Longhorn API version.** Use the group's `preferredVersion` from `/apis` (v1beta2 on
  Longhorn ≥ 1.3, v1beta1 before). Fields read are present in both.
- **Response size.** Large Longhorn installs have hundreds of replicas. `kubeMaxBody` is
  32 MiB, so it's fine. Request only what's needed; there's no field selector for CRDs.
- **Privacy/screenshot mode.** Output passes through `maskResult` like the other Kube*
  calls. Check that PVC names and namespaces don't leak context names (add a privacy test).
