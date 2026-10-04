# D2. Small kubectl actions

Status: **partial**. Size M. Read [../README.md](../README.md) for the conventions.

## What exists today

| Action | Where |
|--------|-------|
| Rollout restart + live status | `KubeRolloutRestart` (`kube_workloads.go`), `KubeRolloutStatus` (`kube_rollout.go`); Android `RolloutStatusSheet.kt`, iOS `RolloutStatusSheet.swift` |
| Delete pod | `KubeDeletePod` (`kube_pods.go`); Workloads `PodsTab.kt` |
| Run a CronJob now | `KubeTriggerCronJob` (`kube_cronjobs.go`) |
| Container logs, tail + follow | `containerlogs.go`, `StartContainerLogFollow`, **through Talos (CRI by container ID)**, opened from Node → Pods only |
| Scaled-down / suspended state | shown only (`SCALED_DOWN`, `CronJobCard.kt` "Suspended") |

Missing: **scale**, **CronJob suspend/resume**, **Deployment rollback to a revision**,
**previous container logs**, **logs from the Workloads pods list**.

## Plan

| Action | Go function | Kubernetes call | Notes |
|--------|-------------|-----------------|-------|
| Scale | `KubeScale(cfg, ctx, server, kind, ns, name string, replicas int)` | `PATCH …/<kind>/<name>/scale` merge `{"spec":{"replicas":n}}` | Deployments and StatefulSets only (DaemonSets have no scale). Bounds 0–100 in UI; scaling to 0 needs the typed name. Warn when an HPA targets it (read `autoscaling/v2` HPAs: "the HPA will change this back"). Warn when Argo CD manages it with self-heal (owner annotations already read by the Argo code). |
| Suspend / resume CronJob | `KubeSuspendCronJob(…, ns, name string, suspend bool)` | merge `{"spec":{"suspend":b}}` | Same Argo self-heal warning. |
| Rollback Deployment | `KubeDeploymentRevisions(…)` (json list) + `KubeRollbackDeployment(…, ns, name string, revision int)` | list ReplicaSets owned by the Deployment, `deployment.kubernetes.io/revision` annotation; patch the Deployment's `spec.template` with the RS template minus `pod-template-hash` (what `kubectl rollout undo` does) | List shows revision, age, images, change-cause annotation. After the patch, open the existing rollout status sheet. |
| Previous logs | `KubePodLogs(…, ns, pod, container string, previous bool, tailLines int)` | `GET …/pods/<p>/log?container=&previous=&tailLines=` with `k.getText` (pattern in `kube_netperf_pods.go`) | Talos CRI only keeps the current container; the kube API keeps the last terminated one. Shown as "Previous run" toggle when `restartCount > 0`. Parsed with the existing `logparse*.go`. |
| Logs from Workloads | — | — | `PodsTab.kt` / iOS `PodsView` rows get "Logs": current logs through the existing Talos follow (node + container ID are in the pod status), previous through `KubePodLogs`. |

All mutations: `validateKubeName`, `findWorkloadKind`, `kubeMutate`, `privacy.reveal`, demo refuses.

## UI

- Workload sheet (Android `ui/workloads`, iOS `Workload*`): a **Scale** stepper (current →
  new, Apply), **History** section with "Roll back to revision N", for Deployments.
- CronJob card: **Suspend / Resume** next to "Run now".
- Pod row: **Logs**; log screen: **Previous run** toggle with the termination reason
  (`terminatedReason` already computed in `kube_pods.go`).

## Phases

1. Go: the five functions, demo data (a Deployment with 3 revisions, a crash-looping pod with
   previous logs), tests, probe. (M)
2. Android. (S–M)
3. iOS. (S–M)

## Open questions

1. Scale limit 100 enough? (Proposal: free entry, confirm above 2× current.)
