# Data services: Longhorn, Garage and CloudNativePG health

Status: **phases 1–4 implemented** (Go core, Android, iOS, opt-in alerts); device checks and optional phase 5 remain. Pick up from [HANDOFF.md](HANDOFF.md).

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
| D1 | **Detection is two-stage.** (1) Free hint: the Overview already loads the app inventory from Talos container images (`AppsViewModel`, catalog ids `longhorn`, `garage`, `cloudnative-pg` in `go/ichorgo/appcatalog.json`). (2) Confirmation: Go checks the API groups (`GET /apis`) for `longhorn.io` and `postgresql.cnpg.io`, and finds Garage Services/StatefulSets. | Kubernetes is only called when a hint exists, so no admin kubeconfig is signed for clusters that don't need one. The `/apis` check covers operators whose images the catalog misses (mirrored registries). |
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
      "lastBackupAt": 0,            // unix ms, 0 = never
      // From the volume's engine (engines.longhorn.io status), 0-100:
      "rebuildProgress": 0,         // slowest replica rebuild, with rebuilding > 0
      "backingUp": false, "backupProgress": 0,
      "restoring": false, "restoreProgress": 0,
      "scheduleError": "",          // message of a False "Scheduled" condition
      "tooManySnapshots": false     // "TooManySnapshots" condition
    }],
    "backupTargets": [{ "name": "default", "url": "s3://longhorn-backups@garage/",
                        "available": true, "message": "" }],
    "nodes": [{ "name": "worker-1", "namespace": "longhorn-system", "ready": true, "schedulable": true,
                "allowScheduling": true, "evictionRequested": false, // node spec, set by KubeLonghornAction
                "replicas": 4,      // replicas the node holds
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

Dragonfly (phase 5, operator `dragonflydb.io/v1alpha1`, detected by its API group like Longhorn
and CNPG; pods selected with `app.kubernetes.io/name=dragonfly`, instance from `app`, role from
`role`):

```jsonc
  "dragonfly": {
    "version": "v1alpha1", "error": "",
    "instances": [{
      "namespace": "app", "name": "cache",
      "phase": "Ready",             // the operator's own word (Ready, Rolling-update…)
      "health": "ok",               // critical: noReady/noMaster; warning: pods/masters/notReady
      "reasons": [],                // noReady|noMaster|masters|pods|notReady
      "replicas": 2, "readyPods": 2,
      "master": "cache-0",          // pod with role=master, "" when none
      "pods": [{ "name": "cache-0", "node": "worker-1", "phase": "Running", "role": "master", "ready": true }]
    }]
  }
```

Alerts (phase 4 rules): `noReady`/`noMaster` → critical; `pods`/`masters` → warning; a rolling
update (`notReady`) doesn't alert.

CAST AI (Workload Autoscaler, `autoscaling.cast.ai/v1`, detected by its API group; one
`Recommendation` per managed workload, listed across namespaces; the requests before CAST AI
come from the `autoscaling.cast.ai/first-seen-container-resources` annotation, the apply mode
from `autoscaling.cast.ai/recommendation-apply-mode`). Android only for now; the Swift model
ignores the section.

```jsonc
  "castai": {
    "version": "v1", "error": "",
    "cpuDeltaMilli": -430, "memoryDeltaBytes": -268435456, // recommended minus original, summed
    "compared": 2,                                         // workloads whose originals are known
    "recommendations": [{
      "namespace": "shop", "name": "api-deployment",
      "kind": "Deployment", "workload": "api",   // spec.targetRef
      "mode": "deferred",                        // immediate|deferred|""
      "readOnly": false,                         // spec.applyPolicy.readonly
      "health": "ok",                            // critical: vpa; warning: hpa/readOnly
      "reasons": [],                             // vpa|hpa|readOnly
      "message": "",                             // the failing condition's message or the read-only reason
      "containers": [{ "name": "api", "cpu": "120m", "memory": "640Mi", "cpuLimit": "1", "memoryLimit": "1Gi",
                       "originalCpu": "500m", "originalMemory": "1Gi" }],
      "cpuDeltaMilli": -380, "memoryDeltaBytes": -402653184
    }]
  }
```

Alerts: `vpa` (the recommendation cannot be applied) → critical; `hpa` and `readOnly` are
shown, not alerted.

MariaDB (operator `k8s.mariadb.com/v1alpha1`, detected by its API group; `mariadbs`, `backups`
and `physicalbackups` when served; pods selected with `app.kubernetes.io/name=mariadb`, cluster
from `app.kubernetes.io/instance`, StatefulSet pods `<cluster>-<n>` only; role from
`status.currentPrimary`). A scheduled logical backup's last run comes from its CronJob
(`batch/v1`, listed only when one exists): the Complete condition's transition time stays put
while runs keep succeeding.

```jsonc
  "mariadb": {
    "version": "v1alpha1", "error": "",
    "clusters": [{
      "namespace": "app", "name": "shop",
      "topology": "replication",    // standalone|replication|galera
      "health": "ok",               // critical: noReady/noPrimary; warning: the others; idle: spec.suspend
      "reasons": [],                // noReady|noPrimary|pods|galeraRecovery|backupFailed|backupStale|notReady
      "suspended": false,
      "message": "",                // the Ready condition's message when it is not True
      "replicas": 2, "readyPods": 2,
      "primary": "shop-0",          // status.currentPrimary, "" when none
      "pods": [{ "name": "shop-0", "node": "worker-1", "phase": "Running", "role": "primary", "ready": true }],
                                    // role: primary|replica|member (Galera)
      "lastBackupAt": 1759460640000,       // ms, latest success over its backups, 0 = never
      "lastBackupFailedAt": 0,             // ms, latest failure; failed when after the success
      "backupSchedule": "0 3 * * *"        // most frequent active cron; stale past 2× its interval
    }]
  }
```

Alerts (phase 4 rules): `noReady`/`noPrimary` → critical; `galeraRecovery`/`backupFailed`/
`backupStale` → warning; a replica rolling out (`pods`) or the operator busy (`notReady`) doesn't
alert.

Percona XtraDB Cluster (operator `pxc.percona.com/v1`, detected by its API group; clusters from
`perconaxtradbclusters`, backups from `perconaxtradbclusterbackups` by `spec.pxcCluster`; pods
selected with `app.kubernetes.io/name=percona-xtradb-cluster`, cluster from
`app.kubernetes.io/instance`, members where `app.kubernetes.io/component=pxc`):

```jsonc
  "percona": {
    "version": "v1", "error": "",
    "clusters": [{
      "namespace": "db", "name": "shop",
      "state": "ready",             // the operator's own word (ready|initializing|paused|stopping|error|unknown)
      "message": "",                // the operator's messages, "; "-joined
      "crVersion": "1.18.0",
      "paused": false,              // spec.pause: health idle, no reasons
      "health": "ok",               // critical: error/noMember; warning: the others; idle when paused
      "reasons": [],                // error|noMember|members|proxy|initializing|backupFailed|backupStale
      "pxcSize": 3, "pxcReady": 3,  // status.pxc (spec.pxc.size before the operator's first pass)
      "proxy": "haproxy",           // the enabled one: haproxy|proxysql|""
      "proxySize": 2, "proxyReady": 2,
      "pods": [{ "name": "shop-pxc-0", "node": "worker-1", "phase": "Running", "ready": true }],
      "lastBackupAt": 1767225600000,       // latest Succeeded backup (status.completed), 0 if none
      "lastBackupFailedAt": 0,             // latest Failed backup (completed, else created), 0 if none
      "backupSchedules": [{ "name": "daily", "schedule": "0 2 * * *", "keep": 7, "storageName": "s3" }]
    }]
  }
```

`initializing` is only listed when nothing more precise (`members`, `proxy`) explains it: the
operator says initializing whenever a pod is not ready. `backupFailed`: the latest failure is
newer than the latest success. `backupStale`: no success for twice the most frequent schedule
(counted from the cluster's creation when it never had one).

Alerts (phase 4 rules): `error`/`noMember` → critical; `members`/`backupFailed`/`backupStale` →
warning; `initializing` and `proxy` alone don't alert.

cert-manager (phase 5, `cert-manager.io/v1`, detected by its API group; certificates, issuers
and clusterissuers; one action, a forced renewal). Expiry math uses the read time; certificates aren't on nodes, so
they take no part in the likely-cause correlation:

```jsonc
  "certManager": {
    "version": "v1", "error": "",
    "certificates": [{              // worst first, then the soonest expiry (never issued last)
      "namespace": "app", "name": "web", "secretName": "web-tls",
      "dnsNames": ["web.example.com"], // commonName then dnsNames, the first 5
      "dnsNameCount": 1,            // all of them
      "issuer": "ClusterIssuer/letsencrypt",
      "health": "ok",               // critical: expired, or not Ready within 7 days of notAfter
      "reasons": [],                // expired|expiring|renewalOverdue|notReady|issuer
      "ready": true,
      "issuing": false,             // the Issuing condition is True (a renewal is running)
      "message": "",                // the Ready condition's message when not ready
      "notAfter": 1764547200000,    // unix ms, 0 before the first issuance
      "renewalTime": 1761955200000, // unix ms, 0 when none
      "failedAttempts": 0           // status.failedIssuanceAttempts
    }],
    "issuers": [{                   // not ready first
      "kind": "ClusterIssuer",      // or Issuer (with its namespace)
      "namespace": "", "name": "letsencrypt",
      "type": "acme",               // acme|ca|selfSigned|vault|venafi, "" for another
      "server": "acme-v02.api.letsencrypt.org", // ACME server host only
      "ready": true, "message": "...",
      "health": "ok"                // warning when not ready
    }]
  }
```

Warnings: `notReady` (a first issuance, DoesNotExist/Issuing, included), `renewalOverdue`
(renewalTime passed over an hour ago), `expiring` (under 14 days left and no renewal still
planned: a short-lived certificate renewed hours ahead is fine), `issuer` (its cert-manager
issuer is not ready; external issuer groups are not checked). Alerts: a critical certificate →
critical; `expiring`/`renewalOverdue`/`notReady` → warning; an issuer not ready → warning on its
own key. Keys: `certmanager|<ns>/<name>` for a certificate, `certmanager|Issuer/<ns>/<name>` and
`certmanager|ClusterIssuer/<name>` for an issuer.

`KubeCertManagerRenew(ns, name)` forces a renewal like `cmctl renew`: it adds (or flips) the
`Issuing` condition to True with reason `ManuallyTriggered` on the certificate's status, a merge
patch of `/status` carrying the resourceVersion just read. Refused while it is already issuing
and in the demo. An ACME issuer counts each renewal against its rate limits, so the apps confirm.

`KubeCertManagerDetails(ns, name)` explains a certificate, read on demand (never cached): its
conditions; its latest 3 CertificateRequests (by the `cert-manager.io/certificate-name`
annotation or an owner reference, newest first) with their conditions, their ACME Orders and
those orders' Challenges (`acme.cert-manager.io`, when served; the challenge's `reason` says
why a domain fails validation); the newest 40 core events of every object of that chain; and
the newest 60 lines of the cert-manager controller log (pods labelled
`app.kubernetes.io/name=cert-manager,app.kubernetes.io/component=controller`, else
`app=cert-manager`, last 4000 lines each) naming one of those objects and its namespace. A
missing certificate is an error; any other part that cannot be read is named in `error`.

```jsonc
  {
    "conditions": [{"type": "Ready", "status": "False", "reason": "Failed", "message": "...", "time": 1764547200000}],
    "requests": [{
      "name": "web-2", "created": 1764547200000, "conditions": [...],
      "orders": [{
        "name": "web-2-1234", "state": "pending", "reason": "",
        "challenges": [{"name": "web-2-1234-5678", "type": "HTTP-01", "dnsName": "web.example.com",
                        "wildcard": false, "state": "pending", "presented": true,
                        "reason": "Waiting for HTTP-01 challenge propagation: wrong status code '404', expected '200'"}]
      }]
    }],
    "events": [{"time": 1764547200000, "type": "Warning", "reason": "PresentError", "message": "...",
                "object": "Challenge/web-2-1234-5678", "count": 3}],
    "log": ["E1005 10:00:00.000000 1 sync.go:190] \"propagation check failed\" ..."],
    "error": ""
  }
```

Velero (phase 5, API group `velero.io`, detected by its group but always read at `v1`: the group
also serves `v2alpha1` (DataUpload/DataDownload), which has no schedules, backups or locations).
Three listings: `schedules`, `backups` (only the latest finished one per schedule, from the
`velero.io/schedule-name` label, plus whether one is running, and the failed ones without a
schedule from the last 7 days are kept) and `backupstoragelocations`:

```jsonc
  "velero": {
    "version": "v1", "error": "",
    "schedules": [{
      "namespace": "velero", "name": "daily",
      "schedule": "0 2 * * *", "paused": false,
      "phase": "Enabled",             // New|Enabled|FailedValidation
      "validationErrors": [],
      "health": "ok",                 // critical: failed/location; warning: partiallyFailed/stale/invalid; idle: paused
      "reasons": [],                  // failed|location|partiallyFailed|stale|invalid
      "storageLocation": "default",   // the namespace's default location when the template names none
      "includedNamespaces": ["app"],  // empty or "*": every namespace
      "lastBackup": {                 // latest finished backup, null when none is left
        "name": "daily-20261003020000", "phase": "Completed",  // Completed|PartiallyFailed|Failed|FailedValidation
        "startedAt": 1759456800000, "completedAt": 1759457040000, "errors": 0, "warnings": 1, "failureReason": ""
      },
      "lastSuccessAt": 1759457040000, // latest Completed backup, 0 when none
      "inProgress": true              // a backup of it is New/InProgress/WaitingForPluginOperations*/Finalizing*
    }],
    "adhoc": [{                       // failed/partially failed backups without a schedule, last 7 days, newest first
      "namespace": "velero", "name": "before-upgrade", "phase": "PartiallyFailed",
      "startedAt": 0, "completedAt": 0, "errors": 1, "warnings": 0, "failureReason": "",
      "storageLocation": "default", "health": "warning"
    }],
    "locations": [{
      "namespace": "velero", "name": "default", "provider": "aws", "bucket": "backups", "default": true,
      "phase": "Available",           // Available|Unavailable, "" before the first validation
      "message": "", "lastValidatedAt": 1759492740000,
      "health": "ok"                  // critical when Unavailable
    }]
  }
```

`stale` is no Completed backup within twice the schedule's interval (the same rough
`cronInterval` as CNPG), counted from the schedule's creation when it has none yet (from its last run when
every backup has expired since). Alerts (key
`velero|ns/name`, `velero|BackupStorageLocation/ns/name` for a location): a critical schedule or
an unavailable location → critical;
`stale`/`partiallyFailed`/`invalid` → warning; a failed backup taken by hand is shown, not alerted.

