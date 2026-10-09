# D4. Rolling Talos upgrade, extension check, Kubernetes upgrade

Status: **partial**. Size L. Depends on D1 (drain and run screen). Read [../README.md](../README.md).

## What exists today (one node at a time)

- `upgradeplan.go` `UpgradePlan`/`computePlan`: blockers and warnings (etcd quorum, peer health,
  lock, only-endpoint risk), `UpgradeVersionCheck`/`versionRisk`.
- `upgraderun.go` `StartUpgrade` → `UpgradeRun` (`Cancel` only stops following), phases
  requested → installing → rebooting → waiting → booted → done; `followUpgrade`, `observeNode`.
- `upgraderequest.go`: legacy `MachineService.Upgrade` (drains) or `LifecycleService` (no drain).
- `upgradelock.go`: a cluster Lease so only one upgrade runs.
- `upgradeimage.go` keeps registry, repo and the Image Factory schematic; `releases.go` lists Talos releases.
- UI: Android `ui/upgrade/*` (foreground `UpgradeService`), iOS `UpgradeView.swift` + `UpgradeJob.swift`.
- **Rollout plan** (client side, both apps): the upgrade chooser groups outdated nodes under
  "Control plane · 1/3 done" / "Workers · 0/5 done" with each node's state, and one
  "Upgrade next" button picks the next node: a failed one to retry, then the **control plane
  first**, then the workers. The user still starts each node; Go does not drive the roll yet.
- Not in README.

Gaps: no whole-cluster roll, no health gate between nodes, no pause, extensions not checked against
the target version, no Kubernetes upgrade.

## Goal

**"Upgrade cluster…"** from the Overview: pick the version, see the order and the checks, then
watch Ichor upgrade every node one by one, stopping by itself if something looks wrong.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| R1 | **Order** (to settle: the shipped rollout plan goes control plane first, while this plan said workers first): workers first (by name), then control planes, the etcd leader last (forfeit leadership first with `EtcdForfeitLeadership`). Already-upgraded nodes are skipped (resumable). | Workers are the cheapest to lose; the leader last avoids two elections. |
| R2 | **Gate between nodes** = the existing `computePlan` for the next node (no blocker) **plus** the previous node: Talos running the target version, `k8s.NodeStatus` Ready, etcd healthy (all members, no alarm), no new pods Pending/CrashLoop since start in namespaces the user marked important (optional). Settle time 60 s. | Reuses what is already proven per node; adds "it really came back". |
| R3 | **Pause / resume / abort between nodes.** Abort never interrupts a node mid-upgrade (cannot be undone safely); it stops before the next. The Lease is held for the whole roll. | Clear, safe semantics. |
| R4 | **Drain each node** through D1's drain when the upgrade path does not drain itself. | Removes `noDrainWarning`. |
| R5 | **Extension check before starting**: the node's `ExtensionStatus` (`hardware.go` `mapExtensions`) vs the Image Factory schematic of the target image. Query `factory.talos.dev` `GET /version/<v>/extensions/official` to warn when an installed extension has no build for the target version. Custom factories: use the image's registry host. | The classic upgrade trap (missing NVIDIA/iscsi-tools extension). |
| R6 | **One Go handle** `StartClusterUpgrade(…, listener)` with nested progress (`node i/n` + the existing per-node phases). The run stays in Go so both apps and the foreground service share one state machine. | Same as `StartUpgrade`. |
| R7 | **Kubernetes upgrade later (phase 4)**: `talosctl upgrade-k8s` is client-side orchestration (machinery `pkg/cluster/kubernetes`). Evaluate calling it directly from gomobile (it patches the machine configs of control planes, then kubelets); gated by D3's apply code. | Big, but the library exists. |

## Go

| File | Content |
|------|---------|
| `upgradecluster.go` | `ClusterUpgradePlan(cfg, ctx, server, image)` (order, per-node plan, extension report), `StartClusterUpgrade(…, image, opts, listener) *ClusterUpgradeRun` with `Pause/Resume/Abort`. |
| `upgradeext.go` | schematic → extensions; Image Factory client (HTTP, short timeout, cached per version; offline → "could not check"). |
| tests | order, skip done nodes, gate failure stops, abort between nodes, extension mismatch. |

## UI

- Overview menu: **Upgrade cluster…** (os:operator for Talos, os:admin for the drain/Lease).
- Plan screen: version picker (`TalosReleases`), the node order as a list with each node's
  checks, extension warnings, "drain each node" switch.
- Run screen: a vertical list of nodes (pending / upgrading with the existing per-node timeline /
  done / failed), Pause, Abort; ongoing notification "Upgrading 3/7: worker-2 rebooting" (U2).
- Document the single-node and cluster upgrades in README.

## Phases

1. Go cluster run + gates + tests. (M)
2. Extension check. (S)
3. Android + iOS screens (reuse D1's run screen). (M)
4. Spike then build Kubernetes upgrade. (L)

## Open questions

1. Settle time between nodes configurable? (Proposal: 60 s fixed, "skip wait" button.)
