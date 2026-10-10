package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_checkup*.go and kube_events.go.

/** The sections of a checkup ([CheckupSection.id]), in the order they are shown. */
object CheckupSectionId {
    const val WORKLOADS = "workloads"
    const val EVENTS = "events"
    const val STORAGE = "storage"
    const val UPGRADE = "upgrade"
    const val WEBHOOKS = "webhooks"
    const val CAPACITY = "capacity"
    const val NODES = "nodes"
    const val LOAD_BALANCERS = "loadbalancers"
    const val TERMINATING = "terminating"
    const val CERTIFICATES = "certificates"
    const val SECRETS = "secrets"
    const val HELM = "helm"
    const val MONITORING = "monitoring"
}

/** What a finding is about ([CheckupFinding.kind]); CheckupWording words each one. */
object CheckupKind {
    const val POD_CRASH_LOOP = "podCrashLoop"
    const val POD_IMAGE_PULL = "podImagePull"
    const val POD_UNSCHEDULABLE = "podUnschedulable"
    const val POD_STUCK_STARTING = "podStuckStarting"
    const val POD_NOT_READY = "podNotReady"
    const val POD_FAILED = "podFailed"
    const val POD_OOM_KILLED = "podOOMKilled"
    const val JOB_FAILED = "jobFailed"
    const val EVENT = "event"
    const val VOLUME_FULL = "volumeFull"
    const val VOLUME_INODES = "volumeInodes"
    const val PVC_PENDING = "pvcPending"
    const val PVC_LOST = "pvcLost"
    const val PV_FAILED = "pvFailed"
    const val PV_RELEASED = "pvReleased"
    const val DEPRECATED_API = "deprecatedAPI"
    const val WEBHOOK_DOWN = "webhookDown"
    const val WEBHOOK_SKIPPED = "webhookSkipped"
    const val NODE_REQUESTS_HIGH = "nodeRequestsHigh"
    const val NODE_PODS_FULL = "nodePodsFull"
    const val NO_ROOM_TO_DRAIN = "noRoomToDrain"
    const val QUOTA_NEAR_LIMIT = "quotaNearLimit"
    const val NODE_NOT_READY = "nodeNotReady"
    const val NODE_PRESSURE = "nodePressure"
    const val NODE_CORDONED = "nodeCordoned"
    const val NODE_VERSION_SKEW = "nodeVersionSkew"
    const val LB_PENDING = "lbPending"
    const val LB_POOL_EXHAUSTED = "lbPoolExhausted"
    const val LB_POOL_CONFLICT = "lbPoolConflict"
    const val NAMESPACE_TERMINATING = "namespaceTerminating"
    const val POD_TERMINATING = "podTerminating"
    const val PVC_TERMINATING = "pvcTerminating"
    /** A network test namespace the app could not delete (it was closed mid-run). */
    const val NETPERF_LEFTOVER = "netperfLeftover"
    const val CSR_PENDING = "csrPending"
    const val CSR_DENIED = "csrDenied"
    const val EXTERNAL_SECRET_FAILED = "externalSecretFailed"
    const val SECRET_STORE_NOT_READY = "secretStoreNotReady"
    const val HELM_FAILED = "helmFailed"
    const val HELM_PENDING = "helmPending"
    const val SCRAPE_TARGETS_DOWN = "scrapeTargetsDown"
    const val PROMETHEUS_RULE_ERRORS = "prometheusRuleErrors"
    /** Failing rules of a group no PrometheusRule writes (the Prometheus configuration's). */
    const val RULE_GROUP_ERRORS = "ruleGroupErrors"
}

/** A section's or the whole checkup's state, worst first. */
enum class CheckupStatus { CRITICAL, WARNING, OK, UNKNOWN, ABSENT }

enum class CheckupSeverity { CRITICAL, WARNING, INFO }

/** What the cluster hides behind green nodes: one section per kind of trouble. */
@Serializable
data class CheckupReport(
    val status: String = "",
    val kubeVersion: String = "",
    val sections: List<CheckupSection> = emptyList(),
    val nodes: List<CheckupNode> = emptyList(),
    val volumes: List<CheckupVolume> = emptyList(),
    val releases: List<CheckupRelease> = emptyList(),
)

/** One check: [checked] objects looked at, [truncated] findings left out, [error] when unreadable. */
@Serializable
data class CheckupSection(
    val id: String,
    val status: String = "",
    val error: String = "",
    val checked: Int = 0,
    val findings: List<CheckupFinding> = emptyList(),
    val truncated: Int = 0,
)

