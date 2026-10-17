package name.levis.ichor.model

/** How an object's images are scanned: a pod's through the pod, a workload's by its spec's image refs. */
enum class ObjectScanMode { POD, WORKLOAD }

/** The workload resources KubeWorkloadsNamed reads (apps/v1). */
private val SCANNED_WORKLOADS = setOf("deployments", "statefulsets", "daemonsets")

/** How [this] object's images are scanned, null for a kind the scan does not take. */
val KubeObjectRef.scanMode: ObjectScanMode?
    get() = when {
        isPod -> ObjectScanMode.POD
        group == "apps" && resource in SCANNED_WORKLOADS -> ObjectScanMode.WORKLOAD
        else -> null
    }

private const val OBJECT_SCAN_PREFIX = "object:"

/** The scan id of an object's images, under which the core keeps its report: "object:<kind>:<ns>/<name>". */
fun objectScanId(kind: String, namespace: String, name: String) = "$OBJECT_SCAN_PREFIX$kind:$namespace/$name"

val KubeObjectRef.scanId: String get() = objectScanId(kind, namespace, name)

/** "Deployment web/api" for an object scan id, null for an app's or a node's. */
fun objectScanLabel(scanId: String): String? {
    val rest = scanId.removePrefix(OBJECT_SCAN_PREFIX).takeIf { it != scanId } ?: return null
    val kind = rest.substringBefore(':', "")
    val path = rest.substringAfter(':', "")
    if (kind.isEmpty() || path.isEmpty()) return null
    return "$kind $path"
}

/** The pod a Pod object's scan reads its images from. */
val KubeObjectRef.scanPods: List<RoutePod> get() = listOf(RoutePod(namespace, name))

/** The workload a workload object's scan reads its image refs from. */
val KubeObjectRef.scanWorkload: WorkloadRef get() = WorkloadRef(kind, namespace, name)
