package name.levis.ichor.model

/**
 * The clusters that record actions after the update from the app-wide switch: every cluster
 * of [summary] but the demo when it was on ([legacyOn]), none when it was off or never set.
 */
fun seededAuditEventClusters(legacyOn: Boolean?, summary: ConfigSummary): Set<String> =
    if (legacyOn == true) {
        summary.contexts.filterNot { it.isDemo }.map { it.fingerprint }.filter { it.isNotBlank() }.toSet()
    } else {
        emptySet()
    }

/** The clusters that record actions, as the Go core takes them (Ichorgo.setAuditEventClusters). */
fun auditEventClustersArgument(fingerprints: Set<String>): String = fingerprints.sorted().joinToString("\n")
