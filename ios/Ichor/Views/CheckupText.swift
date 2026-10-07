import Foundation

// Generated with Android's checkup_*, kube_events_*, monitor_checkup_* and rollback_* resources from
// one table: each text is a key of Localizable.xcstrings and must match it exactly. Arguments are
// already formatted strings, in the order the English sentence names them.

enum CheckupText {
    static var checkupTitle: String { String(localized: "Cluster checkup") }
    static var checkupIntro: String { String(localized: "What the other screens do not show, read from the Kubernetes API. Nothing is changed.") }
    static var checkupVerdictCritical: String { String(localized: "Needs attention") }
    static var checkupVerdictWarning: String { String(localized: "Warnings") }
    static var checkupVerdictOk: String { String(localized: "All clear") }
    static func checkupCounts(_ a: String, _ b: String) -> String { String(localized: "Critical: \(a)  ·  Warnings: \(b)") }
    static func checkupAllClear(_ a: String) -> String { String(localized: "Nothing found in \(a) checks.") }
    static var checkupNothingFound: String { String(localized: "Nothing found") }
    static func checkupNothingIn(_ a: String) -> String { String(localized: "Nothing found. Checked: \(a).") }
    static func checkupTruncated(_ a: String) -> String { String(localized: "And \(a) more, not shown.") }
    static func checkupShowAll(_ a: String) -> String { String(localized: "Show all (\(a))") }
    static var checkupShowLess: String { String(localized: "Show less") }
    static func checkupSectionError(_ a: String) -> String { String(localized: "Could not be read: \(a)") }
    static func checkupOnNode(_ a: String) -> String { String(localized: "On node \(a)") }
    static var checkupCpu: String { String(localized: "CPU") }
    static var checkupMemory: String { String(localized: "memory") }
    static var checkupRequestsTitle: String { String(localized: "Requests against allocatable, per node") }
    static func checkupPodsOf(_ a: String, _ b: String) -> String { String(localized: "\(a) / \(b) pods") }
    static func checkupCoresOf(_ a: String, _ b: String) -> String { String(localized: "\(a) / \(b) cores") }
    static var checkupTaintsTitle: String { String(localized: "Roles, taints and labels") }
    static var checkupCordoned: String { String(localized: "cordoned") }
    static var checkupNoTaint: String { String(localized: "no taint") }
    static func checkupLabels(_ a: String) -> String { String(localized: "Labels: \(a) (tap to show)") }
    static var checkupVolumesTitle: String { String(localized: "Volumes, fullest first") }
    static var checkupVolumeUnmeasured: String { String(localized: "Level unknown: not mounted, or its node did not answer") }
    static var checkupReleasesTitle: String { String(localized: "Releases") }
    static func checkupReleaseRevision(_ a: String, _ b: String) -> String { String(localized: "Revision \(a), \(b) ago") }
    static var checkupReading: String { String(localized: "Reading the cluster…") }
    static var checkupSectionWorkloads: String { String(localized: "Workloads") }
    static var checkupSectionWorkloadsHint: String { String(localized: "Pods that crash, cannot start or were killed, Jobs that failed") }
    static var checkupSectionEvents: String { String(localized: "Warning events") }
    static var checkupSectionEventsHint: String { String(localized: "What Kubernetes complained about in the last hour") }
    static var checkupSectionStorage: String { String(localized: "Volumes") }
    static var checkupSectionStorageHint: String { String(localized: "Claims nearly full, not bound or lost") }
    static var checkupSectionUpgrade: String { String(localized: "Upgrade readiness") }
    static var checkupSectionUpgradeHint: String { String(localized: "Deprecated API versions something still calls") }
    static var checkupSectionWebhooks: String { String(localized: "Admission webhooks") }
    static var checkupSectionWebhooksHint: String { String(localized: "Webhooks whose backend is down") }
    static var checkupSectionCapacity: String { String(localized: "Capacity") }
    static var checkupSectionCapacityHint: String { String(localized: "What pods request against what nodes offer, and quotas") }
    static var checkupSectionNodes: String { String(localized: "Nodes") }
    static var checkupSectionNodesHint: String { String(localized: "Cordoned, under pressure or out of step with the API server") }
    static var checkupSectionLoadbalancers: String { String(localized: "Load balancers") }
    static var checkupSectionLoadbalancersHint: String { String(localized: "Services waiting for an address, pools running out") }
    static var checkupSectionTerminating: String { String(localized: "Stuck deletions") }
    static var checkupSectionTerminatingHint: String { String(localized: "Namespaces, pods and claims that never finish terminating") }
    static var checkupSectionCertificates: String { String(localized: "Certificate requests") }
    static var checkupSectionCertificatesHint: String { String(localized: "Requests nobody approved") }
    static var checkupSectionSecrets: String { String(localized: "External secrets") }
    static var checkupSectionSecretsHint: String { String(localized: "Secrets that no longer sync from their store") }
    static var checkupSectionHelm: String { String(localized: "Helm releases") }
    static var checkupSectionHelmHint: String { String(localized: "Releases that failed or hang") }
    static func checkupPodCrashLoop(_ a: String) -> String { String(localized: "Crashes and restarts in a loop (\(a) restarts)") }
    static func checkupPodImagePull(_ a: String) -> String { String(localized: "Its image cannot be pulled: \(a)") }
    static func checkupPodUnschedulable(_ a: String) -> String { String(localized: "No node can take it, for \(a)") }
    static func checkupPodStuckStarting(_ a: String) -> String { String(localized: "Scheduled \(a) ago, its containers still do not start") }
    static func checkupPodNotReady(_ a: String, _ b: String) -> String { String(localized: "Runs but is not ready: \(a) of \(b) containers") }
    static func checkupPodFailed(_ a: String) -> String { String(localized: "Status \(a)") }
    static func checkupPodOOMKilled(_ a: String) -> String { String(localized: "Killed for using more memory than its limit (\(a) restarts)") }
    static func checkupJobFailed(_ a: String) -> String { String(localized: "The Job gave up \(a) ago") }
    static func checkupEvent(_ a: String, _ b: String, _ c: String) -> String { String(localized: "\(a), \(b) times, last \(c) ago") }
    static func checkupVolumeFull(_ a: String, _ b: String) -> String { String(localized: "\(a) used of \(b)") }
    static func checkupVolumeInodes(_ a: String) -> String { String(localized: "\(a) of its inodes are used") }
    static func checkupPvcPending(_ a: String, _ b: String) -> String { String(localized: "Not bound for \(a) (storage class \(b))") }
    static var checkupPvcLost: String { String(localized: "Its volume no longer exists") }
    static var checkupPvFailed: String { String(localized: "The volume is in the Failed phase") }
    static func checkupPvReleased(_ a: String) -> String { String(localized: "Kept after its claim \(a) was deleted") }
    static func checkupDeprecatedAPI(_ a: String) -> String { String(localized: "Still requested; Kubernetes \(a) removes it") }
    static var checkupDeprecatedAPIUnplanned: String { String(localized: "Deprecated, and still requested") }
    static func checkupWebhook(_ a: String, _ b: String) -> String { String(localized: "\(a) webhook: its Service \(b) has no ready endpoint") }
    static func checkupNodeRequestsHigh(_ a: String, _ b: String) -> String { String(localized: "\(a) of its \(b) is requested") }
    static func checkupNodePodsFull(_ a: String, _ b: String) -> String { String(localized: "\(a) pods of the \(b) the kubelet accepts") }
    static func checkupNoRoomToDrain(_ a: String) -> String { String(localized: "Its pods would not fit on the other nodes: not enough free \(a)") }
    static func checkupQuotaNearLimit(_ a: String, _ b: String) -> String { String(localized: "\(a) of the quota for \(b) is used") }
    static var checkupNodeNotReady: String { String(localized: "Kubernetes sees it as not ready") }
    static func checkupNodePressure(_ a: String) -> String { String(localized: "Under pressure: \(a)") }
    static func checkupNodeCordoned(_ a: String) -> String { String(localized: "Cordoned for \(a)") }
    static var checkupNodeCordonedPlain: String { String(localized: "Cordoned: no new pod is scheduled on it") }
    static func checkupNodeVersionSkew(_ a: String, _ b: String) -> String { String(localized: "Its kubelet is \(a), the API server \(b)") }
    static func checkupLbPending(_ a: String) -> String { String(localized: "No address for \(a)") }
    static func checkupLbPoolExhausted(_ a: String) -> String { String(localized: "No address left (pool of \(a))") }
    static var checkupLbPoolConflict: String { String(localized: "Overlaps another pool") }
    static func checkupTerminating(_ a: String) -> String { String(localized: "Deleting for \(a)") }
    static func checkupCsrPending(_ a: String, _ b: String) -> String { String(localized: "\(a) certificate requests wait for approval (\(b))") }
    static func checkupCsrDenied(_ a: String, _ b: String) -> String { String(localized: "\(a) certificate requests were denied (\(b))") }
    static func checkupExternalSecretFailed(_ a: String) -> String { String(localized: "Does not sync from store \(a)") }
    static func checkupSecretStoreNotReady(_ a: String) -> String { String(localized: "This \(a) cannot reach its backend") }
    static func checkupHelmFailed(_ a: String) -> String { String(localized: "Revision \(a) failed") }
    static func checkupHelmPending(_ a: String, _ b: String) -> String { String(localized: "Stuck in \(a) for \(b)") }
    static var checkupPodCrashLoopFix: String { String(localized: "Open its previous log from the Pods tab: the last lines before the exit say why. A wrong command, a missing config or secret, or a dependency that is down are the usual causes.") }
    static var checkupPodImagePullFix: String { String(localized: "Check the image name and tag, that the registry answers from the node, and the pull secret if the registry is private.") }
    static var checkupPodUnschedulableFix: String { String(localized: "The scheduler’s message says what is missing: free CPU or memory, a node matching its selector or affinity, a toleration for a taint, or a volume in the right zone.") }
    static var checkupPodStuckStartingFix: String { String(localized: "Usually a volume that does not attach or mount, a missing ConfigMap or Secret, or the network plugin failing to set the pod up: its events say which.") }
    static var checkupPodNotReadyFix: String { String(localized: "Its readiness probe fails: the app is up but refuses traffic. Check its log and what it depends on.") }
    static var checkupPodFailedFix: String { String(localized: "Read its log, then delete it: its controller starts a new one.") }
    static var checkupPodOOMKilledFix: String { String(localized: "Raise its memory limit, or find the leak: it will be killed again at the same point.") }
    static var checkupJobFailedFix: String { String(localized: "Read the log of its last pod, fix the cause, then run it again (CronJobs tab: Run now).") }
    static var checkupVolumeFullFix: String { String(localized: "Grow the claim if its storage class allows expansion, or free space: a full volume stops databases and corrupts what is being written.") }
    static var checkupVolumeInodesFix: String { String(localized: "Too many small files: delete some, or recreate the filesystem with more inodes. Free bytes do not help once inodes run out.") }
    static var checkupPvcPendingFix: String { String(localized: "Check that the storage class exists and that its provisioner runs; the claim’s events give the provisioner’s error.") }
    static var checkupPvcLostFix: String { String(localized: "The PersistentVolume was deleted under the claim. Restore the data from a backup into a new claim.") }
    static var checkupPvFailedFix: String { String(localized: "Its reclaim failed. Check the storage backend, then delete the PersistentVolume by hand.") }
    static var checkupPvReleasedFix: String { String(localized: "Its data is still there (Retain). Delete the PersistentVolume when you no longer need it, or clear its claimRef to reuse it.") }
    static var checkupDeprecatedAPIFix: String { String(localized: "Something still calls this API version: update the manifests, charts or controllers that use it before upgrading Kubernetes. The audit analysis on the API server screen names the client.") }
    static var checkupWebhookDownFix: String { String(localized: "With failurePolicy Fail, every request it matches is refused until its pods are back: creating pods can be blocked cluster-wide. Restart its Deployment, or delete the webhook configuration if the tool is gone.") }
    static var checkupWebhookSkippedFix: String { String(localized: "With failurePolicy Ignore, requests go through without it: what it validates or injects is silently not applied.") }
    static var checkupNodeRequestsHighFix: String { String(localized: "New pods will no longer fit here. Add a node, lower the requests that are larger than real usage, or move workloads.") }
    static var checkupNodePodsFullFix: String { String(localized: "Past the limit, pods stay Pending whatever CPU and memory are free. Spread the pods, or raise maxPods in the kubelet configuration.") }
    static var checkupNoRoomToDrainFix: String { String(localized: "Draining it for an upgrade, or losing it, would leave pods Pending. Keep one node’s worth of requests free across the cluster. A rough sum: it ignores affinities.") }
    static var checkupQuotaNearLimitFix: String { String(localized: "Once the quota is full, the namespace’s new pods and claims are refused. Raise the quota, or free what is unused.") }
    static var checkupNodeNotReadyFix: String { String(localized: "Its kubelet no longer reports: check the node’s services and kernel log from its page.") }
    static var checkupNodePressureFix: String { String(localized: "The kubelet evicts pods while this lasts. Free the resource on the node, or move workloads away.") }
    static var checkupNodeCordonedFix: String { String(localized: "No new pod is scheduled on it. Uncordon it from its page once the maintenance is over.") }
    static var checkupNodeVersionSkewFix: String { String(localized: "A kubelet may be up to three minor versions older than the API server, never newer. Finish the Kubernetes upgrade on this node.") }
    static var checkupLbPendingFix: String { String(localized: "Nothing gave this LoadBalancer Service an IP: the pool is empty, no pool matches its labels or class, or the load balancer controller is down.") }
    static var checkupLbPoolExhaustedFix: String { String(localized: "Add addresses to the pool, or free those of Services you no longer need.") }
    static var checkupLbPoolConflictFix: String { String(localized: "Cilium disables a pool that conflicts with another. Fix the ranges so they no longer overlap.") }
    static var checkupNamespaceTerminatingFix: String { String(localized: "Something inside cannot be removed: usually a resource whose finalizer’s controller is gone, or an API that no longer answers. Remove what the message names; clearing finalizers by hand is the last resort.") }
    static var checkupPodTerminatingFix: String { String(localized: "Its node may be unreachable, or a finalizer holds it. If the node is gone for good, force the deletion with kubectl delete --force.") }
    static var checkupPvcTerminatingFix: String { String(localized: "A pod still mounts it (pvc-protection): the claim goes once that pod is deleted.") }
    static var checkupCsrPendingFix: String { String(localized: "Nobody approves them: kubelet serving certificates need an approver (kubelet-serving-cert-approver) or kubectl certificate approve. Until then logs, exec and metrics from this node fail.") }
    static var checkupCsrDeniedFix: String { String(localized: "The approver refused them: its message says why.") }
    static var checkupExternalSecretFailedFix: String { String(localized: "The Secret keeps its last value, or does not exist. Check the store’s credentials and that the remote key exists.") }
    static var checkupSecretStoreNotReadyFix: String { String(localized: "Every ExternalSecret using it stops syncing. Check its credentials, its address and the provider’s status.") }
    static var checkupHelmFailedFix: String { String(localized: "Run helm history to see why, then helm rollback or a new upgrade.") }
    static var checkupHelmPendingFix: String { String(localized: "A Helm operation was interrupted and left its lock: helm rollback to the last deployed revision releases it.") }
    static var kubeEventsTitle: String { String(localized: "Events") }
    static var kubeEventsEmpty: String { String(localized: "No event. Kubernetes keeps them for one hour.") }
    static func kubeEventsAgo(_ a: String) -> String { String(localized: "\(a) ago") }
    static var monitorCheckup: String { String(localized: "Cluster checkup") }
    static var monitorCheckupDesc: String { String(localized: "Also alerts on what the checkup finds: pods that crash or cannot start, volumes nearly full, admission webhooks without a backend, load balancers without an address. Each check uses the Kubernetes API (os:admin), lists every pod and asks each kubelet for its volumes.") }
    static func monitorCheckupOk(_ a: String) -> String { String(localized: "Resolved: \(a)") }
    static var rollbackMenu: String { String(localized: "Roll back to the previous Talos") }
    static func rollbackTitle(_ a: String) -> String { String(localized: "Roll back \(a)") }
    static var rollbackConfirm: String { String(localized: "Roll back") }
    static var rollbackBody: String { String(localized: "The node reboots now into the Talos version it ran before its last upgrade, without draining its pods. For an upgrade that boots but misbehaves.") }
    static func rollbackStarted(_ a: String) -> String { String(localized: "Rollback started: \(a) is rebooting") }
}
