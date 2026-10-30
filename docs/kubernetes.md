# Kubernetes screens

Part of the [Ichor documentation](../README.md#documentation).

## Workload logs

**Logs of all pods** on a Deployment, StatefulSet or DaemonSet reads the logs of its pods through
the Kubernetes API (`get pods/log`), like `kubectl logs -l`, and merges them by time with a
coloured chip per pod. Type in the filter to keep the lines holding that text, whatever the case,
or turn on `.*` for a regular expression (Go syntax), pick a time range (15 min to 24 h, or all)
and the lines per pod, then copy or share the result as a file. The footer says how many lines
matched of those read, and "truncated" when a limit was reached: the first 20 pods, 2,000 lines
per pod, 4 MiB in all, keeping the newest. A pod that cannot answer (still starting, its node
down) shows no lines; a refused `pods/log` shows the refusal. Only what the kubelet still keeps
can be read. A pod's own log sheet has the same filter and time range.

## Image vulnerability scan

**Scan images** on an app's sheet (and on a Talos node's Images screen, and on the Summary of a
Pod, Deployment, StatefulSet or DaemonSet in the Kubernetes browser) looks for known
vulnerabilities in the images it runs, by digest, with Trivy or Grype (Settings → Kubernetes →
*Vulnerability scanner*, Grype by default) in a short-lived Job of the `ichor-imagescan`
namespace: the cluster downloads the vulnerability database and the image layers, not the phone.

- **Pinned scanners:** `aquasec/trivy` and `anchore/grype` (its `-debug` variant, which has a
  shell) are pinned by digest; the pod runs restricted, without a service account token, and only
  gets the pull credentials of the registries scanned.
- **Same report either way:** findings, filters and the SARIF, CycloneDX, HTML, CSV and JSON
  exports do not depend on the scanner. "New since the last scan" compares a scan with the last
  one by the same scanner, since the two name their findings differently.
- **Trivy Operator:** when it runs in the cluster, its reports are read instead, without a Job.
- **Kept reports:** completed scans stay on the phone, encrypted, for 90 days. The app's sheet
  says "Last scan 3 days ago · 2 new" and opens the report without scanning again; the report
  marks and filters what is new. **Activity → Scan reports** lists, opens and deletes them.

## CronJobs

**Kubernetes → CronJobs** lists every CronJob on a card: its icon and name, schedule (and time
zone), when it runs next or that it is suspended, the state of its latest run, a strip of its
recent runs (a hollow bar is a manual one) and how long the last one took. Running and failed
ones come first; tap a card for its recent runs.

**Run now** creates a Job from the CronJob's template after a confirmation, exactly like
`kubectl create job --from=cronjob/NAME`: named `NAME-manual-xxxxx`, annotated
`cronjob.kubernetes.io/instantiate: manual` and owned by the CronJob, so its history limits and
cleanup apply. A suspended CronJob can be run once without resuming its schedule; the
confirmation warns when a run is already in progress, since a manual run ignores the
`concurrencyPolicy`.

On iOS, swipe a workload, CronJob or node row for its first action and long-press it for the
rest: a workload swipes right to Restart and long-presses to Restart, Scale…, Share and Pin; a
CronJob swipes right to Run now and left to Suspend or Resume; a node swipes left to Cordon (or
Uncordon) and Drain…. Each opens the same confirmation as its button, and VoiceOver lists the
swipe actions in its actions rotor.

When kube-green has the namespace asleep, a workload sheet says so on the row (with its wake-up
time) and warns before scaling a workload or resuming a CronJob, since kube-green restores the
replicas it saved at the next wake-up and re-suspends the CronJob at the next sleep; a cluster
without kube-green shows nothing new.

## Rollout history, undo and pause

