package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo nodeFeatures: what a node's Talos version can do.

@Serializable
data class FeatureSupport(
    val supported: Boolean = true,
    /** Oldest Talos version with the feature, e.g. "v1.15"; empty when unknown. */
    val minVersion: String = "",
    val reason: String = "",
) {
    companion object {
        /** Used while nothing is known: never block on a guess, the node answers for itself. */
        val UNKNOWN = FeatureSupport()
    }
}

@Serializable
data class NodeFeatures(
    val version: String = "",
    val features: Map<String, FeatureSupport> = emptyMap(),
)

/** Features whose availability depends on the node's Talos version; [key] is the Go name. */
enum class TalosFeature(val key: String) {
    EVENTS("events"),
    CONTAINERS("containers"),
    CONTAINER_RESTART("containerRestart"),
    PROCESSES("processes"),
    LOG_FOLLOW("logFollow"),
    SERVICE_CONTROL("serviceControl"),
    PACKET_CAPTURE("packetCapture"),
    UPGRADE("upgrade"),
    VOLUMES("volumes"),
    DISK_USAGE("diskUsage"),
    MOUNTS("mounts"),
    KUBESPAN("kubespan"),
    ETCD("etcd"),
    ETCD_SNAPSHOT("etcdSnapshot"),
    ETCD_MEMBER_ACTIONS("etcdMemberActions"),
    RESOURCE_BROWSER("resourceBrowser"),
    SUPPORT_BUNDLE("supportBundle"),
    DISK_HEALTH("diskHealth"),
    ISSUE_CONFIG("issueConfig"),
    NETWORK("network"),
    CONNECTIONS("connections"),
    TIME("time"),
    HARDWARE("hardware"),
    IMAGES("images"),
    IMAGE_PULL("imagePull"),
    MACHINE_CONFIG("machineConfig"),
    DEBUG_SHELL("debugShell"),
    RESET("reset"),
}

/** Support of [feature] on this node; supported while unknown (features not loaded, new name). */
fun NodeFeatures?.support(feature: TalosFeature): FeatureSupport = this?.features?.get(feature.key) ?: FeatureSupport.UNKNOWN

/**
 * Support of a cluster-wide [feature] given the features of the reachable nodes: available
 * as soon as one node has it (the others report their own error inline), and when nothing
 * is known. Unsupported everywhere: the lowest version that would bring it.
 */
fun clusterSupport(nodes: Collection<NodeFeatures>, feature: TalosFeature): FeatureSupport {
    val known = nodes.mapNotNull { it.features[feature.key] }
    if (known.isEmpty() || known.any { it.supported }) return FeatureSupport.UNKNOWN
    return known.filter { it.minVersion.isNotBlank() }.minWithOrNull { a, b -> compareVersions(a.minVersion, b.minVersion) }
        ?: known.first()
}

/** "1.15" → "v1.15"; blank stays blank. */
fun displayVersion(version: String): String {
    val v = version.trim()
    return if (v.isEmpty() || v.startsWith("v")) v else "v$v"
}

/** Compares "v1.9" and "1.15.2" numerically, part by part; missing or odd parts count as 0. */
fun compareVersions(a: String, b: String): Int {
    fun parts(v: String) = v.trim().removePrefix("v").split('.').map { p -> p.takeWhile(Char::isDigit).toIntOrNull() ?: 0 }
    val pa = parts(a)
    val pb = parts(b)
    for (i in 0 until maxOf(pa.size, pb.size)) {
        val diff = (pa.getOrNull(i) ?: 0).compareTo(pb.getOrNull(i) ?: 0)
        if (diff != 0) return diff
    }
    return 0
}

/** Why something is unavailable on a node's Talos version: [minVersion] ("v1.15") when known. */
data class VersionNotice(val minVersion: String)

private val NEEDS_TALOS = Regex("""needs? Talos (v?\d+\.\d+(?:\.\d+)?) or newer""", RegexOption.IGNORE_CASE)
private val UNAVAILABLE_PREFIXES = listOf("unimplemented:", "unsupported:")
private val UNAVAILABLE_MARKERS = listOf("not available on this node's talos version", "not available on this talos version")

/**
 * The notice behind a Go error [message] meaning "this node's Talos version cannot do that"
 * (shown as information, not as a failure), or null for any other error.
 */
fun versionNotice(message: String): VersionNotice? {
    NEEDS_TALOS.find(message)?.let { return VersionNotice(displayVersion(it.groupValues[1])) }
    val lower = message.lowercase()
    // gRPC Unimplemented: the node's API has no such method (see friendlyError in the Go core).
    if (UNAVAILABLE_PREFIXES.any(lower::startsWith) || UNAVAILABLE_MARKERS.any { it in lower }) return VersionNotice("")
    return null
}

/**
 * The notice for an unsupported feature, or null when it is supported. The reason decides
 * when there is one (a feature removed from newer Talos has a minimum version too).
 */
val FeatureSupport.notice: VersionNotice?
    get() = when {
        supported -> null
        reason.isNotBlank() -> versionNotice(reason) ?: VersionNotice("")
        else -> VersionNotice(displayVersion(minVersion))
    }
