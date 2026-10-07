package name.levis.ichor.model

/** Longest name one can give a cluster: it titles the overview and labels a launcher shortcut. */
const val CLUSTER_NAME_MAX = 40

/** A name typed for a cluster, trimmed; null (back to its talosconfig context name) when blank. */
fun normalizeClusterName(input: String): String? =
    input.trim().take(CLUSTER_NAME_MAX).trim().takeIf { it.isNotEmpty() }

/** The names of [saved] (by fingerprint) whose cluster is still among [fingerprints]. */
fun keepClusterNames(saved: Map<String, String>, fingerprints: List<String>): Map<String, String> {
    val known = fingerprints.filter { it.isNotBlank() }.toSet()
    return saved.filterKeys { it in known }
}

/**
 * A managed cluster as its cloud's CLI names its context: [provider] ([EKS], [GKE]), the
 * region or zone, the [owner] (AWS account ID, GCP project) and the cluster's own name.
 */
data class CloudContext(val provider: String, val location: String, val owner: String, val cluster: String) {
    /** The owner for a list row: an AWS account ID shortened (123456789012 becomes 12…12), a GCP project whole. */
    val shortOwner: String get() = if (provider == EKS && owner.length > 4) "${owner.take(2)}…${owner.takeLast(2)}" else owner

    companion object {
        const val EKS = "EKS"
        const val GKE = "GKE"
    }
}

// aws eks update-kubeconfig: the cluster ARN.
private val eksArnPattern = Regex("""arn:aws(?:-[a-z]+)*:eks:([a-z0-9-]+):(\d+):cluster/(.+)""")

// gcloud container clusters get-credentials: gke_<project>_<location>_<cluster>. Neither
// part has an underscore (a domain-scoped project is example.com:name).
private val gkePattern = Regex("""gke_([a-z0-9.:-]+)_([a-z0-9-]+)_([a-z0-9-]+)""")

/** [name] as an EKS or GKE context name, null when it is neither. */
fun parseCloudContext(name: String): CloudContext? =
    eksArnPattern.matchEntire(name)?.destructured?.let { (region, account, cluster) -> CloudContext(CloudContext.EKS, region, account, cluster) }
        ?: gkePattern.matchEntire(name)?.destructured?.let { (project, location, cluster) -> CloudContext(CloudContext.GKE, location, project, cluster) }

/**
 * How clusters are called on screen: the name the user gave one (by fingerprint, kept on
 * this device only), else its cluster name when its context name is an EKS or GKE one,
 * else its context name. Screenshot mode masks context names, so given names are not shown
 * then: they may be just as revealing.
 */
data class ClusterLabels(val names: Map<String, String> = emptyMap(), val masked: Boolean = false) {
    fun of(context: ContextSummary): String = given(context) ?: cloud(context)?.cluster ?: context.name

    /** [context]'s name as an EKS or GKE one, for the location and owner under its label; null when masked. */
    fun cloud(context: ContextSummary): CloudContext? = if (masked) null else parseCloudContext(context.name)

    /** The name the user gave [context], if any and if it may be shown. */
    fun given(context: ContextSummary): String? =
        if (masked || context.fingerprint.isBlank()) null else names[context.fingerprint]
}