A workload's sheet lists its revisions, like `kubectl rollout history`, for Deployments (their
ReplicaSets) as well as StatefulSets and DaemonSets (their ControllerRevisions): each with its
images, age and change cause, the current one marked. Tapping an older revision opens what a
rollback to it would undo: the images from → to on top, then the pod template diff (the live
template against the revision's, masked like the YAML views in screenshot mode). **Roll back to
revision N** asks for the workload's name typed, then replaces the pod template like
`kubectl rollout undo --to-revision=N`, follows the rollout in the status sheet and records the
action in the log. A StatefulSet or DaemonSet with `updateStrategy: OnDelete` takes the template
but replaces no pod: the sheet says "pods update when deleted". The current revision has nothing
to undo.

A Deployment also has **Pause rollout** / **Resume rollout** next to Restart, like `kubectl
rollout pause|resume`: while paused, a change to its template waits, the rollout status says
"paused", and a restart or a rollback is refused until it resumes. The demo cluster lists sample
revisions with a diff and refuses both actions. RBAC: `list` on `replicasets.apps` or
`controllerrevisions.apps` for the history, `patch` on the kind for a rollback or a pause; a
kubeconfig without it shows the refusal before the tap.

**Kubernetes → Workloads → kube-green → a schedule → Edit schedule** moves it: the days, the sleep and
wake-up times ("HH:mm", cron syntax kept) and the time zone the times are in, written with them
so a time is never saved without its zone. "No wake-up" (sleep until changed by hand) is an
explicit switch, off by default. Values are checked on the phone before anything is sent; an
Argo CD app that self-heals the SleepInfo is named, since it would put the old schedule back.
kube-green has no "sleep now" or "wake up now": the schedule is the only thing changed.

Tune how a CronJob looks with these optional keys, as labels or annotations (an annotation wins;
the title and description hold spaces, so they must be annotations):

| Key | Value | Default |
|---|---|---|
| `ichor.levis.name/icon` | An icon name: one of the app's bundled logos (`postgresql`, `longhorn`, `velero`…) or any [Dashboard Icons](https://github.com/homarr-labs/dashboard-icons) slug, downloaded only when *Download missing app icons* is on in Settings | The app its image belongs to, as on the Apps screen, else a clock |
| `ichor.levis.name/title` | A display name, e.g. `Database backup` | The CronJob's name |
| `ichor.levis.name/description` | One line on what it does | None |
| `ichor.levis.name/trigger` | `"false"` hides **Run now** (a job that must only run on schedule); the Go core refuses it too | Manual runs allowed |

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: db-backup
  labels:
    ichor.levis.name/icon: postgresql
  annotations:
    ichor.levis.name/title: Database backup
    ichor.levis.name/description: Dumps the shop database to S3
spec:
  schedule: "0 3 * * *"
  # …
```

Or on an existing CronJob:

```sh
kubectl -n shop label cronjob db-backup ichor.levis.name/icon=postgresql
kubectl -n shop annotate cronjob db-backup ichor.levis.name/title="Database backup"
kubectl -n shop label cronjob wipe-staging ichor.levis.name/trigger=false
```

An icon name that is not a lowercase slug (letters, digits, dashes) is ignored. The next run is
computed on the phone from the schedule and `timeZone` (UTC without one, like the controller on
Talos); the app shows none for a schedule it cannot read.

## Cluster checkup

**Kubernetes → Checkup** reads a dozen things nobody opens a screen for and sums them up: a
verdict, then one card per area with its findings, worst first, each worded with what to do and
Kubernetes' own message. The capacity card draws what the pods of each node request against
what it offers (the scheduler's view, not live usage); the volumes card the fill level of every
claim, read from each node's kubelet; the nodes card the roles, taints and labels; the Helm card
the releases at their latest revision, each one opening its history, values, notes and manifest,
where a failed upgrade can be rolled back.

On a large cluster the first read takes a while: the screen shows what it is reading meanwhile,
step by step (pods, nodes, claims, then each card), with a bar that fills as they finish and the
kubelets asked so far for the volumes. `just probe checkup` prints the same steps to stderr.

- **Thresholds:** a volume warns at 85 % and is critical at 95 % (inodes 90 / 98 %), a node at
  90 % of its allocatable CPU or memory requested, a quota at 90 %. A pod counts as stuck after
  5 minutes Pending or 10 minutes starting; a deletion after 10 minutes; a certificate request
  after 10 minutes without an answer; a Helm `pending-*` after 10 minutes.
- **Deprecated APIs** come from the API server's own `apiserver_requested_deprecated_apis`
  metric: what was requested since it started, not by whom (the audit analysis on the API server
  screen names the client). Critical when the next minor release removes the version.
- **Webhooks:** a `failurePolicy: Fail` webhook whose Service has no ready endpoint refuses every
  request it matches, cluster-wide; `Ignore` silently skips it. Webhooks called by URL are not
  checked.
- **Network test leftovers:** an `ichor-netperf-*` namespace Ichor created and could not delete
  (the app was closed mid-test) shows under stuck deletions after 30 minutes, with a Delete
  button; only such a namespace can be deleted from there.
- **Room to drain:** a rough sum of requests over the schedulable nodes, without affinities: a
  note, never an alert.
- **Crossplane:** a managed resource or composite whose `Synced` condition failed and a package
  (Provider, Configuration, Function) not installed or not healthy are critical; a resource not
  ready after 15 minutes and a definition not established are warnings. A paused resource is
  not a finding. Since it runs at every background check, it reads at most 2,000 managed
  resources of each kind (the Crossplane screen reads them all): past that, a problem further
  down the list is not seen. Condition messages are cut to 300 characters.
- **Web address certificates:** the certificate each host of an Ingress tls entry (or the rule
  hosts behind its wildcard), a Gateway HTTPS listener that names a host, or a Traefik
  IngressRoute serves, read from a TLS handshake (port 443, a Gateway listener's own port;
  what a browser sees, dialled straight from the phone; never from the Secret, so no private
  key reaches the phone). Expired is
  critical; expiring within 14 days or not naming the host (a Secret the proxy cannot find
  serves its default certificate) are warnings. A certificate cert-manager writes is left to
  its own alerts. Up to 40 hosts per check, each read at most every 30 minutes; a host the
  phone cannot reach says nothing. The app sheet's and the Services screen's web addresses
  show the same badge.
- **Tailscale:** with the Tailscale operator installed, an operator Deployment with replicas not
  ready, a Connector whose `ConnectorReady` failed (named with its role: subnet router, exit
  node, app connector) and a ProxyGroup with no proxy available are critical. An operator scaled
  to zero, a Connector the operator has not reconciled yet, a Connector or ProxyGroup naming a
  ProxyClass that does not exist, a ProxyGroup with some proxies down and a ProxyClass that is
  not ready while something uses it are warnings. One still reconciling a changed spec is not a
  finding. "Open Tailscale" leads to the screen.
- **Cluster lifecycle:** on a management cluster running Cluster API or Sidero Metal, a workload
  cluster that failed, a Machine that failed and a Sidero server in use but powered off are
  critical; a cluster still provisioning after an hour, a Machine stuck in a provisioning phase
  (or in Deleting) for over 30 minutes, a MachineDeployment short of available workers (not
  while it scales) and a server waiting to be accepted (five named, then a count) are warnings.
  Each fix line names the `clusterctl` or `kubectl` command; "Open Cluster lifecycle" leads to
  the screen.
- **Plugins:** the findings of every plugin the cluster runs, under the plugin's name and worded
  by the plugin; tapping one opens the plugin's screen. A plugin the account may not read is
  named in the card's error; External Secrets stays in the secrets card. See
  [docs/plugins.md](plugins.md#findings).
- **Alerts:** Settings → Alerts → Cluster checkup runs it at every background check (it lists every
  pod and asks each kubelet). A critical finding notifies at once, a warning when seen on two
  checks in a row, a cleared one once; a section that could not be read keeps what it knew.
- **Hardware sensors:** a node's *About this node* screen shows its temperatures (with each
  sensor's max and critical limits), fans, per-core CPU frequency with the governor, the CPU's
  thermal throttle counters and its PCI devices, read with `os:reader` from `/sys/class/hwmon`,
  the thermal zones and `CPUFreqStats` (Talos 1.5+). Cores held below 70 % of their top speed
  under the `performance` governor, or a sensor above its max, mark the node *Throttled* there
  and, once that screen was opened, on its row in the node lists. A VM has no Sensors card.
- **Spare parts:** *Find spare parts* next to the system, each memory module and each disk on
  *About this node* opens a page of the Ichor site, in your browser, with the part's
  manufacturer, model or part number and serial number: copy them, open the vendor's support
  or warranty page, or search the web. The app itself contacts no vendor; the details travel
  after the `#` of the address, which browsers keep to themselves.
- **Temperature alert:** Settings → Alerts → Node temperatures (off by default, Talos clusters)
  reads every node's hottest sensor at each background check.
  - **Running hot:** a node at its sensor's critical limit − 5 °C (else its max) notifies at once.
    You can set one temperature for every node instead (60–100 °C).
  - **Back to normal:** said once the node is 3 °C under the limit, so a sensor hovering at the
    limit does not flap.
  - **Throttled:** notifies when seen on two checks in a row.
  - **Tapping it:** opens the node's Hardware screen.
- **Disk partitions:** a node's *Storage* screen opens on its disks: per disk a bar of its
  partitions (EFI, META, STATE, EPHEMERAL, user volumes) and the unallocated space, then one row
  per partition with its filesystem, whether it is encrypted, its UUID and size, read with
  `os:reader` from the `block.talos.dev` resources (Talos 1.8+).
- **Rollback:** on a node's upgrade screen, the menu offers `talosctl rollback` (os:admin): the node
  reboots at once into the Talos it ran before its last upgrade, for an upgrade that boots but
  misbehaves. Talos rolls back by itself one that does not boot.
- **Extensions before an upgrade:** once the target version is chosen, the upgrade screen
  checks the node's system extensions against the Image Factory's official list for that
  version. Any extension with no build is named, since the node would come back without it.
  An image from another registry, or a factory that cannot be reached, is said to be
  unchecked.
- **Drain before an upgrade:** a Talos version that upgrades without draining the node first
  (1.18 and later, through the LifecycleService) gets a "Drain the node first" switch on the
  upgrade screen, on by default. The upgrade then runs as a node maintenance: cordon, drain
  (PodDisruptionBudgets honoured), upgrade, wait until the node is back and Ready, uncordon. A
  drain that cannot finish stops before the upgrade is requested and leaves the node cordoned.
- **Pre-pull the image first:** the upgrade screen's "Pre-pull the image on all nodes" downloads
  the installer on every node ahead of the maintenance window (`talosctl image pull`, os:admin),
  three nodes at a time, so each reboot does not wait for the download. A node's Images screen
  can pull any image on every node the same way, into the Kubernetes or the system images. A
  node that cannot pull shows its error; the others go on.
- **Cluster upgrade (cluster menu → Upgrade cluster…, or "Upgrade all" in the rollout):** every
  node to one Talos version, one after the other (os:admin).
  - **Order:** control planes first, the etcd leader last of them (it hands its leadership over
    first), then the workers. Each node keeps its own installer registry and schematic.
  - **Gates:** between nodes, the node must run the new version and be Ready, and etcd must be
    healthy without alarms. Then the cluster settles for a minute. A failed gate pauses the roll
    with its reason, and Resume checks again.
  - **Drain:** "Drain each node first" (on by default) cordons and drains each node before its
    upgrade, for Talos versions whose upgrade does not drain by itself.
  - **Pause and Abort:** both take effect before the next node; a node being upgraded is never
    interrupted. After an abort, the nodes upgraded stay upgraded.
  - **Continue:** a lock on the cluster (a Lease in `kube-system`, renewed while the roll runs)
    keeps two phones from rolling at once. If the app stops mid-roll, the lock expires within
    minutes, and the plan screen offers **Continue**, starting with the nodes left.
  - **In the background:** Android keeps the roll going in a notification ("Upgrading 3/7:
    worker-2, Rebooting") with a progress bar, Pause / Resume and Abort. iOS stops it once
    suspended, so keep Ichor open until the end.
- **Kubernetes upgrade (cluster menu → Upgrade Kubernetes…), like `talosctl upgrade-k8s` (os:admin):**
  - **Versions offered:** the latest patch of the minor the cluster runs and of the next one,
    within what every node's Talos supports. Another version can be typed, inside the same limits.
  - **The plan:** each control plane's API server, controller manager, scheduler and kube-proxy
    images, then each node's kubelet, current version → new. Also the deprecated APIs still in
    use (critical when the target removes them), and the blockers.
  - **Dry run first (on by default):** each node checks its change without applying it; the
    upgrade is then one tap away. Only the image fields change, and a change that would need a
    reboot is refused.
  - **The run:** one control plane at a time, waiting for its static pods to run the new version
    and be Ready, then each kubelet, then the kube-proxy DaemonSet.
  - **Cancel:** stops after the node being changed. The nodes done keep the new version, and
    starting again continues with the others.
  - **Going back:** there is no undo; returning to the previous version is another upgrade.
- **Adding a node from the phone (overview menu → Add a node…, os:admin):** boot the new machine
  from the Talos ISO and type the address its console shows. "Add a node…" is the one entry for
  growing a Talos cluster; on an Omni cluster it opens the boot media directly, and Omni adds the
  machine once it boots with the join arguments.
  - **Booting it (Add a node… → No machine yet? Boot a new machine):** the phone cannot serve a network boot
    itself (DHCP and TFTP need privileged ports), so it shows what does, for the cluster's Talos
    version and Image Factory schematic: the `siderolabs/booter` command (docker or podman) for a
    Linux laptop of the machine's network, the ISO, the iPXE URL, and the kernel, initramfs and
    command line for a PXE server of your own. Each has Copy, Share and a QR code. On an Omni
    cluster it adds the join kernel arguments, which carry the join token (hidden in privacy
    mode).
  - **Maintenance mode:** the node is read over the maintenance API (disks, links, version), which
    has no authentication: the phone must be on the same LAN or on a VPN that reaches it, and a
    public address is refused.
  - **Its config:** made in memory from a member of the same role (its secrets, CAs and tokens),
    with the new host name, install disk and, optionally, a static address. Only a redacted
    preview is shown, and nothing secret is stored on the phone.
  - **What is sent, and how:** after the host name is typed (and the app lock passed), the full
    config goes once, over TLS without client authentication, to the address you typed, as
    `talosctl apply-config --insecure` does. It is never retried, nor sent to another address.
  - **Following it:** send → install → reboot → booted → joining (Talos ready, the Kubernetes node
    Ready) → added to the context's nodes. A control plane also waits for its etcd member to be
    healthy, and etcd counts one more member. After 15 minutes the run stops and says what to
    check (console, disk, network). Stopping only stops following: the install cannot be undone.
  - **Host names:** a host name a cluster member already has is refused, since the run could end
    up following that member. To replace a dead node that cluster discovery still lists, turn on
    "Replaces a node with this host name": the old member's addresses are then never followed.
- **Long runs in the background (Android):** an upgrade, a cluster or Kubernetes upgrade, a
  maintenance, a node join, a config try or apply, an image pull or scan goes on after you leave
  the app, each in its own notification of the one **Operations** channel. It shows the step and,
  where the run counts (nodes, images), a progress bar. Its buttons act from the shade: **Stop**,
  **Pause** / **Resume** and **Abort** for a cluster upgrade, **Stop following** for what cannot be
  stopped, **Keep** / **Revert now** for a try. With the app lock on, Stop, Abort and Keep open the
  run's screen instead. Tapping the notification opens the run's screen.
- **Live Activities (iOS):** the same runs show on the Lock Screen and in the Dynamic Island: the
  step, a bar where the run counts, a config try's countdown, the same buttons, then the result
  (gone 15 minutes later). With the app lock on, the activity names no node, version or cluster.
  iOS updates it only while Ichor runs: once Ichor is suspended it shows the last step and when
  it was true, and the "Keep Ichor open" notification still asks you to come back. With Live
  Activities turned off in Settings › Ichor, nothing changes.

## Capacity

**Kubernetes → Capacity** (also linked from the checkup's capacity card) answers "can one more fit,
and what goes if a node goes down?":

- **Free room** for the cluster and per node: CPU, memory and pod slots left by requests (the
  scheduler's view), with each node's memory requests and live usage as two bars when
  metrics-server answers, and a mark on a node whose pods' memory limits exceed it.
- **Disk:** ephemeral storage left by requests, and each node's free disk as its kubelet reports
  it, with a DiskPressure or "disk low" (under 15% free) mark; a workload near its ephemeral
  storage limit, which the kubelet evicts once it goes over, is flagged. With Settings → Alerts →
  Node storage on, the background monitor also alerts when a node's disk crosses the warning or
  critical threshold or the node reports DiskPressure, on clusters added from a kubeconfig too.
- **N+1:** whether the pods of any one node would fit on the others, or which node would leave
  pods Pending.
- **Workloads, the first to go first:** lowest priority first, those with no PriorityClass flagged,
  each with its numeric priority, QoS class, risk (OOM-killed or near its memory or ephemeral storage limit, evicted
  first as BestEffort, no memory limit) and how many more replicas of its size fit today.
- **What if a node goes down:** a node, several picked together, or the worst one, is lost in a
  simulation of the scheduler: where its pods land, which lower priority pods they preempt, which
  stay Pending, then what to scale down, lowest priority first, within PodDisruptionBudgets and
  never a `system-*` class. A suggestion opens the object's scale dialog; nothing is scaled from
  this screen. It is an estimate by requests: affinity, topology spread and volume node affinity
  are not modelled, and the sheet says so.

## Policies

**Kubernetes → Policies** shows the admission policies of the cluster, Kyverno policies and OPA
Gatekeeper constraints in one list, to answer "is a policy blocking this rollout, and can I relax
it?" in two taps:

- **The engines:** each Kyverno or Gatekeeper controller ready or not, and what its webhook does
  while the engine is down: `Fail` refuses every request it covers (an outage blocks
  deployments), `Ignore` lets them through unchecked. With both engines, each row says which one
  it belongs to.
- **The policies:** not ready ones first, then those in **Enforce** mode (a request that breaks a
  rule is refused) before **Warn** (Gatekeeper: it is let through with a warning) and **Audit**
  mode (it is let through and only reported), each with its failing, warning and passing counts
  from the policy reports, or a constraint's violations from Gatekeeper's last audit. A search narrows the
  list, and a namespace keeps its own policies and the cluster-wide ones that checked something
  there (or are not ready), each with its counts in that namespace.
- **One policy:** its rules, why it is not ready, and the newest resources it fails with
  Kyverno's message; a Pod, Deployment or other well-known kind opens its object screen. A
  Gatekeeper constraint shows its kind, when the audit last ran, and the violations Gatekeeper
  kept (20 by default, its `--constraint-violations-limit`) out of the total.
- **Audit or Enforce:** a Kyverno policy's sheet switches it to **Audit** (to let a hotfix
  through) and back to **Enforce**, its rules' own `failureAction` with it. Audit asks you to type
  the policy's name; Enforce asks a plain confirm. When Argo CD or Flux applies the policy, the
  confirmation says its next sync puts the previous mode back. A policy whose rules mix the two modes is
  warned that every rule takes the new one, and that switching back does not restore the mix.

A cluster without either engine says so; credentials that cannot read the policy reports still show
the policies, with their counts at zero and the reason.

## Autoscaling

**Kubernetes → Autoscaling** shows everything that scales the cluster, to answer "is this service
stuck at its max, is KEDA paused, what does the recommender want, is the Karpenter pool at its
limit?" in two taps:

- **Needs attention:** every HPA at its max or limited, a paused or failing KEDA ScaledObject, a
  VPA without a recommendation yet, a pool at its limit, and a pending, drifted or disrupting
  NodeClaim — each opening its detail sheet.
- **Horizontal (HPA):** where the replica count sits between its bounds, the metrics it scales on
  (current against target), and the AbleToScale / ScalingActive / ScalingLimited conditions. An
  HPA KEDA created is linked to its ScaledObject instead of shown twice.
- **KEDA:** the ScaledObjects with their triggers (a query, a topic, a cron window — never a
  secret), their fallback, and the pause annotations; the ScaledJobs next to them.
- **Vertical (VPA):** per container, the live requests against what the recommender suggests,
  with the lower and upper bounds. No recommendation yet says so, not zero.
- **Node pools (Karpenter):** each pool's usage against its limits, its disruption budgets, and
  its NodeClaims with their instance type and state.

A group the cluster does not run (KEDA, VPA, Karpenter) collapses to one "not installed" line;
credentials that cannot read one group show the reason in that group's header only.

The workload sheet names the autoscaler that owns the replica count — "HPA web-hpa · 3 of
1–5", or "KEDA worker-so · paused at 0" — with a tap through to the Autoscaling screen, and
the list marks autoscaled workloads with an "auto" badge.

**Pause and resume KEDA, raise an HPA ceiling.** The two fixes made at 3 a.m. are one tap from
the sheets. On a ScaledObject, **Pause at…** sets `autoscaling.keda.sh/paused-replicas` (KEDA
scales the workload to that count and stops), **Hold current count** sets
`autoscaling.keda.sh/paused: "true"`, and **Resume scaling** removes both. On an HPA, **Edit
min/max** sets `minReplicas` and `maxReplicas`; Save stays off while max is below min, and min
cannot go under 1. An HPA KEDA created is not edited: KEDA would put its bounds back, so the sheet
points to the ScaledObject's `minReplicaCount` / `maxReplicaCount` instead.

## Virtual machines

**Kubernetes → Virtual machines** shows the KubeVirt virtual machines of the cluster ("Harvester"
in the title on a Harvester cluster), to answer "which VM is down, where does it run, is it
still importing its disk?" and act on it from the phone:

- **The list:** counts by state (running, stopped, failing, migrating, importing), then each VM
  failing first with KubeVirt's status, its node, first address and guest OS, and a line for a
  disk import ("Importing win11-root 62.00%") or a live migration ("Migrating worker-2 →
  worker-3"). It follows a VM on its way (starting, stopping, migrating) every few seconds.
- **A VM's sheet:** run strategy, resources, addresses, conditions, disks and the latest
  migration, then **Start**, **Stop**, **Restart**, **Pause** / **Unpause** and **Migrate**,
  each enabled only when the VM's state allows it and the credentials may. Stop and restart ask
  for the VM's name typed; stop can skip the guest's grace period, and says when it writes
  `runStrategy: Halted` into the spec (a GitOps tool may set it back on its next sync).

- **Its serial console:** **Console** on a running VM's sheet opens the guest's serial console
  in the app's terminal, like `virtctl console` (get on `virtualmachineinstances/console` in
  subresources.kubevirt.io). The guest must send its console to ttyS0 (or hvc0); one viewer at
  a time, so opening it elsewhere (another phone, `virtctl console`) disconnects the app. On
  Android the console keeps running when you leave the screen, like a pod's shell; on iOS it
  ends with the screen. Each open is in the audit log (`vm-console`).

A cluster without KubeVirt says so.

## Crossplane

**Kubernetes → Crossplane** (the button appears when the cluster runs Crossplane) shows the
control plane's packages (providers, configurations, functions, with their Installed and Healthy
state and the pull error), the APIs its XRDs define (scope, claim kind, compositions), and every
composite resource, claim and managed resource with its Ready and Synced conditions — problems
first, with a filter and a search, and the Synced message (the cloud API's error) on the row.
Tapping a row opens the object's summary (conditions, events, YAML).

A composite or managed resource's menu (Android) or swipe and long-press actions (iOS) offer
three reversible actions, each a plain confirmation and an entry in the activity log:

- **Pause** (`crossplane.io/paused: "true"`): the provider stops calling the cloud API while a
  `ReconcileError` is looked into. A paused resource cannot be deleted, and pausing a composite
  leaves its composed resources reconciling.
- **Resume** removes the annotation.
- **Reconcile now** (`crossplane.io/reconcile-requested-at`) reconciles it at once instead of at
  the next poll. Providers built on crossplane-runtime 2.3+ (and Crossplane 2.3+ for composites)
  handle the annotation and record it in `status.lastHandledReconcileAt`; older ones do not read
  it, but the annotation changing is still an update their watch reacts to, so the request queues
  a reconcile there too, without that status line. The app says "requested", not "done".

The actions are off, with the reason, for credentials that may not patch the object (`os:admin`).
A claim is changed through its composite; deleting stays with the object screen's delete.

## Tailscale

**Kubernetes → Tailscale** (the button appears when the cluster runs the Tailscale operator)
shows the operator, every connector (subnet routes, exit node, app connector, tailnet IPs and
devices), proxy group (type, devices, the kube-apiserver URL), proxy class (and who uses it) and
the tailnet nameserver — problems first, with the operator's reason. Tapping a row opens the
object's summary; nothing is changed here (os:admin).

## Cluster lifecycle (Cluster API, Sidero Metal)

**Kubernetes → Cluster lifecycle** (the button appears on a management cluster that runs
Cluster API or Sidero Metal) shows the workload clusters with their phase, control plane and
workers ready of desired, endpoint and Kubernetes versions; their machines grouped by cluster,
control plane first, with phase, node, version, addresses and a "stuck since" when one has not
moved for half an hour; the machine deployments with their replicas; and, with Sidero Metal,
the bare-metal servers (accepted, cordoned, in use, clean, power, server classes, CPU and
memory) and how many servers each server class has free. Problems come first, with the failure
or the conditions that are not True. Tapping a row opens the object's summary; nothing is
changed here (os:admin).

## Argo CD app icons

Ichor names an Argo CD app's logo from its Helm chart, its name or the images it runs. An app it
cannot recognise, such as a personal project or an internal service, gets a monogram; give it a
logo with the `ichor.levis.name/icon` annotation on the Application:

| Value | Example | Shown |
|---|---|---|
| An icon name: a bundled logo or any [Dashboard Icons](https://github.com/homarr-labs/dashboard-icons) slug | `grafana` | The bundled logo, else downloaded from jsDelivr when *Download missing app icons* is on |
| An `https://` link to a PNG, WebP, JPEG or GIF | `https://git.example.org/logo.png` | Downloaded when *Download missing app icons* is on, then kept in the app's cache |
| The image inline, base64, 64 KiB at most | `data:image/png;base64,iVBORw0…` | Always: nothing is downloaded |

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-site
  namespace: argocd
  annotations:
    ichor.levis.name/icon: https://git.example.org/my-site/logo.png
spec:
  # …
```

Or on an existing app:

```sh
kubectl -n argocd annotate application my-site ichor.levis.name/icon=https://git.example.org/my-site/logo.png
# inline: no request at all, the logo travels with the Application
kubectl -n argocd annotate application my-site \
  ichor.levis.name/icon="data:image/png;base64,$(base64 -w0 logo.png)"
```

The annotation wins over Ichor's own guess. A label of the same name works too, but label values
are short, so a label can only hold an icon name. An invalid value (plain `http://`, a link with a
password, SVG, which neither app can draw, or an image over 64 KiB) is ignored and the app keeps its
usual icon. A downloaded logo is cached under its URL: change the URL (e.g. `?v=2`) to refresh it.
Only the link itself is requested, without cookies and without following redirects; the server it
points to sees the phone's IP address.
