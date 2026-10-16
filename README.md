# Ichor for Kubernetes and Talos Linux

[![Android CI](https://github.com/cyrinux/ichor/actions/workflows/android.yml/badge.svg?branch=main)](https://github.com/cyrinux/ichor/actions/workflows/android.yml)
[![iOS CI](https://github.com/cyrinux/ichor/actions/workflows/ios.yml/badge.svg?branch=main)](https://github.com/cyrinux/ichor/actions/workflows/ios.yml)

**Any Kubernetes cluster from its kubeconfig, and the whole Talos layer, from your phone.**
Ichor is a free, open-source Android and iOS app to monitor and operate Kubernetes clusters:
workloads, pods, logs, Argo CD and Flux, on EKS, GKE, AKS, DigitalOcean, Rancher, Omni or your
own (see [Kubernetes clusters without Talos](#kubernetes-clusters-without-talos)). On
[Talos Linux](https://www.talos.dev) clusters it goes down to the nodes: node health, etcd,
KubeSpan, upgrades, machine config and live graphs in your pocket, and a notification the
moment a node goes down.

Website: <https://cyrinux.github.io/ichor/>

In the app, **Settings → About → Ichor website** opens the project site.
**About → Integrations → Platform → Talos Linux** opens the Talos website.

Want to explore without a cluster? Tap **Try demo** on the import screen.
The built-in demo works offline and shows five sample nodes, live metrics, services,
logs, events, etcd, networking, storage, hardware and resources. The overview marks
it as demo data; cluster changes and credential exports are unavailable. It can
sit alongside your real clusters and be removed from **Manage clusters**.

> Ichor is an independent community project, not affiliated with or endorsed by Sidero Labs.
> Talos is a trademark of Sidero Labs, Inc.

- **Viewing:** node status, services, resources, logs, etcd and the cluster health check.
- **Actions:** reboot or shut down a node, and wake a powered-off one with Wake-on-LAN (set a
  MAC address and, optionally, a broadcast address and port per node, from its action sheet;
  the phone sends the magic packet itself, so it has to reach the node's network). The MACs of
  each node's network cards are recorded on the phone while it is up, so a node that goes down
  before it was set up can still be woken.
- **Kubernetes:** list Deployments, StatefulSets and DaemonSets with their rollout state and
  restart one with a rolling update (`kubectl rollout restart`), then follow it live with its
  old and new pods (`kubectl rollout status`); list pods with their
  `kubectl get pods` status and delete one so its controller starts a new one, or only those of
  a node or of a workload (`--field-selector spec.nodeName=`, the workload's selector); list CronJobs
  with their schedule, next run and recent runs, and run one now (`kubectl create job --from`,
  with an icon and a title of your choice, see [CronJobs](#cronjobs)); measure the
  network between two nodes (TCP throughput and latency, see [Network test](#network-test));
  open an app in the browser from its sheet, at the hosts of the Ingresses, Gateway API
  HTTPRoutes, GRPCRoutes and TLSRoutes and Traefik IngressRoutes whose Services select its pods (kgateway included: its
  routing objects are the standard ones, and the network map's Gateway boxes name the
  controller serving them — kgateway, Traefik, Cilium, Envoy Gateway, Istio, NGINX Gateway
  Fabric); browse any resource, CRDs included, with the
  columns `kubectl get` prints, read and edit its YAML (a dry-run diff first; a change made
  meanwhile is never overwritten), apply pasted manifests like `kubectl apply --server-side`
  (each object previewed as a diff first; a field another manager owns is refused, never taken
  over), list Helm releases and roll one back to an earlier revision
  (a dry-run plan first; a release Flux manages is suspended first), follow a pod's log and
  port-forward to it on the phone (127.0.0.1 only); see what gives LoadBalancer Services their
  address (MetalLB and Cilium pools with their free addresses, MetalLB advertisements and BGP
  peers, kube-vip ranges and leases) and why one has none; or export a kubeconfig.
- **Argo CD:** when the cluster runs it, list its Applications with their health and sync state,
  follow a sync wave by wave, and sync (with prune, dry run, selected resources…), refresh,
  terminate a sync, pause or resume auto-sync, or roll back to an earlier deployment. A sync,
  a Flux reconcile or a rollout restart started from the phone is followed in the background,
  with its progress and Terminate or Stop in the notification (Android) or Live Activity (iOS). Ichor
  patches the Application resources with the admin kubeconfig, like `argocd --core`, so no
  Argo CD token is needed. **Show diff** tells what a sync would change before running it,
  like the Diff tab of the Argo CD UI: Ichor reads the comparison the application controller
  already made (the live and the predicted state of each object, in Argo CD's Redis, through a
  port-forward: nothing runs in the cluster, nothing is written) and renders it object by
  object, Secret values hidden; the revision, each history entry and the running sync link to
  their commit on GitHub, GitLab, Gitea, Forgejo, Codeberg or Bitbucket. An app an ApplicationSet or a parent app manages keeps its spec:
  only sync, refresh and terminate are offered there, since its owner would revert the rest.
  Each app also has a network view, like Argo CD's own but down to the nodes: host → Gateway →
  Ingress/HTTPRoute/GRPCRoute/TLSRoute/IngressRoute → Service → pods → node, every box coloured by health, traffic flowing along
  the healthy paths, and the likely root cause named (a node not ready, a crash-looping pod, a
  Service without ready pods).
  An app the icon catalog does not know (a personal project) can bring its own logo, see
  [Argo CD app icons](#argo-cd-app-icons).
- **Flux:** when the cluster runs Flux v2, list its Kustomizations and HelmReleases with their
  readiness, applied and attempted revision, source, what a Kustomization applied and a
  release's Helm history, and their Git, OCI and Helm sources; reconcile (with its source
  first if wanted), suspend or resume any of them, and force or reset a HelmRelease that gave up.
  The detail shows a HelmRelease's drift as Flux reports it (with **Correct drift**), the
  dependsOn chain as a tree with what waits on it, and the object's events. Like Argo CD, Ichor writes the annotations and `spec.suspend` the `flux` CLI does, with the
  admin kubeconfig: nothing to install in the cluster.
- **Activity log:** every change Ichor made to your clusters (reboots, scaling, syncs,
  rollbacks…) is kept 90 days on the phone, encrypted, with its outcome. Optionally
  (Settings → Privacy, off by default) each Kubernetes object the app changes also gets a
  Kubernetes Event, reason `IchorAction`, so the cluster's own history shows what the phone
  did (`kubectl get events`, the Argo CD timeline); node reboots and Talos changes have no
  object to attach it to and send nothing.
- **Incident summary:** overview menu → Share incident summary… turns the last 6 h, 24 h or 7 days
  of the cluster's history on this phone and the actions taken from it into a Markdown page
  (what is wrong now, timeline, actions, polling gaps), names hidden by default, to share or copy.
- **Network policies:** every NetworkPolicy and, with Cilium, CiliumNetworkPolicy and
  CiliumClusterwideNetworkPolicy, with Calico its NetworkPolicy and GlobalNetworkPolicy
  (selectors in Calico's own language, Allow/Deny/Pass/Log rules, read from its CRDs or its
  API server), in plain words: the pods each one selects, whether they are isolated in each
  direction, and what each rule lets in or out (pods, namespaces, CIDRs, entities, FQDNs,
  ports, HTTP/DNS rules), with each namespace's share of isolated pods.
- **Policies (Kyverno):** every Kyverno policy with its mode (Enforce or Audit), whether it is
  ready, how many resources pass or fail it and which ones, and whether the engine's
  controllers and admission webhook are up. See [Policies](#policies).
- **Virtual machines (KubeVirt / Harvester):** every VM with its status, node, address, guest
  OS and any disk import or live migration in flight, failing ones first; start, stop (with or
  without the grace period), restart, pause and migrate one from its sheet, or open its
  serial console in the app's terminal. See [Virtual machines](#virtual-machines).
- **API server health and pressure:** the Kubernetes API server's readyz and livez checks, and
  what loads it right now: Ichor reads its `/metrics` twice a few seconds apart and shows the
  request rate, 5xx and 429 answers, which clients send the requests (API Priority and Fairness
  flow schemas: nodes, controllers, service accounts…) and how full each priority level is, the
  busiest verbs and resources with their mean latency, the requests waiting in a queue with the
  user that sent them, open watches and the resources with the most objects in etcd. A verdict
  sums it up: healthy, busy, throttling or unhealthy.
  **Who loads it** goes further with the Kubernetes audit log Talos keeps on every control plane
  (read through the Talos API, os:admin): over the last 5 to 60 minutes it names the client at
  fault, service account and program, and says what it does wrong and what to change: lists in a
  loop instead of watching, watches that keep restarting, refused (403) or failing calls, an object
  rewritten every few seconds, creates of objects that exist, slow requests, throttling. When many
  clients share a symptom (5xx, slowness, watches cut at the same interval) it blames the API
  server, etcd or what sits between them instead, and it tells when a control plane's API server
  stopped logging.
- **Cluster checkup:** what no other screen shows, on one screen (Kubernetes → Checkup): pods
  that crash, cannot pull their image, stay Pending or were killed for their memory, failed Jobs,
  the Warning events of the last hour, volumes nearly full (the kubelets' own figures, no
  Prometheus needed), claims not bound or lost, deprecated API versions something still calls
  before the next Kubernetes upgrade, admission webhooks whose Service has no ready endpoint,
  admission policies: an engine that is down, a policy that is not ready, a workload whose pods a policy denies,
  what pods request against what each node offers (and whether one node's pods would still fit
  elsewhere), quotas at their limit, nodes left cordoned or under pressure, LoadBalancer Services
  without an address (and why), empty MetalLB or Cilium pools, MetalLB pools nothing advertises,
  a refused MetalLB configuration and BGP sessions down, namespaces, pods and claims stuck in Terminating,
  certificate requests nobody approved, web address certificates (not cert-manager's) expired,
  expiring or naming other hosts, External Secrets that do not sync, Helm releases
  failed or stuck, and Crossplane: managed resources that cannot sync, unhealthy providers. Every finding says what to change. Read-only; the critical ones can also
  notify in the background (Settings → Alerts). A pod's log sheet and a workload's sheet show
  the object's Kubernetes events, like the end of `kubectl describe`. See
  [Cluster checkup](#cluster-checkup).
- **Live flows (Cilium + Hubble, or Calico 3.30+ + Whisker):** follow the cluster's traffic like
  Hubble UI, or only what is dropped, for a namespace or a pod. With Cilium, Ichor runs
  `hubble observe --follow` in each cilium-agent pod (the CLI ships in the agent image, so nothing
  is installed and Hubble Relay is not needed); with Calico it streams Goldmane's flows through the
  whisker Service (via the API server proxy, nothing to expose), 15-second aggregates with the
  policy trace that decided them. Either way it groups repeated drops, explains the drop reason and
  names the policy behind it: the deny rule that matched, or, for the usual default-deny, the
  policies that isolate the pod. History is what the agents still hold in their flow buffer, or
  what Goldmane still keeps.
- **Node shell without Talos:** on a cluster added from a kubeconfig, a node's menu can open a
  root shell on the node, like `kubectl debug node/`: a privileged pod in the node's namespaces,
  then `nsenter`. Off by default (Settings → Kubernetes → Node debug shell), behind the app lock,
  recorded in the activity log, and the pod is deleted when the shell ends.
- **Tablets and foldables (Android):** on a tablet or an unfolded foldable, the nodes list and
  the Argo CD apps open their node or app side by side with the list.
- **Background:** alerts and a home-screen widget.
- **Data services:** the storage and database operators the cluster runs (Longhorn, Garage,
  CloudNativePG, Dragonfly, MariaDB, Percona, cert-manager, Velero, ExternalDNS, Sealed Secrets,
  Rook Ceph, CAST AI, CSI VolumeSnapshots, backup CronJobs) on one tabbed screen, each with its
  health and what needs a look; kube-green's sleep schedules say which namespaces sleep on a
  schedule, when they wake up, and whether they are asleep right now, and a schedule can be moved
  from the phone (days, sleep and wake-up times, time zone). ExternalDNS shows the
  hostnames it manages, where each comes from, and the controller's settings; it can resolve
  every hostname from the phone and compare it with the record's targets, so a name that points
  elsewhere, or that no controller owns, stands out. Sealed Secrets shows the controller, the
  sealing keys and whether each SealedSecret unsealed, by name only. When the garage-operator
  manages Garage, its clusters, buckets (size, quota, website), keys and node declarations show
  next to the live Garage state.
- **Several clusters:** switch from the header, give each one a color and a name of your own, and
  open any of them straight from the app icon (long press: a shortcut / quick action per cluster).
- **Protection:** fingerprint/PIN lock, or a tap of a security key (YubiKey or any FIDO2 key,
  over NFC or USB), which can also seal the stored configs so that nothing reads them without it.
- **AI diagnosis:** optional, off by default: ask Claude or an OpenAI model what is wrong and
  how to fix it, or hand the question to an assistant app (see [AI diagnosis](#ai-diagnosis-optional)).

The Talos API layer is the official Go client (`siderolabs/talos/pkg/machinery`, same code as
`talosctl`) compiled with gomobile, so talosconfig parsing, Ed25519 mTLS and endpoint→node
proxying behave exactly like `talosctl`.

[<img src="https://raw.githubusercontent.com/ImranR98/Obtainium/main/assets/graphics/badge_obtainium.png" alt="Get it on Obtainium" height="54">](https://apps.obtainium.imranr.dev/redirect?r=obtainium://app/%7B%22id%22%3A%22name.levis.ichor.foss%22%2C%22url%22%3A%22https%3A%2F%2Fgithub.com%2Fcyrinux%2Fichor%22%2C%22author%22%3A%22cyrinux%22%2C%22name%22%3A%22Ichor%22%2C%22additionalSettings%22%3A%22%7B%5C%22apkFilterRegEx%5C%22%3A%20%5C%22%5E%28%3F%21.%2Aunsigned%29.%2A%5C%5C%5C%5C.apk%24%5C%22%7D%22%7D)

**Install on Android:**

- **[Obtainium](https://obtainium.imranr.dev)** installs and updates the app straight from this
  repository's GitHub releases. The button above pre-fills it, with a filter that skips
  unsigned APKs.
- **Manually:** download `ichor-v<version>-<abi>.apk` from the
  [latest release](https://github.com/cyrinux/ichor/releases/latest); `arm64-v8a`
  fits almost all phones.

**F-Droid:** not listed yet. The planned route is [IzzyOnDroid](https://apt.izzysoft.de/fdroid/),
an F-Droid repository that ships the signed release APKs.

```
app/            Android: Kotlin + Jetpack Compose UI
ios/            iOS: SwiftUI app (XcodeGen) + IchorCore Swift package
go/ichorgo  Go core exposed to Kotlin and Swift (JSON in/out)
go/cmd/probe    desktop CLI calling the same Go functions
flake.nix       Nix build environment (default)
build/          amd64 Docker toolchain (CI)
```

## Features and the talosconfig role they need

Talos enforces access with the roles baked into the talosconfig client certificate. The app
reads those roles and explains up front when a feature needs more.

| Feature | Talos API | Minimum role |
|---|---|---|
| Cluster overview (readiness, stage, version, role) | `Version`, COSI resources | `os:reader` |
| Node services and resources | `ServiceList`, `Memory`, `LoadAvg`, `SystemStat`, `CPUInfo`, `Mounts` | `os:reader` |
| Service logs and kernel log (dmesg) | `Logs`, `Dmesg` | `os:reader` |
| etcd members, leader, DB size, alarms | `EtcdMemberList`, `EtcdStatus`, `EtcdAlarmList` | `os:reader` |
| Background alerts and widget | same as the overview and etcd | `os:reader` |
| AI diagnosis report (optional) | the calls above, `Time`, `Events`, `Containers`, `NodeStatus`, `StaticPodStatus` | `os:reader` |
| **Reboot (`-m default\|powercycle\|force`) / shutdown (`--force`)** | `Reboot`, `Shutdown` | **`os:operator`** |
| **Cluster health check** | `ClusterService/HealthCheck` | **`os:admin`** |
| **Kubeconfig export** | `Kubeconfig` | **`os:admin`** |
| **Kubernetes workloads and pods, rollout restart, pod delete** | `Kubeconfig`, then the Kubernetes API | **`os:admin`** |
| **Kubernetes CronJobs, run now** | `Kubeconfig`, then the Kubernetes API (cronjobs, jobs) | **`os:admin`** |
| **CSI snapshots, snapshot creation and backup CronJob health** | `Kubeconfig`, then the Kubernetes API (volumesnapshots, volumesnapshotcontents, volumesnapshotclasses, PVCs, storageclasses, cronjobs, jobs) | **`os:admin`** |
| **CloudNativePG replica promotion** | `Kubeconfig`, then the Kubernetes API (clusters/status, pods and optional pods/proxy for lag) | **`os:admin`** |
| **Velero: run a schedule now, delete a backup** | `Kubeconfig`, then the Kubernetes API (velero.io schedules, backups, deletebackuprequests) | **`os:admin`** |
| **Velero: restore a backup with a preview and namespace mapping** | `Kubeconfig`, then the Kubernetes API (velero.io backups, restores, backupstoragelocations) | **`os:admin`** |
| **kube-green sleep schedules** | `Kubeconfig`, then the Kubernetes API (sleepinfos.kube-green.com, cluster-wide and one namespace's own when a workload or CronJob sheet opens; optional controller Deployment in `kube-green`) | **`os:admin`** |
| **Edit a kube-green schedule** | `Kubeconfig`, then the Kubernetes API (merge patch of sleepinfos.kube-green.com: `patch` in its namespace; the button is off with the reason without it) | **`os:admin`** |
| **Data services: ExternalDNS hostnames and controllers** | `Kubeconfig`, then the Kubernetes API (deployments, ingresses, services, optional dnsendpoints.externaldns.k8s.io and httproutes/gateways) | **`os:admin`** |
| **Data services: Sealed Secrets status and sealing keys** | `Kubeconfig`, then the Kubernetes API (sealedsecrets.bitnami.com, deployments, secrets metadata with the sealing-key label) | **`os:admin`** |
| **App web addresses (Ingress, HTTPRoute, GRPCRoute, TLSRoute, Traefik IngressRoute)** | `Kubeconfig`, then the Kubernetes API (pods, services, ingresses, httproutes, grpcroutes, tlsroutes, gateways, ingressroutes, ingressroutetcps) | **`os:admin`** |
| **Network test between two nodes** | `Kubeconfig`, then the Kubernetes API (namespaces, pods) | **`os:admin`** |
| **Node pressure (PSI) and cgroups (like `talosctl cgroups`)** | `Copy` of `/sys/fs/cgroup`, `Containers` | **`os:admin`** |
| **Policies (Kyverno)** | `Kubeconfig`, then the Kubernetes API (kyverno.io policies, wgpolicyk8s.io policy reports, Kyverno's Deployments and webhooks) | **`os:admin`** |
| **Virtual machines: list, start/stop/restart/pause/migrate, serial console** | `Kubeconfig`, then the Kubernetes API (kubevirt.io, subresources.kubevirt.io including `virtualmachineinstances/console`, cdi.kubevirt.io) | **`os:admin`** |
| **Cluster checkup, events of a pod or workload** | `Kubeconfig`, then the Kubernetes API (pods, nodes, events, claims, webhooks, quotas, services, CSRs, secrets metadata…), each kubelet's `stats/summary` through the API server proxy, and a TLS handshake with each web address to read the certificate it serves (never the Secret, whose private key would come along) | **`os:admin`** |
| **Talos rollback (`talosctl rollback`)** | `Rollback` | **`os:admin`** |
| **CloudNativePG primary restart and hibernate/wake** | `Kubeconfig`, then the Kubernetes API: get clusters and pods, patch `clusters/status` for restart, patch clusters for hibernation | **`os:admin`** |
| **Longhorn faulted-volume salvage** | `Kubeconfig`, then the Kubernetes API: get volumes, list replicas/nodes, create `services/proxy` to the Longhorn manager | **`os:admin`** |
| **Longhorn volume attach/detach** | `Kubeconfig`, then the Kubernetes API: get volumes/nodes, list nodes for preview, create `services/proxy` to the Longhorn manager | **`os:admin`** |
| **Node reset (`talosctl reset`)** | `Reset` | **`os:admin`** |

Notes:

- **Source:** the rules come from Talos v1.14 (`internal/app/machined/pkg/system/services/machined.go`).
- **Health check:** the call itself is allowed for `os:reader`. Talos then runs the check with the
  caller's roles, and the Kubernetes checks need an admin kubeconfig. With a lower role, the
  overview and etcd screens give the same node and etcd picture.
- **Roles combine:** an `os:operator` config can do everything an `os:reader` one can.
- **Kubernetes access:** see [Kubernetes clusters without Talos](#kubernetes-clusters-without-talos)
  to use your own kubeconfig identity instead of the admin kubeconfig (no `os:admin` needed).
- **Omni:** a cluster managed by Omni has no talosconfig roles; Omni's own role for the identity
  applies, see [Omni clusters](#omni-clusters).
- **Kubernetes workloads:** the app asks Talos for an admin kubeconfig and keeps it in memory
  only, for 30 minutes; it is never written to disk. The phone must reach the Kubernetes API
  (port 6443): when the kubeconfig's address (often a VIP or an internal name) does not answer,
  the app tries the Talos endpoints on the same port.

### Network tools

**Node menu → Network tools** runs one check from that node and shows the result as fields,
not as terminal output. It uses the same privileged `nicolaka/netshoot` container as the debug
shell, so it needs os:admin; the first run pulls the image.

| Tool | What it shows |
|---|---|
| DNS | The records (A, AAAA, CNAME, MX, NS, PTR, SRV, TXT) with their TTL, the answer's status, the resolver that answered and the query time. Leave the resolver empty for the node's own, or give one such as the cluster DNS. |
| Ping | Packets sent and received, loss, and min / avg / max round trip. |
| Port | Whether a TCP `host:port` is open, closed or filtered. |
| Trace | The path, hop by hop, with loss and average latency (`mtr`, else `tracepath`). |
| HTTP/TLS | The status after redirects and the total time. For https, whether the certificate verifies, its subject, issuer and days left. |

- **Targets:** checked before anything runs: no spaces, no shell characters, no leading dash.
  The last ten are offered again per cluster, on that phone only.
- **More:** a run can be cancelled. The raw output sits under each result, and **Share** sends
  the result as text.

### Packet capture

**Node → Capture** is `talosctl pcap`: pick an interface, an optional tcpdump filter, how long
and how big, and watch the packets arrive. The file is saved on the phone and opens in the app
or in Wireshark.

- **Two nodes at once:** pick a **Second node** (or, on the KubeSpan map, tap a link and then
  **Capture both ends**).
  - **The run:** both nodes are captured with the same filter. Each node gets its own interface
    and its own size limit, and both stop together.
  - **The file:** one file, packets in time order, each marked with its node. It is pcapng
    inside, and Wireshark shows the node as the interface.
  - **Timing:** each packet carries the time of the node that captured it, so a difference
    between the two clocks shifts one node's packets against the other's, and a reply can show
    before its request.
  - **The clock note:** at the start, each node's clock is compared with its NTP server, like
    `talosctl time`. The capture screen and the saved file then say how many ms apart the two
    clocks are, as a warning from 5 ms. The gap is recorded in the file.

### Network test

**Kubernetes → Network** (also under **Cluster insights → Network**) measures the network from a
client node to a server node with netperf, the way `cilium connectivity perf` does, on any CNI:
TCP throughput (`TCP_STREAM`), then TCP round trips (`TCP_RR`: p50/p90/p99 latency), pod to pod
and, optionally, host to host. The test keeps running when you leave the screen, with its progress and Stop in the
notification or Live Activity.

- **What runs:** a namespace `ichor-netperf-<random>`, a `netserver` pod on the server node and
  one short-lived `netperf` pod per measurement on the client node, with the
  `quay.io/cilium/network-perf` image pinned by digest (pulled from quay.io by the nodes). The
  namespace is deleted at the end, also when the test fails or is stopped; one left behind by a
  killed app is deleted by the next test.
- **Pod security:** the pods run as non-root, without capabilities, with a read-only root file
  system and the `RuntimeDefault` seccomp profile, so pod to pod passes the `restricted` level.
  The host network variant creates the namespace with
  `pod-security.kubernetes.io/enforce=privileged`.
- **Firewall:** host to host uses TCP ports 12866 (control) and 12867 (data) on the server node.
  With an ingress firewall on the nodes, allow them between nodes, or the host measurements
  report that netserver does not answer.
- **Load:** each measurement saturates the link between the two nodes for its duration (5, 10 or
  20 s); run it when that is acceptable.
- **Saved results:** the last 20 finished tests of each cluster (stopped ones too, with what they
  measured) are listed under the setup, to compare over time. They are kept encrypted on the
  phone, outside backups, apart for the privacy mask; open one to delete it (or swipe it away on iOS).
- **Charts:** with two saved tests or more between the chosen client and server, an *Over time*
  card charts their pod-to-pod throughput and p50 latency (tap a point to see that test). Each
  latency result also draws where the round trips fell, from the fastest to the slowest, with
  p50 to p99 as a bar. The KubeSpan map shows the last pod-to-pod throughput measured
  between two nodes on their link, and the link's sheet the rest of that test.
- **Why a KubeSpan peer is down:** on the KubeSpan *Peers* tab, tap a peer to see why in plain
  words. The reasons cover:
  - no endpoint to try;
  - a handshake that went stale, with the endpoints tried;
  - an endpoint that keeps changing;
  - a different KubeSpan MTU on the other node;
  - endpoint filters that may exclude every address.

  The raw fields are under *Details*. A node Omni manages shows its SideroLink connection.
  Nothing secret is read: neither the WireGuard key nor the KubeSpan shared secret.

### Image vulnerability scan

**Scan images** on an app's sheet (and on a Talos node's Images screen) looks for known
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

### CronJobs

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

When kube-green has the namespace asleep, a workload sheet says so on the row (with its wake-up
time) and warns before scaling a workload or resuming a CronJob, since kube-green restores the
replicas it saved at the next wake-up and re-suspends the CronJob at the next sleep; a cluster
without kube-green shows nothing new.

**Data services → kube-green → a schedule → Edit schedule** moves it: the days, the sleep and
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

### Cluster checkup

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
- **Alerts:** Settings → Alerts → Cluster checkup runs it at every background check (it lists every
  pod and asks each kubelet). A critical finding notifies at once, a warning when seen on two
  checks in a row, a cleared one once; a section that could not be read keeps what it knew.
- **Hardware sensors:** a node's *About this node* screen shows its temperatures (with each
  sensor's max and critical limits), fans, per-core CPU frequency with the governor, the CPU's
  thermal throttle counters and its PCI devices, read with `os:reader` from `/sys/class/hwmon`,
  the thermal zones and `CPUFreqStats` (Talos 1.5+). Cores held below 70 % of their top speed
  under the `performance` governor, or a sensor above its max, mark the node *Throttled* there
  and, once that screen was opened, on its row in the node lists. A VM has no Sensors card.
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
  from the Talos ISO and type the address its console shows.
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

### Capacity

**Kubernetes → Capacity** (also linked from the checkup's capacity card) answers "can one more fit,
and what goes if a node goes down?":

- **Free room** for the cluster and per node: CPU, memory and pod slots left by requests (the
  scheduler's view), with each node's memory requests and live usage as two bars when
  metrics-server answers, and a mark on a node whose pods' memory limits exceed it.
- **N+1:** whether the pods of any one node would fit on the others, or which node would leave
  pods Pending.
- **Workloads, the first to go first:** lowest priority first, those with no PriorityClass flagged,
  each with its numeric priority, QoS class, risk (OOM-killed or near its memory limit, evicted
  first as BestEffort, no memory limit) and how many more replicas of its size fit today.
- **What if a node goes down:** a node, several picked together, or the worst one, is lost in a
  simulation of the scheduler: where its pods land, which lower priority pods they preempt, which
  stay Pending, then what to scale down, lowest priority first, within PodDisruptionBudgets and
  never a `system-*` class. A suggestion opens the object's scale dialog; nothing is scaled from
  this screen. It is an estimate by requests: affinity, topology spread and volume node affinity
  are not modelled, and the sheet says so.

### Policies

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

### Autoscaling

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

### Virtual machines

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

### Crossplane

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

### Tailscale

**Kubernetes → Tailscale** (the button appears when the cluster runs the Tailscale operator)
shows the operator, every connector (subnet routes, exit node, app connector, tailnet IPs and
devices), proxy group (type, devices, the kube-apiserver URL), proxy class (and who uses it) and
the tailnet nameserver — problems first, with the operator's reason. Tapping a row opens the
object's summary; nothing is changed here (os:admin).

### Cluster lifecycle (Cluster API, Sidero Metal)

**Kubernetes → Cluster lifecycle** (the button appears on a management cluster that runs
Cluster API or Sidero Metal) shows the workload clusters with their phase, control plane and
workers ready of desired, endpoint and Kubernetes versions; their machines grouped by cluster,
control plane first, with phase, node, version, addresses and a "stuck since" when one has not
moved for half an hour; the machine deployments with their replicas; and, with Sidero Metal,
the bare-metal servers (accepted, cordoned, in use, clean, power, server classes, CPU and
memory) and how many servers each server class has free. Problems come first, with the failure
or the conditions that are not True. Tapping a row opens the object's summary; nothing is
changed here (os:admin).

### Argo CD app icons

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

### Creating a talosconfig for the phone

Generate a dedicated identity instead of copying your admin `~/.talos/config`. If the phone is
lost, its certificate only grants what you chose, and it expires on its own.

`talosctl config new` needs exactly one node to sign the certificate: use a control-plane node.

```sh
CP=10.0.0.2   # any control-plane node

# Monitoring only (recommended default):
talosctl -n $CP config new talosconfig-phone --roles os:reader --crt-ttl 8760h

# Monitoring + reboot/shutdown:
talosctl -n $CP config new talosconfig-phone --roles os:operator --crt-ttl 8760h

# Everything, including the health check and kubeconfig export (treat the phone like a laptop):
talosctl -n $CP config new talosconfig-phone --roles os:admin --crt-ttl 8760h
```

Check what was generated. The app queries the context's `nodes`, falling back to its
`endpoints` when the list is empty, so make sure every node is listed:

```sh
talosctl --talosconfig talosconfig-phone config info
talosctl --talosconfig talosconfig-phone config node 10.0.0.2 10.0.0.3 ...   # all nodes
talosctl --talosconfig talosconfig-phone -n $CP version                                # sanity check
```

The `talosconfig*` filenames are gitignored in this repository.

**Renewal:** the certificate lasts `--crt-ttl` (default one year). Settings shows the expiry
date. When background alerts are on, the app notifies daily from 14 days before expiry. To
renew, generate a new file and import it (Settings → Clusters & backup → Add a cluster): a cluster already
imported, i.e. the same context name and CA, is updated in place.

### Importing it

Pick one of these:

- **File:** `adb push talosconfig-phone /sdcard/Download/`, then choose it in the app.
- **Paste:** paste the YAML.
- **QR code:** run `qrencode -r talosconfig-phone -o talosconfig.png` and scan it. This works
  for configs up to about 2.9 KB. A larger one (a kubeconfig with an embedded certificate)
  usually fits compressed; the app expands it:

  ```sh
  gzip -9 < talosconfig-phone | qrencode -8 -t ansiutf8
  ```

  Both forms work, as does the older `ichor-config:` text form (base64url of gzip).

Delete `talosconfig.png` and the copy in `Download/` afterwards; both contain the private key.
The app stores the config AES-GCM encrypted, excluded from backups: an Android Keystore key (in
the StrongBox chip when the phone has one) wraps a random data key that encrypts the config, so
it opens in milliseconds however many clusters it holds.

### Several clusters

Every context of a talosconfig is a cluster in the app, and importing another talosconfig
adds its contexts to the ones already there (like `talosctl config merge`: a context whose
name is taken by another cluster is added as `name-1`). To switch cluster:

- **Android:** swipe the overview's top bar left or right, or tap its title for the list.
- **iOS:** swipe the row of dots under the overview's title, or tap it for the list.

On Android, screens with tabs (node, Kubernetes, data services, Flux, network, KubeSpan,
insights, flows) also switch tab with a sideways swipe on their content.

The list is also where a cluster is removed (its credentials are deleted from the device) and
where its color is chosen. Each cluster gets a color of its own, and the app's palette (light,
dark and true black alike) is generated from the color of the cluster on screen, so it is
always clear which cluster a reboot is about to hit. Background alerts and the widget follow
the cluster on screen.

**Search every cluster's nodes, pods, workloads and GitOps apps from the overview** (Android:
"Search clusters" in the title's menu, or the search action of the top bar; iOS: "Search
clusters" in the top bar's ⋯ menu, which can be moved to the bar). Results come from
what each cluster last showed: other clusters than the one on screen need "Keep last known
state" on. Typing never calls a cluster; a result opens its screen on its cluster, like a share
link. The search also opens from the app icon: a long press on Android ("Search clusters"),
a Home Screen quick action on iOS. On iOS the clusters and the Argo CD and Flux apps the phone
knows also show in Spotlight and open Ichor on them; only their names are indexed, and only
while "Keep last known state" is on and neither the app lock nor the screenshot mode is.

**Pin a node, workload, pod, CronJob, Argo CD or Flux app** (the pin button next to its share
link) to a Pinned card at the top of that cluster's overview: up to 12 per cluster, each with its
ready or health state when the overview knows it, opening like its share link; a long press on a
row (Android) or a swipe (iOS) unpins it, and the card moves or hides like the others.

**Open a screen straight from the home screen or a shortcut:**

- **Android:** "Add to home screen" (next to a screen's share link, and on the checkup and
  alerts screens) pins an icon in the cluster's color that opens that screen of that cluster
  after unlock; it greys out when the cluster is removed.
- **iOS:** "Open in Ichor" in the Shortcuts app takes a share link and opens its screen once
  unlocked, so a shortcut (or Siri, with the phrase you give it) opens a node, an app or etcd.

### Kubernetes clusters without Talos

A cluster can also be added from a kubeconfig alone, the same ways as a talosconfig (file,
paste, QR, or "Open with" / the share sheet); the app tells the two apart. The preview lists
every context with how it signs in, and why one cannot be added (a file path to inline with
`kubectl config view --flatten --minify`, a proxy, plain HTTP…). These clusters get a
Kubernetes home (nodes, API server version, who you are signed in as) and every Kubernetes
screen; the Talos ones are hidden. What they may do is up to their RBAC.

Each node on that home also says where the cloud put it, read from its labels: the
autoscaler pool it came from (a Karpenter NodePool — open source or EKS Auto Mode — an EKS
managed node group, a GKE custom compute class or node pool, an AKS agent pool), its
machine type (`node.kubernetes.io/instance-type`) and whether it is spot, on-demand or
reserved capacity (`karpenter.sh/capacity-type`, `eks.amazonaws.com/capacityType`,
`cloud.google.com/gke-spot`, `kubernetes.azure.com/scalesetpriority`). Nothing is shown
when no such label is set, as on bare metal.

| Kubeconfig user | What the app does |
|---|---|
| Client certificate, token (ServiceAccount) | Uses it as is |
| `kubectl oidc-login` / kubelogin, `auth-provider: oidc` | Signs in in the browser (kubelogin's own `localhost` redirect, so nothing changes on the identity provider) or with a device code; renews the token with the refresh token |
| `aws eks get-token`, aws-iam-authenticator | IAM Identity Center (device code, like `aws sso login`; the account and role are picked from a list after the sign-in) or access keys; `--role-arn` through AssumeRole. No kubeconfig yet: **Add from a cloud account** → EKS → IAM Identity Center |
| `gke-gcloud-auth-plugin` | A service account key, your Google account in the browser like gcloud (every build), Sign in with Google (Play build and iOS), the user credentials `gcloud auth application-default login` writes (`application_default_credentials.json`, also with `--login-config` for workforce identity), your organisation's own OAuth client (Desktop app type: sign-in in the browser, no key), or a workforce identity login config (browser sign-in at your organisation's identity provider) |
| Azure `kubelogin` (AKS with Entra ID) | Device code or browser, or a service principal |
| `doctl … exec-credential` | A DigitalOcean API token (short-lived cluster credentials from it) |
| `rancher token` | A Rancher API key |

**GKE credentials by QR:** scan a gcloud ADC file from the import screen or tap
**Scan credentials QR code** in GKE discovery. It opens discovery with the credential
filled in; choose projects if needed, then tap **Find clusters**. Google user,
workforce identity session and service account files are supported. Keep the QR
private: it contains a refresh token or private key.

```sh
qrencode -t ansiutf8 < ~/.config/gcloud/application_default_credentials.json
```

**EKS Identity Center by QR:** paste or scan the `~/.aws/config` that `aws configure sso`
wrote, from the import screen or with **Scan ~/.aws/config QR code** in EKS discovery. It
opens discovery on IAM Identity Center with the start URL and its region filled in; tap
**Sign in and find clusters**, and the profile's account and role are already ticked. The first profile that uses Identity Center is taken (an `sso-session` alone
works too, then you pick the account and role). The file holds no secret. An
`~/.aws/credentials` file is refused: type access keys on the EKS discovery screen.

```sh
qrencode -t ansiutf8 < ~/.aws/config
```

**GKE with your Google account, no setup**: pick "Your Google account (like gcloud)" on the
GKE sign-in sheet and tap **Sign in**. Google's sign-in opens in the browser (your
organisation's SSO applies) and comes back by itself; the app then keeps the session as
`gcloud auth application-default login` does, renewing it without the browser until Google ends
it. It uses gcloud's own public OAuth client, so the consent page names Google Cloud SDK, and
Workspace admins who allow gcloud allow it. The refresh token is stored like an imported
`application_default_credentials.json`, so encrypted backups keep the session.

**GKE with workforce identity, no laptop**: pick "Workforce identity (login config)" on the
GKE sign-in sheet, import the login config your administrator made with
`gcloud iam workforce-pools create-login-config`, and tap **Sign in**. Google's workforce page
opens in the browser and sends you on to your organisation's identity provider, as
`gcloud auth login --login-config` does; the session is then stored and renewed like an
imported workforce `application_default_credentials.json`, and the config stays filled in for
the next sign-in.

**GKE with your organisation's OAuth client**: in the Google Cloud console (APIs & Services),
1. set the OAuth consent screen to **Internal** (no Google verification needed),
2. create an OAuth client ID of type **Desktop app**,
3. in Ichor, pick "Your organisation's OAuth client" on the GKE sign-in sheet and paste its ID
   and secret. Sign in then opens your Google account in the browser (company SSO included);
   the app keeps a refresh token and asks again only when Google ends the session.

A **Web application** client works too (the kind kubenav uses): add
`https://cyrinux.github.io/ichor/auth/google/` (or a page of your own doing the same) to its
authorised redirect URIs and enter it as the redirect URL. Google sends the browser to that
page, which hands the sign-in back to the app; if the browser cannot open the app, the page
shows a sign-in code to paste. The code is useless without the key only the app holds (PKCE).

**Add from a cloud account** (on the add screen) lists the clusters of an AWS, Google Cloud,
Azure, DigitalOcean or Rancher account and adds the ones you pick, signed in with the same
credentials. For Google Cloud that is a service account key, the gcloud user credentials of
your own account, your Google account in the browser like gcloud (every build), **Sign in with Google** (Play build, and iOS builds that carry Ichor's Google
client), or your organisation's OAuth client in the browser, with no file to paste: the app then
looks through every project you can see (or the project IDs you enter), and the clusters you add
reuse that sign-in without a second prompt.

**EKS with IAM Identity Center, no kubeconfig**: on **Add from a cloud account**, pick EKS, then
"IAM Identity Center", and enter your access portal's start URL and its region. **Sign in and
find clusters** opens the portal in the browser with the device code filled in, as
`aws configure sso` does; the app then lists the accounts and roles you may assume (one is
taken as is) and you tick those to look in. It reads their clusters in the regions you enter,
or in the Identity Center region and the common regions when you leave them empty, a few at a
time; a region the role may not list (`eks:ListClusters`, `eks:DescribeCluster`) is skipped and
named. The clusters you add sign in with the role they were found with and the same session:
no second browser round.
A Talos cluster can also use one of these kubeconfig clusters for its Kubernetes
screens (cluster menu → **Kubernetes access**): your own identity and RBAC instead of the
admin kubeconfig, which also works with an `os:reader` talosconfig.

Sign-in tokens and entered keys are sealed on the device like the configs. Backups keep the
keys you entered but not the tokens of a sign-in: after a restore, those clusters ask to sign
in again.

### Omni clusters

A cluster managed by [Sidero Omni](https://omni.siderolabs.com/) is reached through Omni, which
proxies the Talos API to its machines and signs every call with a PGP key, as `talosctl` and
`omnictl` do. Add one in any of three ways:

- **Add from Sidero Omni** (on the add screen): the instance URL, then an account email or a
  service account key (`OMNI_SERVICE_ACCOUNT_KEY`, as `omnictl serviceaccount create` prints it).
  Omni lists the clusters the identity may see and the preview adds the ones you pick.
- **Import Omni's talosconfig** of a cluster (downloaded from Omni, or `omnictl talosconfig`),
  like any other talosconfig. It has no certificate: the app asks for the sign-in on first use.
- **Enter details** (the form): the instance URL, the cluster name in Omni and who signs in.

An account signs in by confirming a key in the browser, as `omnictl` does: the key lasts 4 hours,
then the app asks again. A service account key is long-lived and is entered once. One sign-in
serves every cluster of the same identity on the same instance; removing one cluster keeps the
others signed in. Backups keep service account keys but not account sign-ins.

What the app may do is Omni's role for the identity: the Reader role reads the Talos API through
Omni and lists the clusters; Kubernetes goes through Omni's own proxy, signed in with the same
key, and needs an access policy on the cluster (or the Operator role), as `omnictl kubeconfig`
does. Issuing a talosconfig for a teammate is not available for Omni clusters: download one from
Omni instead.

### Share links

**Share link** sends a link to the screen on show, for a teammate whose Ichor holds a
talosconfig for the same cluster: an Argo CD or Flux app, a node (on its tab), a workload, pod
or CronJob, etcd, the health check, the Argo CD and Flux lists, the Kubernetes tabs or the
cluster overview. It is in the top bar, the node and cluster menus, the workload sheet, the
pod's log sheet (link icon), a CronJob's card (Android) or a pod or CronJob's context menu (iOS).

- The link is `https://cyrinux.github.io/ichor/open/#…`. That page hands it to the app as
  `ichor://open?…` (Android also offers to open the https link in Ichor directly). What follows
  `#` stays in the browser: the website never sees it.
- The cluster is named by a hash of its CA certificate, the same on every phone whatever the
  context is called there. The link carries the names the screen shows (namespace, name,
  hostname and address), nothing secret. In screenshot mode it carries the masked names.
- Opening it unlocks the app first, switches to that cluster (keeping the context on screen
  when it is one of it, so its role) and opens the screen. A link only ever navigates: no
  action, not even one waiting for confirmation. A node opens only when it is one of that
  cluster's (else the link stops at the overview). Without that cluster, the app says so.

### Backup and restore

The stored config's key never leaves the phone, so moving to a new phone takes a backup:
**Settings → Clusters & backup → Back up clusters and settings** (with the app lock on, you authenticate
first). Restore it from the same section, or from **Restore a backup** on the first screen of
a fresh install. Backups move between Android and iOS.

- **What it holds:** the talosconfig (every cluster's credentials), the cluster on screen,
  theme, language, screenshot mode, monitoring, and each cluster's name, color, VPN-only
  setting and Wake-on-LAN targets.
- **What it leaves out:** AI diagnosis settings and API keys, the app lock and "Allow
  screenshots" (set them again on the new phone).
- **Encryption:** a passphrase of at least 12 characters. The key comes from Argon2id (64 MiB,
  3 passes) and the file is sealed with AES-256-GCM. Without the passphrase it cannot be opened,
  and the passphrase cannot be recovered.
- **Restoring replaces** every cluster and setting on the phone with those of the backup.

## Safety features

- **Reboot / shutdown:** the same options as talosctl, from the Talos v1.14 sequencer:

  | Option | What happens |
  |---|---|
  | Reboot `default` | Stop pods and services, then reboot (kexec fast reboot when available). No cordon/drain. |
  | Reboot `powercycle` | Same graceful stop, then a full firmware reboot instead of kexec. |
  | Reboot `force` | Reboot immediately, with no pod or service shutdown. Only for a stuck node. |
  | Shutdown `--force` | Skip the Kubernetes cordon/drain, e.g. when the API is down. Pods and services are still stopped. |

  Every option goes through the same confirmation:
  1. You type the node's hostname to confirm.
  2. Control-plane nodes get an etcd-quorum warning.
  3. With the app lock on, you also authenticate with fingerprint or PIN, or a security key.
- **Reset (node menu → Reset…):** `talosctl reset` with a plan first. The last control plane,
  or one whose departure would cost etcd its quorum, cannot be reset: the app and the Go core
  both refuse it. The options, then the same typed hostname and app lock as a reboot:

  | Option | What happens |
  |---|---|
  | Wipe everything | The system disk and the node's other disks, listed in the sheet. |
  | Wipe the system disk | The system disk only: the node starts over as a new machine. |
  | Wipe the user disks | The node's other disks only. |
  | Leave etcd first (`--graceful`, on by default) | Cordon, drain and leave etcd cleanly first. Off on a control plane, its etcd member stays behind: remove it from the etcd screen. |
  | Reboot after (`--reboot`, on by default) | Off, the node stays powered off once wiped. |

- **Replacing a control plane (etcd → a failed member's menu → Replace this control plane…,
  `os:admin`):** one screen walks through the replacement, step by step, read from the cluster
  so that leaving and coming back resumes:
  1. **Check etcd quorum:** the removal must leave enough healthy members. A healthy member is
     never offered, and a removal that would lose quorum is blocked with the reason.
  2. **Remove the etcd member:** the usual removal plan and typed hostname. The Go core reads
     the plan again first, refuses a member that recovered, and sends the removal through a
     healthy control plane.
  3. **Reset the old node:** the reset sheet, without `--graceful` (the member already left).
     A node that no longer answers is skipped: power it off yourself. So is an address that a
     current member now uses (a replacement that took the old IP).
  4. **Boot the new node:** by hand for now. Boot it from a Talos image, open the machine config
     of a healthy control plane (secrets revealed only after the app lock), save it as
     `controlplane.yaml`, then run `talosctl apply-config --insecure -n <new-node-ip> -f controlplane.yaml`
     from a laptop.
  5. **Wait for the new member:** the screen polls etcd until a new healthy voting member joins.

- **Container restart (node → Pods → a container's menu → Restart container…, `os:admin`):**
  `talosctl restart` without a reboot. A Talos system container (apid, trustd, an extension
  service) asks for the typed hostname; a Kubernetes container asks a plain confirm, as the
  kubelet starts it again. With the app lock on, you authenticate first.

- **Policy mode (Kubernetes → Policies → a Kyverno policy, `os:admin`):** switching a policy
  to Audit, which lets requests that break it through, asks for the typed policy name; switching
  back to Enforce asks a plain confirm. The button is off, with the reason, when the credentials
  may not patch the policy, and the activity log keeps every switch.

- **Autoscaling (Kubernetes → Autoscaling → a ScaledObject or an HPA, `os:admin`):** pausing,
  holding or resuming a KEDA ScaledObject and editing an HPA's min and max are reversible: each
  asks a plain confirm, except a pause at 0, which stops every pod and so asks for the
  ScaledObject's name typed, like a scale to 0. Each button is off, with the reason, when the credentials may not patch
  that kind; the activity log keeps every change; the demo cluster refuses them.

- **META keys (node menu → Machine config → META, `os:admin`):**
  - **What they are:** the boot-time values Talos reads before the machine config, such as the
    network config of a metal node (0x0a). This is the last-resort fix for a node that cannot boot
    with its config.
  - **Writing or deleting a key:** asks for the typed hostname, like `talosctl meta write` and
    `talosctl meta delete`. With the app lock on, you authenticate first.
  - **What the app refuses or hides:** keys that only Talos writes (upgrade, STATE encryption)
    cannot be changed. Secret values are never shown. The activity log keeps the key and the
    size, never the value.

- **Machine config changes (node menu → Machine config → Edit):**
  - **Review first:** the node checks every change with a dry run before anything is applied.
  - **Snippets:** while editing, ready-made changes cover the usual ones: NTP and DNS servers, an
    interface MTU, a sysctl, kernel arguments, a kubelet or API server argument, a node label, a
    registry mirror, KubeSpan on or off. You fill a few fields, the change lands in the draft and
    goes through the same review. On Talos 1.12+ configs, a setting held in its own document
    (ResolverConfig, TimeSyncConfig, KubeletConfig…) is changed there.
  - **Ways to apply it:**

    | Mode | What happens |
    |---|---|
    | Try for 1, 5 or 10 min | Applied without a reboot; the node goes back to its previous config by itself unless you keep it. |
    | Apply now | Applied for good, without a reboot. Refused when the change needs one. |
    | Apply at the next reboot | Saved on the node (`--mode staged`), applied when it next reboots. |
    | Apply and reboot now | Applied, then the node reboots; you type its hostname first. |

  - **Changes that need a reboot:** they offer only the last two modes.
  - **A try keeps running in the background:** leave the screen and the countdown goes on in a notification, with **Keep** and **Revert now**.
    - **App lock:** with it on, Keep opens the app first.
    - **On iOS:** the background time is short. Once iOS suspends Ichor, the node reverts by itself at the deadline, and the notification says so.
  - **The same change on several nodes ("Also apply to other nodes…"):**
    - **Preview:** the field edits are replayed on each picked node's own config, and you see each node's diff first. A node the edits do not fit is skipped.
    - **Run:** one confirmation for all, typed with the cluster's name. Nodes are applied one after the other, workers first and control planes last, and the first failure stops the rest.
    - **Reboot mode:** each node is back before the next starts, and etcd must be healthy before a control plane reboots.
    - **Try mode:** for one node only.
    - **In the background:** on Android the rollout goes on in a notification, node by node, after you leave the app. On iOS it runs while iOS allows, and a notification asks you to come back.
- **App lock (Settings → Security):**
  - **Methods:** fingerprint, with the device PIN, pattern or password as fallback; or a
    **security key** (below).
  - **When it asks:** at launch, after 30 s in the background, and before reboot, shutdown and kubeconfig export.
  - **Privacy:** it hides the app in recent apps and blocks screenshots, unless you turn on
    "Allow screenshots", which also requires authenticating.
  - **Changing it:** turning it on or off requires authenticating.
- **Security keys (Settings → Security → Security keys):** a YubiKey or any FIDO2 key, tapped on
  the back of the phone (NFC) or plugged in (USB), opens the app and confirms the risky actions
  instead of the fingerprint. Up to two keys (one worn, a spare). Ichor makes a FIDO2 credential
  on the key (user presence only: a touch, never the key's PIN) and checks its signature over a
  fresh challenge each time; it talks to the key itself with Yubico's SDK, no Play Services and no
  account. The PIN that the app lock falls back to is the **device** PIN: a forgotten device PIN
  locks the phone itself, and no app can offer a key as a way around that.
  - **Require the key:** the stored configs (talosconfig, kubeconfig, the kubeconfig sign-ins) are
    sealed a second time with a data key that only the hmac-secret output of one of the enrolled
    keys unwraps, on top of the Keystore encryption. Fingerprint or PIN alone no longer opens the
    app, and a cold start reads nothing until a key is tapped. The data key is held in memory
    while the app process lives, so **background alerts and the widget pause once Android has
    closed the app, until you unlock it again**. Losing every enrolled key means deleting the
    stored configs and importing them again ("I lost my security keys" on the lock screen).
  - **Adding a key** needs a tap (and, once, the key's FIDO PIN if the key insists on it for new
    credentials); with the requirement on, a second tap of the same key. Turning the requirement
    on asks for a tap of every enrolled key; turning it off asks for one tap. A key set to always
    verify the user (fingerprint or PIN on the key itself) computes another secret, so changing
    that setting after enrolment needs the requirement turned off and on again.
- **Keep last known state (Settings → Privacy, on by default):** the last data fetched from each
  cluster is kept on the phone, encrypted, for 24 hours and never backed up, so the home opens on
  it at once and still shows it when the cluster cannot be reached. Turn it off to keep cluster
  data in memory only (an install that had turned it off keeps it off). With it off but background
  alerts on, the home opens on the nodes the last background check saw, labelled as such, while
  it refreshes; never in screenshot mode.
- **Notifications:** with the app lock on, alerts show only a generic text on the lock screen.
  The widget shows counts only, no hostnames.
- **Kubeconfig export:** the kubeconfig is written only to the file you choose. Use it with
  kubectl or any Kubernetes client, then delete the file.

## etcd snapshots

**etcd → Backup → Save snapshot…** saves the etcd database, like `talosctl etcd snapshot`
(`os:operator`, `os:etcd:backup` or `os:admin`), to a file you choose. The snapshot holds every
Kubernetes Secret, so it is **encrypted with [age](https://age-encryption.org) by default**, while
it downloads: the clear database never touches the phone's storage. Choose how:

- **Public keys** (default once you have saved some for the cluster): one or more age (`age1…`),
  SSH (`ssh-ed25519`, `ssh-rsa`, e.g. `~/.ssh/id_ed25519.pub`) or YubiKey keys. For a YubiKey,
  paste the `age1tag1…` form of its key, printed by a recent
  [age-plugin-yubikey](https://github.com/str4d/age-plugin-yubikey); the older `age1yubikey1…`
  form needs the plugin to encrypt and is refused. The phone holds nothing that can decrypt the
  file. The keys are remembered per cluster (they are not secret).
- **Passphrase**: at least 12 characters, never stored.
- **None**: the old clear file, with a warning.

The file is named `etcd-<cluster>-<node>-<date>.snapshot.age`. After saving, **How to restore**
shows the commands for the file. On any Unix machine:

```sh
# install age: apt install age | dnf install age | pacman -S age | brew install age
age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot etcd-….snapshot.age   # public key (or -i key.txt)
age -d -i age-yubikey-identity-….txt -o etcd.snapshot etcd-….snapshot.age  # YubiKey, needs age-plugin-yubikey
age -d -o etcd.snapshot etcd-….snapshot.age                         # passphrase (prompts)
sha256sum etcd.snapshot   # must match the SHA-256 the app showed
talosctl -n <control-plane-ip> bootstrap --recover-from=./etcd.snapshot
```

Post-quantum `age1pq1…` / `age1tagpq1…` keys need age 1.3 or later to decrypt.

Or from the phone, when no etcd member answers any more: **etcd → Recover from snapshot…**
(os:admin) picks the file, asks for its passphrase or age secret key (`AGE-SECRET-KEY-1…`, never
stored), the control plane to recover on, the typed cluster name and an acknowledgement, then
uploads it and bootstraps etcd there with one member. The file is decrypted while it uploads: the
clear database never touches the phone's storage. A snapshot encrypted for a YubiKey or an SSH key
cannot be opened on the phone: decrypt it on a laptop first. The action is refused while any
member still answers (a recovery would split a live cluster). Afterwards, reset the other control
planes so they join the new one, as in the
[Talos disaster recovery guide](https://www.talos.dev/latest/advanced/disaster-recovery/).

### NOSPACE fix

When etcd raises its `NOSPACE` alarm, the cluster accepts no more writes. **etcd → Alarms → Fix
NOSPACE…** (os:admin) runs the usual sequence as one followed run:

1. A snapshot, as above, unless you switch it off.
2. A defragmentation of every member, one at a time: followers first, the leader last.
3. The alarm disarmed.
4. etcd read again.

The run stops at the first failure and says which member failed. When the alarm comes back at
once, the database is still over its quota: raise `quota-backend-bytes` in the machine config,
or delete data.

## AI diagnosis (optional)

Off by default. Turn it on in Settings → AI diagnosis; until then the app shows no trace of it
and never contacts a model provider.

- **What it does:** it reads the cluster state into a report, shows you that report, and only
  when you tap *Ask* sends it to the model you chose. The answer names the likely cause, the
  evidence and the steps to fix it, and is written as it arrives.
- **The report:** node readiness and stage, Talos services, memory, disks, clock offset, etcd,
  control-plane static pods, pods without a running container, recent warning and error events,
  the last 40 log lines of services that are not healthy and, with an os:admin talosconfig,
  the storage, database and backup operators that are not fine (Longhorn, Garage,
  CloudNativePG, Velero, cert-manager…), with the nodes involved. It never contains the
  talosconfig, a machine configuration or a kubeconfig.
- **Anonymized by default:** the nodes' names, IP addresses and domain are replaced with
  placeholders (`cp-1`, `10.0.0.7`, `homelab.lan`) before the report leaves the phone, and the
  real ones are put back in the answer on the phone. The mask only knows the cluster's own
  names: the rest of the log lines (pod names, other host names such as a registry or an API
  endpoint) is sent as it is, so read the report first if your logs are sensitive.
- **Providers:** Anthropic (Claude, default `claude-opus-5-5`) or OpenAI (default
  `gpt-6-astra`), with your own API key; any model can be typed or picked from the provider's
  list. The key is stored encrypted (Android Keystore, iOS Keychain) and is only sent over
  HTTPS, to the provider or to the server URL you set for it.
- **Your own server:** set *Server URL* to a gateway or a local model speaking the Anthropic
  Messages API (`https://host`) or the OpenAI Chat Completions API (`http://host:11434/v1`).
  A server that needs no key may use plain HTTP; the report then crosses your network in
  clear, so keep that to a network you trust.
- **Without an API key:** *Share with an assistant app* hands the same question and report to
  the Claude, ChatGPT or Gemini app (or any app taking text) through the system share sheet.
- **Check the answer:** a model can be wrong. Read a command before running it, especially one
  that resets a node or changes etcd membership.
- **Health check helper:** when the cluster health check fails, the health page shows *Explain
  with AI*: one tap sends the check's lines and failure, node readiness and recent warning and
  error events (no logs, no etcd), anonymized like the report, and the answer names the likely
  cause and the next two or three screens to check. *Continue in Diagnosis* opens the full
  report with the failure as the note.
- **Panel assistant:** on the Metrics screen, the sparkle button (or *Ask AI* in the panel
  editor) opens a chat with the same model to write a PromQL panel from a description, or to
  change the one being edited. The model gets the metric names your source knows and the
  built-in panels as examples; every query it proposes is run against your source before it is
  shown, and a query the source turns down goes back to the model, which gets two more tries.
  *Use this panel* fills the editor. What is sent: your messages, the source's metric names and
  the panel being edited, as they are (not anonymized); never the cluster's names or addresses.

From a workstation, the same code runs against a real cluster:

```sh
cd go
go run ./cmd/probe diagnose-report        # the anonymized report; nothing is sent
# These two send the report with the real names (not anonymized) and print the answer:
ANTHROPIC_API_KEY=... go run ./cmd/probe diagnose anthropic
OPENAI_API_KEY=... go run ./cmd/probe diagnose openai gpt-6-astra
# The health check helper: runs the health check, then prints what would be sent (anonymized,
# nothing is sent), or sends it with the real names and prints the answer
go run ./cmd/probe health-explain-report
ANTHROPIC_API_KEY=... go run ./cmd/probe health-explain anthropic
# The panel assistant: the metric names (nothing is sent), then one question with its checked panel
SRC='{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}'
go run ./cmd/probe prom-metrics "$SRC"
ANTHROPIC_API_KEY=... go run ./cmd/probe prom-chat "$SRC" anthropic "memory by namespace, top ten"
```

## Cluster insights

Open **Cluster insights** from the overview to compare selected effective settings or
record an incident. These features are read-only and work with `os:reader`.

- **Configuration drift:** compares DNS, NTP, interface MTUs, extension versions,
  Secure Boot, UKI and Talos versions. Without a saved baseline, each role is compared
  with its first reachable node; save a baseline to compare the same nodes over time.
  Differences can be intentional. Failed or unavailable fields stay unknown, and a
  node absent from the baseline is not treated as a configuration change. A DNS, NTP or MTU
  difference has **Fix with a patch…**: it opens that node's machine config with the
  reference's value already in the draft, on the review (`os:admin` to apply).
- **Incident recorder:** collects live Talos events and samples node readiness,
  service state, network link state and counters about every five seconds (plus API
  call time). It stops after ten minutes or when its screen leaves the foreground.
  Review the latest saved timeline when you return; starting a new session replaces
  it. The timeline retains up to 600 entries, with bounded detail per entry, and
  reports discarded older entries. Metric samples can be shown or hidden.
  Evidence cards show readiness, service health and link changes with status icons
  and colors. Metric cards format CPU percentages, device throughput, disk activity
  and average I/O time; network warnings use errors and drops from the sampled
  interval rather than historical totals. Expand **Technical details** for the
  original bounded payload. New samples retain a separate readable summary of up
  to eight disks, eight interfaces and eight collection errors, with omissions
  reported. Older truncated samples remain available as technical details; record
  a new session to get their formatted metrics.
- **Bottleneck metrics:** the node’s **Live** tab now includes CPU I/O wait and VM
  steal time, per-disk throughput/activity/average I/O time, and per-interface
  throughput/errors/drops. Rates use consecutive counters; rebooted nodes, new
  devices and failed reads do not produce misleading rate spikes.

Baselines and recordings are encrypted on the device, excluded from backups, and
separated by cluster and screenshot-mode settings. Delete them from the insights
screen. No machine configurations, kubeconfigs or raw logs are collected. Event
messages and node details can still contain operational information.

The recorder is a foreground troubleshooting tool, not continuous monitoring.
Sampled changes between polls can be missed. Events use node clocks, sampled
changes use the phone clock, and chronological proximity does not establish cause.
Disk activity alone does not establish saturation, especially on parallel devices.

## Updates

The Android app updates itself from this repository's GitHub releases (Settings → About → Updates):

- **When it checks:** once a day, or when you tap "Check now".
- **What it downloads:** the signed APK for the phone's ABI, after you confirm.
- **Verification before installing:**
  1. the SHA-256 checksum GitHub publishes for the file;
  2. the package name;
  3. that it is signed with exactly the installed app's key.
- **Installing:** Android shows its own confirmation, and asks once to allow
  "Install unknown apps" for Ichor.

Updates only work between builds signed with the same key, which means CI release builds with
the `ICHOR_KEYSTORE*` secrets set. A **debug** build, signed with your machine's debug key,
cannot be replaced by a release. To switch once:

1. uninstall the debug build;
2. install `ichor-vX.Y.Z-<abi>.apk` from a release;
3. re-import the talosconfig.

There is no self-update on iOS, where sideloaded apps are reinstalled with Sideloadly or AltStore.

## Build

APKs are split per ABI (`arm64-v8a` for almost all phones, `armeabi-v7a`, `x86_64`), with
compressed native libraries. A release APK is about 16 MB.

```sh
./build.sh            # debug APKs -> app/build/outputs/apk/debug/app-<abi>-debug.apk
./build.sh release    # needs ICHOR_KEYSTORE, ICHOR_KEYSTORE_PASSWORD, ICHOR_KEY_ALIAS, ICHOR_KEY_PASSWORD
just install           # builds, then installs the APK matching the connected device's ABI
./build.sh play       # Google Play App Bundle -> app/build/outputs/bundle/play/app-play.aab
```

The Play build is the release build without the self-updater (Play delivers updates) and
without the donation links, with feature funding through Play Billing instead (the billing
library is only in that build; the others get the no-op store in `app/src/foss`). Its store listing, privacy policy and Console answers are in
`fastlane/` (see [fastlane/PLAY_CONSOLE.md](fastlane/PLAY_CONSOLE.md)).

- **Default toolchain:** Nix (`flake.nix`). `BUILDER=docker ./build.sh` uses `build/Dockerfile`
  instead, and `IN_CONTAINER=1 ./build.sh` runs inside that image (CI, e.g. a Forgejo runner).
- **aarch64 Linux hosts:** the Android SDK and NDK are x86_64-only, and the Go runtime and the
  NDK clang crash under qemu-user. `build.sh` therefore keeps Go, gomobile, the JDK and Gradle
  native:
  - gomobile is built from a patched copy, because upstream refuses linux/arm64;
  - cgo uses a native clang (the NDK's version, 21 for r29) with the NDK sysroot (`.cache/ndk`);
  - only `aapt2` runs emulated.
- **Nix sandbox:** for the x86_64 SDK builds, `build.sh` exposes the binfmt qemu closure to the
  sandbox, which requires a trusted Nix user.
- **Page size:** native libraries are linked with 16 KB page alignment, as Android 15+ requires.

Go core only (fast, native):

```sh
cd go && go test ./...
go run ./cmd/probe overview   # also: services NODE, resources NODE, logs NODE SERVICE, dmesg NODE,
                              #       etcd, health, kubeconfig (prints size only), parse
go run ./cmd/probe -config ../talosconfig-phone overview   # test a role-limited config
```

## iOS

The iOS app (`ios/`) reuses the same Go core, built as `Ichorgo.xcframework` with
gomobile, around a SwiftUI UI. The pure logic lives in the `IchorCore` Swift package:
models, formatting, lock state and power-request rules.

**Feature parity with Android:**

- import from a file, by pasting, or by QR code (VisionKit);
- the config is encrypted to a Secure Enclave key (Keychain fallback without one);
- overview, node services, resources, logs and live graphs;
- etcd, KubeSpan and the cluster health check;
- reboot and shutdown with the same modes and typed confirmation;
- the debug shell (SwiftTerm terminal);
- kubeconfig export;
- background alerts and a home-screen widget;
- Face ID / passcode lock, security keys (NFC and the Lightning 5Ci; USB-C YubiKeys on iPhone
  are best effort), themes, and role-based actions.

**What iOS does differently:**

- **Self-update:** not possible for sideloaded apps; reinstall new releases with Sideloadly or
  AltStore.
- **Screenshots:** iOS does not let apps block them. The app-switcher privacy cover hides the
  content instead.
- **Background alerts:** best effort. iOS decides when background checks run, and skips them
  while the device is locked, because the config cannot be decrypted then; with a security key
  required, also after iOS closed the app, until it is unlocked again.
- **Leftover data:** Keychain items survive uninstalling the app. Use Settings → Clusters & backup → Delete to
  remove the config.
- **Widget:** shares the last check with the app through an App Group, which a free Apple ID
  may not support when sideloading. In that case the widget stays empty.

**Building:** Xcode only runs on macOS.

```sh
just ios-test-linux   # core package tests, here on Linux (Nix swift shell)
just ios-test         # macOS: xcframework + core tests + simulator build
just ios-build        # macOS: unsigned IPA in ios/build/
```

**Installing:** CI produces an unsigned IPA. Sideloadly or AltStore re-sign it with your Apple
ID: a free account means reinstalling every 7 days, a paid one lasts a year. To sign in Xcode
instead, set `ICHOR_IOS_TEAM_ID` before `xcodegen generate`.

**App Store:** `scripts/ios-build.sh appstore` builds the App Store variant, compiled with
`APP_STORE`, which hides the donation links (App Review Guideline 3.2.2), signs it through Xcode
automatic signing with an App Store Connect API key, and uploads it to TestFlight. On a `v*` tag,
`ios.yml` runs it once these secrets of the `release` environment are set:

| Secret | Value |
|---|---|
| `IOS_TEAM_ID` | the Apple Developer team ID |
| `ASC_KEY_ID` | the App Store Connect API key ID (Users and Access → Integrations, Admin role) |
| `ASC_ISSUER_ID` | the issuer ID shown above the keys |
| `ASC_KEY_P8` | the contents of `AuthKey_<id>.p8` |

Register `name.levis.ichor`, `name.levis.ichor.widget` and the `group.name.levis.ichor` App
Group in the developer portal, and create the app in App Store Connect, before the first upload.
What to request from Apple and declare in App Store Connect (multicast entitlement for
Wake-on-LAN, privacy label and manifest, export compliance, review notes) is in
[fastlane/APP_STORE_CONNECT.md](fastlane/APP_STORE_CONNECT.md).

## CI (GitHub Actions)

- **`.github/workflows/android.yml`** (Ubuntu, Nix flake): `./build.sh check`, then the debug
  and release APKs.
- **`.github/workflows/ios.yml`** (macOS 26): core tests, simulator build, unsigned IPA; on
  `v*` tags, the App Store upload (see [iOS](#ios)).

Both run on pushes to `main` and on pull requests. Release APKs are named
`ichor-v<version>-<abi>.apk`, or `…-unsigned.apk` when no keystore secrets are set;
the updater ignores unsigned ones. On a `v*` tag (`just release-tag 0.1.0`,
then `git push origin v0.1.0`), they attach the release APK and the IPA to the GitHub release.

To sign the release APK in CI, generate a key once and upload it as secrets of the `release`
environment (Settings → Environments; restricted to `v*` tags, so pull requests never see them):

```sh
just android-keystore-gen       # then export ICHOR_KEYSTORE, ICHOR_KEYSTORE_PASSWORD, ICHOR_KEY_ALIAS
just github-secrets             # sets the three secrets below with gh (values via stdin)
```

The secrets it sets:

| Secret | Value |
|---|---|
| `ICHOR_KEYSTORE_BASE64` | `base64 -w0 ~/ichor-release.jks` |
| `ICHOR_KEYSTORE_PASSWORD` | the keystore password |
| `ICHOR_KEY_ALIAS` | the key alias, e.g. `ichor` |

Without them, and on every build that isn't a `v*` tag, the release APK is unsigned.

## Languages

The app is available in English, French, Spanish, Ukrainian, German and Italian.

- **Android:** pick a language in Settings → Appearance → Language. On Android 13+, the
  system's per-app language settings work too.
- **iOS:** iOS sets the app's language in the Settings app; the app's Language row opens it.

When you add or change a UI string, translate it in all six languages:

| Platform | English source | Translations |
|---|---|---|
| Android | `app/src/main/res/values/strings.xml` | `values-{fr,es,uk,de,it}/strings.xml` |
| iOS | `ios/**/Localizable.xcstrings` | the same catalogs (String Catalogs) |

Then run:

```sh
just i18n-check   # also part of `just check` and the iOS CI build
```

It fails on a missing translation, or when placeholders (`%1$s`, `%@`, `%lld`) differ from
English.

## Support the project

Ichor is free, with no ads or tracking. To help keep it going:

- [GitHub Sponsors](https://github.com/sponsors/cyrinux)
- Bitcoin: `bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl`
- Ethereum: `0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804`

The app's About section and the [website](https://cyrinux.github.io/ichor/#support) show them as QR codes.

The Google Play version can't show those links (Play's payment rules). Instead,
**Settings → About → Fund features** lets you back the features you want next, with Google
Play purchases. The list is [`docs/roadmap.json`](docs/roadmap.json); the maintainer side is
in [fastlane/PLAY_CONSOLE.md](fastlane/PLAY_CONSOLE.md#feature-funding-in-app-products).

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE) for third-party components.
The Ichor name, logo and icon are not covered by the license; see [TRADEMARKS.md](TRADEMARKS.md).
To contribute, see [CONTRIBUTING.md](CONTRIBUTING.md).

### Android demo deep link

Use `ichor://demo` in Google Play Console's Pre-launch report deep link settings
after uploading a build containing this support. It opens the offline demo overview
and preserves imported clusters. An enabled app lock still requires unlock.

Test a stopped app, then repeat while it is open:

```sh
adb shell am force-stop name.levis.ichor
adb shell am start -W -a android.intent.action.VIEW -d 'ichor://demo' -p name.levis.ichor
adb shell am start -W -a android.intent.action.VIEW -d 'ichor://demo' -p name.levis.ichor
```