/** One problem. [reason] and [message] are Kubernetes' own words; the rest depends on [kind]. */
@Serializable
data class CheckupFinding(
    val kind: String,
    val severity: String = "",
    val namespace: String = "",
    val name: String = "",
    val node: String = "",
    val reason: String = "",
    val message: String = "",
    val extra: String = "",
    val value: Double = 0.0,
    val limit: Double = 0.0,
    val count: Int = 0,
    /** Unix ms, 0 when unknown. */
    val since: Long = 0,
)

/** A node as Kubernetes sees it: requests against allocatable (cores, bytes), taints and labels. */
@Serializable
data class CheckupNode(
    val name: String,
    val roles: List<String> = emptyList(),
    val ready: Boolean = false,
    val cordoned: Boolean = false,
    val kubelet: String = "",
    val taints: List<String> = emptyList(),
    val labels: List<String> = emptyList(),
    val cpuRequests: Double = 0.0,
    val cpuAllocatable: Double = 0.0,
    val cpuPercent: Double = 0.0,
    val memoryRequests: Double = 0.0,
    val memoryAllocatable: Double = 0.0,
    val memoryPercent: Double = 0.0,
    val pods: Int = 0,
    val podCapacity: Int = 0,
)

/** A PersistentVolumeClaim and how full it is; not [measured] when no kubelet reported it. */
@Serializable
data class CheckupVolume(
    val namespace: String,
    val name: String,
    val phase: String = "",
    val storageClass: String = "",
    val capacity: Double = 0.0,
    val used: Double = 0.0,
    val usedPercent: Double = 0.0,
    val inodesPercent: Double = 0.0,
    val measured: Boolean = false,
    val pod: String = "",
    val node: String = "",
)

/** A Helm release at its latest revision. */
@Serializable
data class CheckupRelease(
    val namespace: String,
    val name: String,
    val status: String = "",
    val revision: Int = 0,
    val updated: Long = 0,
)

private fun statusOf(value: String): CheckupStatus = when (value) {
    "critical" -> CheckupStatus.CRITICAL
    "warning" -> CheckupStatus.WARNING
    "ok" -> CheckupStatus.OK
    "absent" -> CheckupStatus.ABSENT
    else -> CheckupStatus.UNKNOWN
}

val CheckupReport.verdict: CheckupStatus get() = statusOf(status)

val CheckupSection.state: CheckupStatus get() = statusOf(status)

val CheckupFinding.level: CheckupSeverity
    get() = when (severity) {
        "critical" -> CheckupSeverity.CRITICAL
        "warning" -> CheckupSeverity.WARNING
        else -> CheckupSeverity.INFO
    }

/** "namespace/name", or the name alone for what has no namespace. */
val CheckupFinding.subject: String get() = if (namespace.isEmpty()) name else "$namespace/$name"

/** The sections worth showing: what the cluster does not run is left out. */
val CheckupReport.shownSections: List<CheckupSection> get() = sections.filter { it.state != CheckupStatus.ABSENT }

/** How many findings of [level] the whole report holds. */
fun CheckupReport.count(level: CheckupSeverity): Int = sections.sumOf { s -> s.findings.count { it.level == level } }

val CheckupRelease.inTrouble: Boolean get() = status == "failed" || status.startsWith("pending-")

/**
 * The findings worth a background alert, keyed "section|kind|subject" and valued by severity
 * ("critical" or "warning"): everything but the notes and the events, which only describe.
 */
fun CheckupReport.alertIssues(): Map<String, String> = buildMap {
    sections.forEach { section ->
        if (section.id == CheckupSectionId.EVENTS) return@forEach
        section.findings.forEach { f ->
            if (f.level != CheckupSeverity.INFO) put("${section.id}|${f.kind}|${f.subject}", f.severity)
        }
    }
}

/** A Kubernetes event of an object, as `kubectl describe` ends with. */
@Serializable
data class KubeEvent(
    val type: String = "",
    val reason: String = "",
    val message: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val count: Int = 1,
    val first: Long = 0,
    val last: Long = 0,
    val source: String = "",
)

@Serializable
data class KubeEventList(val events: List<KubeEvent> = emptyList())

val KubeEvent.isWarning: Boolean get() = type != "Normal"
