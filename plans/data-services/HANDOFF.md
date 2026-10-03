# Handoff: data services (Longhorn / Garage / CloudNativePG)

For the next session or agent that picks this up. Read [README.md](README.md) (decisions
plus wire format), then the plan of the phase you're doing.

## State on 2026-10-03

- Planning done. No code written yet.
- Branch with the plans: `worktree-plan-data-services`.
- User decisions:
  - **Garage status comes from its CLI run inside the Garage container**
    (`garage status` / `garage stats`, and `garage json-api` on v2). Exec runs through the
    Kubernetes API, with the `/health` service proxy as a fallback. The earlier admin-token
    idea (phase 1b) is dropped.
  - **Background alerts (phase 4) are opt-in**, off by default.
  - **The user's Garage is v2.3.0**, built with `kubernetes-discovery`, `metrics`, `lmdb`,
    `sqlite`, `fjall`, `k2v` and more. So `garage json-api` is the path to build; 1.x is
    deferred. Garage may also register `garagenodes.deuxfleurs.fr` objects, which helps
    detection.
- Research done:
  - **Longhorn**: CRDs `longhorn.io` volumes/replicas/nodes. Volume `status.state`
    (creating/attached/detached/attaching/detaching/deleting) and `status.robustness`
    (healthy/degraded/faulted/unknown).
  - **CNPG**: `postgresql.cnpg.io/v1` clusters. `status.phase`, `readyInstances`,
    `currentPrimary`/`targetPrimary`, conditions `Ready`, `ContinuousArchiving`,
    `LastBackupSucceeded`.
  - **Garage**: no CRDs.
    - Since v2.0.0, `garage json-api <Endpoint>` runs admin API endpoints from the CLI.
      `GetClusterHealth` returns `status`, `connectedNodes`, `knownNodes`, `storageNodes`,
      `storageNodesOk`, `partitions`, `partitionsQuorum`, `partitionsAllOk`.
      `GetClusterStatus` returns nodes, the layout and staged changes.
    - `block_resync_queue_length` (non-zero is normal) and `block_resync_errored_blocks`
      (should be 0) are the "sync" signals. `garage stats` shows them as "resync queue
      length" and "blocks with resync errors".
    - The image is a static `/garage` binary on scratch: no shell.
    - Admin port 3903 `/health` needs no auth (200, or 503 without quorum). This is the fallback.
- Codebase facts used (verified 2026-10-03):
  - Kube calls: `go/talosmobile/kube_client.go` (`withKube`, `kubeClient.get`). No exec code exists yet.
  - `golang.org/x/net` is a direct dependency (for `x/net/websocket`).
  - Function template: `kube_workloads.go`
  - Demo: `kube_demo.go`
  - App catalog ids: `appcatalog.json` (`longhorn`, `garage`, `cloudnative-pg`)
  - Android plumbing: `TalosRepository.kubeCall`, `LoadingViewModel`, `OverviewScreen` items
  - iOS: `TalosClient.swift:188`, `OverviewView.swift`

## Order of work

1. **Phase 1, Go** ([01-go-core.md](01-go-core.md)). Start with `kube_exec.go` and the
   Garage path against the user's real cluster (`just probe dataservices garage --raw`):
   it's the least certain part, and its output becomes the test fixtures. Then Longhorn
   and CNPG.
2. **Phases 2 and 3** in parallel (separate worktrees/agents): [02-android.md](02-android.md),
   [03-ios.md](03-ios.md).
3. **Phase 4** ([04-monitoring.md](04-monitoring.md), opt-in), then optionally phase 5
   (support bundle and AI context).

One PR per phase, conventional commits (`feat: ...`).

## Kick-off prompts

Paste one of these into a fresh session in `/home/cyril/personal/ichor`:

- Phase 1:
  > Implement phase 1 of plans/data-services (read README.md, HANDOFF.md, 01-go-core.md).
  > Start with kube_exec.go and the Garage CLI path. TDD: write the tests first. Stop after
  > `just test` is green and `just probe dataservices --raw` runs; report what the real
  > cluster returned and commit its Garage output as fixtures.
- Phase 2:
  > Implement plans/data-services/02-android.md on top of the merged phase 1. Verify with
  > the demo cluster and `just test` + `just i18n-check`.
- Phase 3:
  > Implement plans/data-services/03-ios.md on top of the merged phase 1. Verify with
  > `just ios-test-linux` + `just i18n-check`.
- Phase 4:
  > Implement plans/data-services/04-monitoring.md (Android + iOS), opt-in and off by default.

## Reference cluster and fixtures

The user's cluster config (GitOps, ArgoCD) is at `~/hacklab/talos`. Its `AGENTS.md`
says which kubeconfig to use. Read it for the namespaces and workload names, which are
kept out of this public repo on purpose; the shape is in
[README.md §Reference cluster](README.md#reference-cluster-the-users).

Before writing parsers, capture real outputs as fixtures. These commands are read-only,
but they touch the live cluster, so **ask the user before running them**. Fill in the
placeholders from `~/hacklab/talos`:

```sh
# Garage: main (DaemonSet) and NAS (StatefulSet)
kubectl -n <garage-ns> exec ds/<garage-ds> -c garage -- /garage json-api --help
kubectl -n <garage-ns> exec ds/<garage-ds> -c garage -- /garage json-api GetClusterHealth
kubectl -n <garage-ns> exec ds/<garage-ds> -c garage -- /garage json-api GetClusterStatus
kubectl -n <garage-ns> exec ds/<garage-ds> -c garage -- /garage json-api GetNodeStatistics '{"node":"*"}'   # syntax to confirm via --help
kubectl -n <nas-ns> exec sts/<nas-sts> -c garage -- /garage json-api GetClusterHealth
# Longhorn 1.12 / CNPG + plugin
kubectl get volumes.longhorn.io,replicas.longhorn.io,nodes.longhorn.io,backuptargets.longhorn.io -A -o json
kubectl get clusters.postgresql.cnpg.io,scheduledbackups.postgresql.cnpg.io,objectstores.barmancloud.cnpg.io -A -o json
```

**This repo is public.** Trim fixtures to a few objects and replace real namespaces,
names, hostnames, IPs, bucket names and node ids with neutral ones (`garage`, `db`,
`node-1`…) before committing them under
`go/talosmobile/testdata/{garage/v2.3.0,longhorn/1.12,cnpg}/`.

## Questions for the user

1. ~~Garage version~~ → v2.3.0 (answered 2026-10-03).
2. ~~Garage namespace/pods~~ → found in `~/hacklab/talos` (two instances).
3. ~~Exec blockers~~ → none in the repo (no Kyverno/Gatekeeper/NetworkPolicy in the Garage namespaces).
4. OK to run the read-only fixture commands above against the live cluster?

## Progress log

Append a line per session: date, phase, what was done, what's next.

- 2026-10-03: plans written (README, 01–04, HANDOFF). Next: phase 1.
- 2026-10-03: Garage switched to in-container CLI via Kubernetes exec (user's suggestion);
  admin-token phase 1b dropped; alerts confirmed opt-in.
- 2026-10-03: user runs Garage v2.3.0 → json-api only (GetClusterHealth, GetClusterStatus,
  GetNodeStatistics node=*); 1.x text parsing deferred; garagenodes CRD added as a hint.
- 2026-10-03: surveyed ~/hacklab/talos. Garage = a 7-node DaemonSet + a standalone StatefulSet; Longhorn 1.12 with a Garage backup target; 29 CNPG clusters, all
  plugin backups (barman-cloud v0.13.0) → CNPG backup health now comes from ObjectStore
  `serverRecoveryWindow` + ScheduledBackups, and `isWALArchiver: false` is neutral.
  Next: fixtures (with the user's OK), then phase 1.
