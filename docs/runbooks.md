# Ichor runbooks

A runbook is an ordered list of the app's own actions, written once in calm and run step by
step when it matters: "scale the ingest API to 0, run the compaction CronJob, scale it back to
2, snapshot etcd". The Go core loads it, plans it (what each step does, whether it is
destructive, the role it needs and whether the cluster allows it) and runs it, stopping at
the first failed step. Each destructive step waits for its own confirmation before it runs.
Every step calls the function the matching button calls, so it keeps that button's
permission check, its refusal in the demo cluster and its entry in the Activity log; the run
adds one more entry, `runbook`, with the number of steps that ran.

On the phone, the cluster menu's **Runbooks…** lists the cluster's runbooks. Import one by
pasting it, from a file or from an https link; the app shows its plan before keeping it and
never runs it on import. Run asks before each destructive step (the node's hostname for a
node step), follows the run in the notification (Android) or a Live Activity (iOS), and can
be stopped. A step the cluster's permissions refuse keeps Run off until you let the run go
past it ("continue on error").

Runbooks cannot do anything the app cannot already do: there are no conditions, loops or
new actions. The Go API is `RunbookSummary`, `RunbookPlan` and `StartRunbook`
(`go/ichorgo/runbook.go`, `runbook_run.go`); from a computer, `just probe runbook-plan FILE
-context CTX` prints the plan and `just probe runbook-run FILE -yes -context CTX` runs it
(`-yes` is required when a step is destructive, and confirms each such step).

## Format

```yaml
schemaVersion: ichor.dev/runbook/v1alpha1
name: Restart ingestion            # required
description: Scale down, restart.  # optional
steps:                             # 1 to 50
  - id: scale-down                 # required, unique: errors and the run name it
    action: scale                  # one of the actions below
    target: {kind: Deployment, namespace: demo-shop, name: ingest-api}
    params: {replicas: 1}
    continueOnError: false         # true: a failure of this step does not stop the run
```

Unknown keys, unknown actions, a missing target and a parameter the action does not take
are errors that name the step. A Kubernetes action takes `target.namespace` and
`target.name` (and `target.kind` when it lists kinds); a node action takes `target.node`
only (an address or hostname of the talosconfig).

## Actions

| Action | Target | Params | Destructive | Role |
|---|---|---|---|---|
| `scale` | kind `Deployment` (default) or `StatefulSet` | `replicas` (required) | yes | os:admin |
| `rolloutRestart` | kind `Deployment` (default), `StatefulSet` or `DaemonSet` | | yes | os:admin |
| `suspendCronJob` | a CronJob | `suspend` (default `true`; `false` resumes) | no | os:admin |
| `triggerCronJob` | a CronJob | | no | os:admin |
| `argoSync` | an Argo CD Application | | yes | os:admin |
| `fluxReconcile` | kind `Kustomization` (default), `HelmRelease`, `GitRepository`, `OCIRepository`, `HelmRepository` or `Bucket` | | no | os:admin |
| `etcdSnapshot` | node | | no | os:operator, os:etcd:backup or os:admin |
| `nodeReboot` | node | `mode`: `default`, `powercycle` or `force` | yes | os:operator or os:admin |
| `nodeMaintenance` | node | `action`: `reboot` (default), `shutdown` or `none`; `includeBare`; `acknowledge` | yes | os:admin |

- The role is the Talos client role of a talosconfig cluster; on a kubeconfig cluster the
  Kubernetes actions are checked against its RBAC only, and the node actions are refused.
  On both, the plan asks the cluster's RBAC (SelfSubjectAccessReview) as the buttons do.
- `argoSync` and `fluxReconcile` wait until the sync or reconcile ends: a step is done when
  Argo CD reports Succeeded or Flux reports Ready.
- `etcdSnapshot` writes the snapshot into the app's private data directory, under
  `snapshots/`; the step's message gives the path. The file holds every Secret in clear.
- `nodeMaintenance` cordons and drains the node first, as the Maintenance screen does; its
  `acknowledge` confirms the warnings that screen asks about (it never overrides a blocker).

## Examples

Pause ingestion, compact, resume:

```yaml
schemaVersion: ichor.dev/runbook/v1alpha1
name: Compact the ingest store
steps:
  - {id: stop-ingest, action: scale, target: {namespace: demo-shop, name: ingest-api}, params: {replicas: 0}}
  - {id: compact, action: triggerCronJob, target: {namespace: demo-shop, name: store-compaction}}
  - {id: start-ingest, action: scale, target: {namespace: demo-shop, name: ingest-api}, params: {replicas: 2}}
```

Snapshot etcd, then reboot a worker after draining it:

```yaml
schemaVersion: ichor.dev/runbook/v1alpha1
name: Reboot worker 1
steps:
  - {id: snapshot, action: etcdSnapshot, target: {node: 10.5.0.2}}
  - {id: reboot, action: nodeMaintenance, target: {node: 10.5.0.5}, params: {action: reboot}}
```
