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

/** [disk] is one of [node]'s writable disks, or the node listed none (a typed path then). */
fun joinDiskFits(disk: String, node: MaintenanceInspection): Boolean =
    installDisks(node.disks).let { it.isEmpty() || disk in it }

// Mirrors go/ichorgo/join_apply.go.

/** One step of a node join, as StartNodeJoin reports it. [address]: where the node is followed. */
@Serializable
data class JoinProgress(
    val phase: String = "",
    val message: String = "",
    val at: Long = 0,
    val address: String = "",
)

/** The phases of a join, in order. */
enum class JoinPhase(val wire: String) {
    KEY("key"),
    CREATING("creating"),
    STARTING("starting"),
    APPLYING("applying"),
    INSTALLING("installing"),
    REBOOTING("rebooting"),
    BOOTED("booted"),
    JOINING("joining"),
    OMNI("omni"),
    REGISTERING("registering"),
    RESCUE("rescue"),
    WRITING("writing"),
    SNAPSHOT("snapshot"),
    CLEANUP("cleanup"),
    DONE("done"),
    ;

    companion object {
        fun of(wire: String): JoinPhase? = entries.firstOrNull { it.wire.equals(wire, ignoreCase = true) }

        /** A node in maintenance mode that gets its config from the app. */
        val maintenanceRun = listOf(APPLYING, INSTALLING, REBOOTING, BOOTED, JOINING, REGISTERING, DONE)

        /** A server created with the config as its user data. */
        val cloudRun = listOf(CREATING, STARTING, REBOOTING, BOOTED, JOINING, REGISTERING, DONE)

        /** A server created with Omni's join config. */
        val cloudOmniRun = listOf(CREATING, STARTING, OMNI, DONE)

        /** A throwaway server that becomes a Talos snapshot. */
        val snapshotRun = listOf(KEY, CREATING, RESCUE, WRITING, SNAPSHOT, CLEANUP, DONE)
    }
}

data class JoinStep(val phase: JoinPhase, val status: StepStatus, val at: Long = 0, val message: String = "")

/**
 * The join's timeline over [phases] from the [events] so far, like [maintenanceTimeline]: phases
 * before the latest one are done, the latest is current (failed when [failed]), the rest
 * pending; all done once [finished] without failure.
 */
fun joinTimeline(
    events: List<JoinProgress>,
    finished: Boolean,
    failed: Boolean,
    phases: List<JoinPhase> = JoinPhase.maintenanceRun,
): List<JoinStep> {
    val known = events.mapNotNull { e -> JoinPhase.of(e.phase)?.takeIf { it in phases }?.let { it to e } }
    val reached = known.maxOfOrNull { phases.indexOf(it.first) } ?: if (failed) 0 else -1
    return phases.mapIndexed { index, phase ->
        val own = known.filter { it.first == phase }.map { it.second }
        val status = when {
            finished && !failed -> StepStatus.DONE
            index < reached -> StepStatus.DONE
            index == reached && failed -> StepStatus.FAILED
            index == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
        JoinStep(phase, status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
    }
}
