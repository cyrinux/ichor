package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/join_inspect.go.

/** What a node in maintenance mode (booted from the Talos ISO, not installed) says about itself. */
@Serializable
data class MaintenanceInspection(
    val address: String = "",
    val version: String = "",
    val arch: String = "",
    val platform: String = "",
    /** Null when unavailable. */
    val system: SystemInfo? = null,
    val disks: List<DiskInfo> = emptyList(),
    /** Physical links only. */
    val links: List<LinkInfo> = emptyList(),
    val addresses: List<AddressInfo> = emptyList(),
    /** False for a node already installed: it asks for a client certificate, and nothing was read. */
    val maintenance: Boolean = false,
    /** Section ("system", "disks", "links", "addresses") -> error. */
    val errors: Map<String, String> = emptyMap(),
) {
    /** The addresses of [link], as prefixes ("192.168.1.20/24"). */
    fun addressesOn(link: String): List<String> = addresses.filter { it.link == link }.map { it.address }
}

/** The address as typed, ready to send: no blanks; empty when there is nothing to inspect. */
fun joinAddress(typed: String): String = typed.trim()

// Mirrors go/ichorgo/join_config.go.

/** Step 2: the new node's machine config, made from a member's, with its secrets hidden. */
@Serializable
data class JoinConfigPreview(
    val redactedYaml: String = "",
    val warnings: List<String> = emptyList(),
    val needsBootstrap: Boolean = false,
    /** Bytes of the full config the node will receive. */
    val size: Int = 0,
    val role: String = "",
    val hostname: String = "",
    val template: String = "",
)

/** What a member's role makes of the new node: "init" (an old bootstrap node) joins as a control plane. */
fun joinRole(role: String): String = if (role == "init") "controlplane" else role

/** The members a config can be taken from: those that answer and have a role, workers first. */
fun joinTemplates(nodes: List<NodeOverview>): List<NodeOverview> = nodes
    .filter { it.reachable && joinRole(it.role) in setOf("worker", "controlplane") }
    .sortedWith(compareBy({ joinRole(it.role) != "worker" }, { it.hostname }))

/** "<role>-<n>" with the lowest n that no member's host name takes yet, from 1. */
fun suggestedHostname(role: String, hostnames: Collection<String>): String {
    val taken = hostnames.toSet()
    return generateSequence(1) { it + 1 }.map { "${joinRole(role)}-$it" }.first { it !in taken }
}

/** The disks a config can install to: writable ones, by their device path. */
fun installDisks(disks: List<DiskInfo>): List<String> = disks.filter { !it.readonly && it.devPath.isNotBlank() }.map { it.devPath }
