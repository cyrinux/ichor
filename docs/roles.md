# Features and the talosconfig role they need

Part of the [Ichor documentation](../README.md#documentation).


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
| **Clean up dead pods (k9s sanitize)** | `Kubeconfig`, then the Kubernetes API (`list` and `delete` on pods in the namespace) | **`os:admin`** |
| **Kubernetes CronJobs, run now** | `Kubeconfig`, then the Kubernetes API (cronjobs, jobs) | **`os:admin`** |
| **CSI snapshots, snapshot creation and backup CronJob health** | `Kubeconfig`, then the Kubernetes API (volumesnapshots, volumesnapshotcontents, volumesnapshotclasses, PVCs, storageclasses, cronjobs, jobs) | **`os:admin`** |
| **CloudNativePG replica promotion** | `Kubeconfig`, then the Kubernetes API (clusters/status, pods and optional pods/proxy for lag) | **`os:admin`** |
| **Velero: run a schedule now, delete a backup** | `Kubeconfig`, then the Kubernetes API (velero.io schedules, backups, deletebackuprequests) | **`os:admin`** |
| **Velero: restore a backup with a preview and namespace mapping** | `Kubeconfig`, then the Kubernetes API (velero.io backups, restores, backupstoragelocations) | **`os:admin`** |
| **kube-green sleep schedules** | `Kubeconfig`, then the Kubernetes API (sleepinfos.kube-green.com, cluster-wide and one namespace's own when a workload or CronJob sheet opens; optional controller Deployment in `kube-green`) | **`os:admin`** |
| **Edit a kube-green schedule** | `Kubeconfig`, then the Kubernetes API (merge patch of sleepinfos.kube-green.com: `patch` in its namespace; the button is off with the reason without it) | **`os:admin`** |
| **Data services: ExternalDNS hostnames and controllers** | `Kubeconfig`, then the Kubernetes API (deployments, ingresses, services, optional dnsendpoints.externaldns.k8s.io and httproutes/gateways) | **`os:admin`** |
| **Data services: Sealed Secrets status and sealing keys** | `Kubeconfig`, then the Kubernetes API (sealedsecrets.bitnami.com, deployments, secrets metadata with the sealing-key label) | **`os:admin`** |
| **Data services: External Secrets sync status and stores** | `Kubeconfig`, then the Kubernetes API (`list` on externalsecrets.external-secrets.io; optional secretstores, clustersecretstores and secrets with the `reconcile.external-secrets.io/managed=true` label, values masked) | **`os:admin`** |
| **Numaflow pipelines, vertices and buffer services** | `Kubeconfig`, then the Kubernetes API (`list` on pipelines, vertices, monovertices and interstepbufferservices in numaflow.numaproj.io; one it may not list is reported and the others still show) | **`os:admin`** |
| **Pause or resume a Numaflow pipeline or mono vertex** | `Kubeconfig`, then the Kubernetes API (merge patch of `spec.lifecycle.desiredPhase`: `patch` on pipelines or monovertices in their namespace) | **`os:admin`** |
| **Temporal clusters, namespaces and schedules** | `Kubeconfig`, then the Kubernetes API (`list` on temporalclusters, temporalnamespaces, temporalschedules and temporalclusterclients in temporal.io; a client's certificate Secret by name only) | **`os:admin`** |
| **Pause or unpause a Temporal schedule** | `Kubeconfig`, then the Kubernetes API (merge patch of `spec.schedule.state.paused`: `patch` on temporalschedules in their namespace) | **`os:admin`** |
| **Temporal worker deployments** | `Kubeconfig`, then the Kubernetes API (`list` on workerdeployments, connections, clusterconnections and workerresourcetemplates in temporal.io; connection Secrets by name only) | **`os:admin`** |
| **Refresh an ExternalSecret** | `Kubeconfig`, then the Kubernetes API (`get` and `patch` on externalsecrets.external-secrets.io in its namespace: merge patch of the `external-secrets.io/force-sync` annotation; the button is off with the reason without `patch`, and on a `CreatedOnce` secret) | **`os:admin`** |
| **Fail over a Dragonfly instance** | `Kubeconfig`, then the Kubernetes API (`get` and `delete` on pods in the instance's namespace: deletes the master pod, pinned to its uid; the button is off with the reason without `delete`, on a replica, and on a master with no ready replica; the operator then promotes the most up-to-date ready replica, and writes it had not received yet are lost) | **`os:admin`** |
| **Dataflow jobs declared through Config Connector** | `Kubeconfig`, then the Kubernetes API (`list` on dataflowjobs and dataflowflextemplatejobs of dataflow.cnrm.cloud.google.com; jobs launched outside Config Connector are not shown) | **`os:admin`** |
| **App web addresses (Ingress, HTTPRoute, GRPCRoute, TLSRoute, Traefik IngressRoute)** | `Kubeconfig`, then the Kubernetes API (pods, services, ingresses, httproutes, grpcroutes, tlsroutes, gateways, ingressroutes, ingressroutetcps) | **`os:admin`** |
| **Network test between two nodes** | `Kubeconfig`, then the Kubernetes API (namespaces, pods) | **`os:admin`** |
| **Node pressure (PSI) and cgroups (like `talosctl cgroups`)** | `Copy` of `/sys/fs/cgroup`, `Containers` | **`os:admin`** |
| **Policies (Kyverno)** | `Kubeconfig`, then the Kubernetes API (kyverno.io policies, wgpolicyk8s.io policy reports, Kyverno's Deployments and webhooks) | **`os:admin`** |
| **Virtual machines: list, start/stop/restart/pause/migrate, serial console** | `Kubeconfig`, then the Kubernetes API (kubevirt.io, subresources.kubevirt.io including `virtualmachineinstances/console`, cdi.kubevirt.io) | **`os:admin`** |
| **Cluster checkup, events of a pod or workload** | `Kubeconfig`, then the Kubernetes API (pods, nodes, events, claims, webhooks, quotas, services, CSRs, secrets metadata…), each kubelet's `stats/summary` through the API server proxy, and a TLS handshake with each web address to read the certificate it serves (never the Secret, whose private key would come along) | **`os:admin`** |
| **Node taints and labels: edit** | `Kubeconfig`, then the Kubernetes API (`patch nodes`, with a dry run; pods of the node for the eviction preview) | **`os:admin`** |
| **Talos rollback (`talosctl rollback`)** | `Rollback` | **`os:admin`** |
| **CloudNativePG primary restart and hibernate/wake** | `Kubeconfig`, then the Kubernetes API: get clusters and pods, patch `clusters/status` for restart, patch clusters for hibernation | **`os:admin`** |
| **Longhorn faulted-volume salvage** | `Kubeconfig`, then the Kubernetes API: get volumes, list replicas/nodes, create `services/proxy` to the Longhorn manager | **`os:admin`** |
| **Longhorn volume attach/detach** | `Kubeconfig`, then the Kubernetes API: get volumes/nodes, list nodes for preview, create `services/proxy` to the Longhorn manager | **`os:admin`** |
| **Longhorn engine upgrade** | `Kubeconfig`, then the Kubernetes API: get volumes and settings, list engineimages and replicas, create `services/proxy` to the Longhorn manager | **`os:admin`** |
| **Node reset (`talosctl reset`)** | `Reset` | **`os:admin`** |
| **Boot a new machine: Image Factory ISO and iPXE links, `booter` command** | `Version`, COSI `MachineConfig` (the member's install image, for its schematic; with `os:reader` the default schematic is used, with a warning); on Omni, `GetMachineJoinConfig` for the join arguments (Omni's Operator role) | **`os:admin`** |
| **Create a Hetzner server for a new node** | The user data below (`os:admin` on plain Talos), `ClusterMember` and the node's own reads while following it; on Omni, `MachineStatuses` (Omni's Reader role). At Hetzner, a project token with Read & Write | **`os:admin`** |
| **Cloud host user data for a new node** | Plain Talos: `Version` and COSI `MachineConfig` (sensitive) of the template member, as for its config; on Omni, `GetMachineJoinConfig` for the join config (Omni's Operator role) | **`os:admin`** |
| **Rotate the Talos API CA (`talosctl rotate-ca --kubernetes=false`)** | COSI `OSRoot` and `MachineConfig` (sensitive), `ApplyConfiguration` (no reboot), `Version` on every node | **`os:admin`** |

Notes:

- **Source:** the rules come from Talos v1.14 (`internal/app/machined/pkg/system/services/machined.go`).
- **Health check:** the call itself is allowed for `os:reader`. Talos then runs the check with the
  caller's roles, and the Kubernetes checks need an admin kubeconfig. With a lower role, the
  overview and etcd screens give the same node and etcd picture.
- **Live cluster usage:** above 24 nodes, each sample (`SystemStat`, `Memory`) asks only the nodes
  with a problem and those on screen, and every node once a minute, the totals keeping each node's
  last counters, so the summary card says "Live: N of M nodes" in between.
- **Roles combine:** an `os:operator` config can do everything an `os:reader` one can.
- **Kubernetes access:** see [Kubernetes clusters without Talos](clusters.md#kubernetes-clusters-without-talos)
  to use your own kubeconfig identity instead of the admin kubeconfig (no `os:admin` needed).
- **Omni:** a cluster managed by Omni has no talosconfig roles; Omni's own role for the identity
  applies, see [Omni clusters](clusters.md#omni-clusters).
- **Kubernetes workloads:** the app asks Talos for an admin kubeconfig and keeps it in memory
  only, for 30 minutes; it is never written to disk. The phone must reach the Kubernetes API
  (port 6443): when the kubeconfig's address (often a VIP or an internal name) does not answer,
  the app tries the Talos endpoints on the same port.
