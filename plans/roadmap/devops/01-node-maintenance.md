# D1. Node maintenance: cordon → drain → reboot → wait Ready → uncordon

Status: **missing**. Size L. Read [../README.md](../README.md) for the conventions.

## What exists today

- `power.go`: `Reboot(mode default|powercycle|force)` and `Shutdown(force)` through `nodeAction`
  and `validatePowerTarget`. README "Safety features": a `default` reboot does **no cordon/drain**.
- `upgraderequest.go`: the legacy `MachineService.Upgrade` drains on its own; the
  `LifecycleService` path (Talos ≥ v1.18 once the legacy call is gone) does **not**
  (`noDrainWarning`). So upgrades lose their drain too.
- The node's `unschedulable` flag is only read: `diagnose_collect.go` `fetchKubeNodeState` (Talos
  `k8s.NodeStatus`, no kube API needed) and the Argo network health.
- Nothing posts an Eviction or reads PodDisruptionBudgets.
- Reusable: `kubeMutate`, `k.patch`, `k.post`, `kubeMutationError` (`kube_client.go`), `listPods`
  (`kube_pods.go`), `observeNode` + `upgradeTracker` (`upgraderun.go`) for "rebooting → back",
  `computePlan` etcd-quorum checks (`upgradeplan.go`), `takeUpgradeLock` (a Lease, so maintenance and
  upgrades never overlap), the `UpgradeProgress` timeline (Android) and `UpgradeJob` (iOS),
  `UpgradeService` (Android foreground service).

## Goal

From a node's action sheet: **"Maintenance…"** opens a plan, then a run screen that walks the node
through the steps with live progress, and says exactly what blocks when something blocks.
Two standalone actions come with it: **Cordon / Uncordon** and **Drain**.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| M1 | **Drain like `kubectl drain`**: cordon, then POST `policy/v1` `Eviction` for each pod on the node, skipping DaemonSet pods and mirror (static) pods; pods with `emptyDir` are listed as "loses local data" in the plan; bare pods (no controller) need an explicit tick. | Same semantics as the tool people know; the Eviction API respects PDBs. |
| M2 | **PDB-aware, never forced.** A `429` from the Eviction means a PDB blocks: retry every 5 s and show "waiting for PDB `x` (0 disruptions allowed)" with the pods involved. No delete fallback in v1. | A phone at 3 am must not take a database below quorum. The user can stop and decide. |
| M3 | **Pre-flight plan** (`NodeMaintenancePlan`): pods to evict grouped by owner, PDBs that currently allow 0 disruptions, emptyDir/bare pods, etcd impact for a control plane (reuse `computePlan`), whether another node is already cordoned or under maintenance (the Lease). Blockers vs warnings like the upgrade plan. | Same shape the upgrade plan already has, so the UI pattern is reused. |
| M4 | **The run is one Go handle** `StartNodeMaintenance(…, listener)` → `MaintenanceRun.Cancel()`, phases `cordon`, `drain` (n/m pods), `action` (reboot/shutdown/upgrade/none), `waiting` (node back, Talos ready, `k8s.NodeStatus` Ready), `uncordon`, `done`. Cancel stops after the current step and leaves the node cordoned (said clearly). | The Listener + Run pattern of `StartUpgrade`; one progress JSON for both apps and for U2 live progress. |
| M5 | **Action after drain** is a choice: reboot (default mode), shutdown (no wait/uncordon, the node stays cordoned), upgrade (hands over to `runUpgrade` with the chosen image), or **none** (just drain; "uncordon" later from the sheet). | Covers reboot, hardware work and upgrades with one flow. |
| M6 | **Held under the upgrade Lease** (`takeUpgradeLock`, holder "maintenance:<node>"). | Prevents two phones, or a maintenance and an upgrade, from draining two nodes at once. |
| M7 | **Gates**: cordon/drain need os:admin (kubeconfig); the reboot step os:operator. The plan screen explains a missing role up front. | Existing role model. |
| M8 | **The upgrade gets the drain back**: when the upgrade goes through `LifecycleService`, `UpgradeScreen` offers "drain first" (default on), which runs this flow with action = upgrade. Removes `noDrainWarning` in that case. | Fixes a real regression path before Talos v1.18 removes the legacy call. |

## Go (`go/ichorgo`)

| File | Content |
|------|---------|
| `kube_nodes.go` | `KubeCordon(config, ctx, kubeServer, node string, on bool)`: merge patch `{"spec":{"unschedulable":on}}` on `/api/v1/nodes/<name>`; Talos node → kube node name via `nodenames.go`. |
| `kube_drain.go` | `drainPlan(ctx, k, node)` (list pods with `fieldSelector=spec.nodeName=`, owners, PDBs `/apis/policy/v1/poddisruptionbudgets`, match PDB selectors with the existing selector code in `kube_netpol_selector.go`), `evictPod` (POST `…/pods/<p>/eviction`), `drain(ctx, k, plan, emit)` with retry/backoff on 429 and a wait until evicted pods are gone (or `terminationGracePeriod` + 30 s). |
| `maintenance.go` | `NodeMaintenancePlan(...) (json)`, `StartNodeMaintenance(config, ctx, kubeServer, node, action, image string, includeBare bool, listener MaintenanceListener) *MaintenanceRun`. Reuses `observeNode`/`upgradeTracker` for the wait and `takeUpgradeLock`. |
| `maintenance_demo.go` | Demo plan (one PDB blocking, one emptyDir pod) and a simulated run. |
| tests | `httptest` API server: eviction 201/429/404, DaemonSet and mirror pods skipped, PDB matching, cancel leaves the node cordoned, Lease conflict. |
| probe | `just probe maintenance-plan <node>`, `just probe drain <node> --action none`. |

Wire (progress):

```jsonc
{ "phase": "drain", "message": "evicting 7 of 12 pods",
  "pods": [{ "namespace": "db", "name": "pg-1", "owner": "Cluster/pg", "state": "blocked",
             "reason": "PDB pg-primary allows 0 disruptions" }],
  "elapsedMs": 41000 }
```

## Android

- `ui/node/NodeMenu.kt`: add `MAINTENANCE`, `CORDON`/`UNCORDON` (label from current state).
- New `ui/maintenance/`: `MaintenancePlanScreen.kt` (blockers/warnings like `UpgradeScreen`),
  `MaintenanceRunScreen.kt` reusing `UpgradeProgress` timeline rows plus a pod list with states.
- Run in a foreground service: generalise `UpgradeService` into an `OperationService` hosting
  either run (keeps one ongoing-notification implementation, feeds U2).
- Confirmation: typed hostname + auth (`HostnameConfirm.kt`), as for reboot.
- Overview node row: a "cordoned" chip (data already in `k8s.NodeStatus`).

## iOS

- `NodeActionsMenu` entries; `MaintenanceView.swift` (plan + run), run kept alive like
  `UpgradeJob` (`beginBackgroundTask` + "keep open" notification).
- `IchorCore/Maintenance.swift` models; `TalosClient.swift` calls.

## Phases

1. Go: cordon, drain plan, drain, run, demo, tests, probe. (M)
2. Android + iOS: cordon/uncordon actions and the cordoned chip. (S)
3. Android + iOS: plan and run screens, service. (M)
4. Upgrade "drain first" (M8). (S)

## Open questions

1. Bare pods: allow with a tick (proposed) or refuse?
2. Default action: reboot (proposed) or none?
