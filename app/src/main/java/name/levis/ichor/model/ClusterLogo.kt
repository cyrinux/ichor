package name.levis.ichor.model

// The logo the cluster selector shows for a cluster: a bundled icon (assets/appicons, see
// scripts/sync-app-icons.py CLUSTER_ICONS). Same rules on iOS (IchorCore ClusterLogo.swift).

private const val LOGO_TALOS = "talos"
private const val LOGO_KUBERNETES = "kubernetes"
private const val LOGO_AWS = "aws"
private const val LOGO_GCP = "google-cloud"

/** The cloud a kubeconfig context signs in to, by its sign-in method (the Go core's auth). */
private val authLogos = mapOf(
    "eks" to LOGO_AWS,
    "gke" to LOGO_GCP,
    "azure" to "azure",
    "digitalocean" to "digital-ocean",
    "rancher" to "rancher",
)

/** The managed services' API server hosts (GKE's is a bare IP). */
private val hostLogos = mapOf(
    ".eks.amazonaws.com" to LOGO_AWS,
    ".azmk8s.io" to "azure",
    ".k8s.ondigitalocean.com" to "digital-ocean",
)

/**
 * The bundled logo of [context]'s cluster: Talos for a talosconfig context, else its cloud when
 * the sign-in method, the context name (EKS ARN, gke_…) or the API server host tells, else Kubernetes.
 */
fun clusterLogo(context: ContextSummary): String {
    if (!context.isKube) return LOGO_TALOS
    authLogos[context.auth]?.let { return it }
    when (parseCloudContext(context.name)?.provider) {
        CloudContext.EKS -> return LOGO_AWS
        CloudContext.GKE -> return LOGO_GCP
    }
    val host = context.endpoints.firstOrNull().orEmpty().substringAfter("://").substringBefore('/').substringBefore(':').lowercase()
    return hostLogos.entries.firstOrNull { host.endsWith(it.key) }?.value ?: LOGO_KUBERNETES
}
