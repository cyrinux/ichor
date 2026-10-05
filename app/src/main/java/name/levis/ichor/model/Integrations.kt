package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** The operators a cluster runs that Ichor does not show yet, read from its API groups. */
@Serializable
data class IntegrationReport(
    /** The unsupported groups, by operator ("istio.io": networking, security…). */
    val families: List<IntegrationFamily> = emptyList(),
    /** The groups served that Ichor already reads. */
    val supported: List<String> = emptyList(),
)

@Serializable
data class IntegrationFamily(
    val id: String,
    val groups: List<IntegrationGroup> = emptyList(),
)

@Serializable
data class IntegrationGroup(
    val name: String,
    val version: String = "",
    val kinds: List<String> = emptyList(),
) {
    val label: String get() = if (version.isEmpty()) name else "$name/$version"
}

/** This family with only the groups named in [picked]: what an integration request carries. */
fun IntegrationFamily.picking(picked: Set<String>): IntegrationFamily = copy(groups = groups.filter { it.name in picked })
