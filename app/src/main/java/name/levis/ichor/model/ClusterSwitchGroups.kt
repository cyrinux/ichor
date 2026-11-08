package name.levis.ichor.model

/** One switcher entry per physical cluster, preserving the order of its first context. */
fun clusterSwitchGroups(contexts: List<ContextSummary>): List<List<ContextSummary>> =
    contexts.groupBy { it.clusterId.ifBlank { it.fingerprint.ifBlank { it.name } } }.values.toList()