Rook Ceph (phase 5, operator `ceph.rook.io/v1`, detected by its API group; `cephclusters`,
`cephblockpools`, `cephfilesystems` and `cephobjectstores`, plus one pod listing selected with
`app in (rook-ceph-osd,rook-ceph-mon)`, matched to a cluster by namespace, OSD number from
`ceph-osd-id`). An external cluster (`spec.external.enable`) has no pods of its own:

```jsonc
  "ceph": {
    "version": "v1", "error": "",
    "clusters": [{
      "namespace": "rook-ceph", "name": "rook-ceph",
      "phase": "Ready",             // Rook's own word (Ready, Connected when external, Progressing, Failure…)
      "message": "Cluster created successfully",
      "cephHealth": "HEALTH_WARN",  // status.ceph.health; "" before Ceph reported
      "health": "warning",          // critical: healthErr/failure/full/noOSD/noQuorum; warning: the rest
      "reasons": ["healthWarn"],    // healthErr|failure|full|noOSD|noQuorum|healthWarn|nearFull|osds|mons|notReady
      "checks": [{ "name": "MON_DOWN", "severity": "HEALTH_WARN", "message": "1/3 mons down" }], // errors first
      "bytesTotal": 3298534883328, "bytesUsed": 1099511627776, // raw capacity (status.ceph.capacity)
      "osdsUp": 3, "osdsTotal": 3,  // OSD pods ready
      "monsReady": 2, "monsTotal": 3,
      "notReadyNodes": ["worker-3"], // nodes of its OSD and mon pods that are not ready
      "version": "19.2.3-0",        // status.version.version
      "external": false
    }],
    "pools": [{                     // block pools, filesystems and object stores
      "namespace": "rook-ceph", "name": "replicapool",
      "kind": "blockPool",          // blockPool|filesystem|objectStore
      "phase": "Ready",
      "health": "ok"                // critical on Failure, warning when not Ready (Connected)
    }],
    "osds": [{ "namespace": "rook-ceph", "id": "0", "pod": "rook-ceph-osd-0-…", "node": "worker-1", "phase": "Running", "ready": true }]
  }
```

`full`/`nearFull` use Ceph's own default ratios: more than 95% / 85% of the raw capacity used.
`noQuorum` is half the mons or more not ready (Ceph stops answering, its reported health goes
stale); `noOSD` is no OSD pod ready while some exist. `notReady` only shows when nothing else is
wrong. Alerts (phase 4 rules, key `ceph|ns/name`, `ceph|kind/ns/name` for a pool): a critical cluster or a failed pool →
critical; `healthWarn`/`nearFull`/`osds` → warning; a mon down (Ceph's `MON_DOWN` warns anyway)
or a reconcile in progress (`notReady`) doesn't alert on its own. The likely cause counts the
`notReadyNodes` of a cluster that needs attention.

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
