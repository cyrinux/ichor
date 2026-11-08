package name.levis.ichor.model

// The cluster list, once long, in sections: per provider, and per account or project within
// one. Same rules on iOS (IchorCore ClusterGroups.swift).

/** From this many clusters the list is grouped: the page dots turn into a counter at the same size. */
const val CLUSTER_GROUP_FROM = 6

/** Where a cluster runs, by its bundled [logo]; [brand] is its name (not translated). */
enum class ClusterProvider(val logo: String, val brand: String) {
    TALOS("talos", "Talos"),
    OMNI("talos", "Omni"),
    AWS("aws", "AWS"),
    GCP("google-cloud", "Google Cloud"),
    AZURE("azure", "Azure"),
    DIGITALOCEAN("digital-ocean", "DigitalOcean"),
    RANCHER("rancher", "Rancher"),
    KUBERNETES("kubernetes", "Kubernetes"),
}

/**
 * A section of the cluster list: its [provider] and, when the contexts tell, the [owner] (a
 * shortened AWS account ID, a GCP project, an Omni identity). [key] tells groups apart; it is
 * not a title.
 */
data class ClusterGroup(
    val key: String,
    val logo: String,
    val provider: ClusterProvider,
    val owner: String? = null,
    val contexts: List<ContextSummary> = emptyList(),
)

private val logoProviders = ClusterProvider.entries.filter { it != ClusterProvider.OMNI }.associateBy { it.logo }

/**
 * The group [context] belongs to, without its contexts. Screenshot mode ([masked]) leaves the
 * owner out: the clusters of every account of a provider then share one group.
 */
fun clusterGroupKey(context: ContextSummary, masked: Boolean): ClusterGroup {
    val cloud = if (context.isKube) parseCloudContext(context.name) else null
    val provider = when {
        context.omni -> ClusterProvider.OMNI
        !context.isKube -> ClusterProvider.TALOS
        cloud != null -> if (cloud.provider == CloudContext.EKS) ClusterProvider.AWS else ClusterProvider.GCP
        else -> logoProviders[clusterLogo(context)] ?: ClusterProvider.KUBERNETES
    }
    // Whose clusters they are: the id keys the group, the owner titles it.
    val (id, owner) = when {
        masked -> null to null
        context.omni -> context.authKey.ifBlank { context.identity }.ifBlank { null } to context.identity.ifBlank { null }
        cloud != null && cloud.owner.isNotEmpty() -> cloud.owner to cloud.shortOwner
        else -> null to null
    }
    val key = listOfNotNull(provider.name.lowercase(), id).joinToString(":")
    return ClusterGroup(key, provider.logo, provider, owner)
}

/**
 * [contexts] in groups, in the order of each group's first cluster, the clusters in their stored
 * order; null (a flat list) under [CLUSTER_GROUP_FROM] clusters.
 */
fun clusterGroups(contexts: List<ContextSummary>, masked: Boolean): List<ClusterGroup>? {
    if (contexts.size < CLUSTER_GROUP_FROM) return null
    return contexts.map { it to clusterGroupKey(it, masked) }
        .groupBy { (_, group) -> group.key }
        .values
        .map { members -> members.first().second.copy(contexts = members.map { it.first }) }
}
