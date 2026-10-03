# Handoff: data services (Longhorn / Garage / CloudNativePG)

For the next session or agent that picks this up. Read [README.md](README.md) (decisions
plus wire format), then the plan of the phase you're doing.

## State on 2026-10-03

- Planning done. No code written yet.
- Branch with the plans: `worktree-plan-data-services`.
- Research done:
  - **Longhorn**: CRDs `longhorn.io` volumes/replicas/nodes. Volume `status.state`
    (creating/attached/detached/attaching/detaching/deleting) and `status.robustness`
    (healthy/degraded/faulted/unknown).
  - **CNPG**: `postgresql.cnpg.io/v1` clusters. `status.phase`, `readyInstances`,
    `currentPrimary`/`targetPrimary`, conditions `Ready`, `ContinuousArchiving`,
    `LastBackupSucceeded`.
  - **Garage**: no CRDs.
    - Admin port 3903: `/health` needs no auth (200, or 503 when there's no quorum).
    - `/metrics` exposes `block_resync_queue_length` and `block_resync_errored_blocks`.
    - The admin API `GetClusterHealth` (token) returns `status`, `connectedNodes`,
      `knownNodes`, `storageNodes`, `storageNodesOk`, `partitions`, `partitionsQuorum`,
      `partitionsAllOk`.
- Codebase facts used (verified 2026-10-03):
  - Kube calls: `go/talosmobile/kube_client.go` (`withKube`, `kubeClient.get`)
  - Function template: `kube_workloads.go`
  - Demo: `kube_demo.go`
  - App catalog ids: `appcatalog.json` (`longhorn`, `garage`, `cloudnative-pg`)
  - Android plumbing: `TalosRepository.kubeCall`, `LoadingViewModel`, `OverviewScreen` items
  - iOS: `TalosClient.swift:188`, `OverviewView.swift`

## Order of work

1. **Phase 1, Go** ([01-go-core.md](01-go-core.md)). Do Garage discovery against the
   user's real cluster early (`just probe dataservices garage`): it's the least certain part.
2. **Phases 2 and 3** in parallel (separate worktrees/agents): [02-android.md](02-android.md),
   [03-ios.md](03-ios.md).
3. **Phase 4** ([04-monitoring.md](04-monitoring.md)), then optionally phase 5 (support
   bundle and AI context).
4. Phase 1b (Garage token) only if the header-forwarding check passes and the user wants it.

One PR per phase, conventional commits (`feat: ...`).

## Kick-off prompts

Paste one of these into a fresh session in `/home/cyril/personal/ichor`:

- Phase 1:
  > Implement phase 1 of plans/data-services (read README.md, HANDOFF.md, 01-go-core.md).
  > TDD: write the mapping tests first. Stop after `just test` is green and
  > `just probe dataservices` runs; report what the real cluster returned.
- Phase 2:
  > Implement plans/data-services/02-android.md on top of the merged phase 1. Verify with
  > the demo cluster and `just test` + `just i18n-check`.
- Phase 3:
  > Implement plans/data-services/03-ios.md on top of the merged phase 1. Verify with
  > `just ios-test-linux` + `just i18n-check`.
- Phase 4:
  > Implement plans/data-services/04-monitoring.md (Android + iOS).

## Questions for the user (answer before or during phase 1)

1. How is Garage deployed on your cluster (official Helm chart, or an operator)? Which
   namespace/service, and is `metrics_token` set?
2. Do you want the optional Garage admin-token support (phase 1b)?
3. Should background alerts (phase 4) be on by default for new installs, or opt-in as planned?

## Progress log

Append a line per session: date, phase, what was done, what's next.

- 2026-10-03: plans written (README, 01–04, HANDOFF). Next: phase 1.
