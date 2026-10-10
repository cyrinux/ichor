package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/upgradecluster.go (ClusterUpgradePlan, StartClusterUpgrade).

/** A node of the rolling upgrade, in the order it is upgraded; [state] is one of the constants below. */
@Serializable
data class ClusterUpgradeNode(
    val node: String,
    val hostname: String = "",
    /** controlplane | worker */
    val role: String = "",
    /** The Talos version it runs now. */
    val from: String = "",
    val image: String = "",
    val state: String = PENDING,
    /** The etcd leader: upgraded last of the control planes, after it hands the leadership over. */
    val leader: Boolean = false,
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
) {
    val name: String get() = hostname.ifEmpty { node }
    val controlPlane: Boolean get() = role == "controlplane"

    companion object {
        const val PENDING = "pending"
        const val RUNNING = "running"
        const val DONE = "done"
        const val FAILED = "failed"
    }
}

/** What upgrading every node to [version] would do, nodes in their upgrade order. */
@Serializable
data class ClusterUpgradePlan(
    val version: String = "",
    val image: String = "",
    val nodes: List<ClusterUpgradeNode> = emptyList(),
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    /** Whether the upgrade drains nodes by itself (Talos v1.18+); otherwise the switch offers it. */
    val drain: Boolean = false,
) {
    val pending: List<ClusterUpgradeNode> get() = nodes.filter { it.state != ClusterUpgradeNode.DONE }
    val done: Int get() = nodes.count { it.state == ClusterUpgradeNode.DONE }

    /** Some nodes already run [version], others not: a roll was interrupted, starting continues it. */
    val continues: Boolean get() = done > 0 && pending.isNotEmpty()

    /** Nothing left to upgrade. */
    val upToDate: Boolean get() = nodes.isNotEmpty() && pending.isEmpty()

    /** Every blocker, the cluster's then each node's (named). */
    fun allBlockers(): List<String> = blockers + nodes.flatMap { n -> n.blockers.map { "${n.name}: $it" } }

    val canStart: Boolean get() = !upToDate && allBlockers().isEmpty()
}

/** A step of the roll: the node at [index] being upgraded, a gate, a pause, or the end. */
@Serializable
data class ClusterUpgradeProgress(
    val phase: String = "",
    val index: Int = 0,
    val total: Int = 0,
    val node: String = "",
    val hostname: String = "",
    /** The node's own upgrade phase while [phase] is [NODE] (the single-node upgrade's phases). */
    val nodePhase: String = "",
    val message: String = "",
    val at: Long = 0,
    val nodes: List<ClusterUpgradeNode> = emptyList(),
) {
    val name: String get() = hostname.ifEmpty { node }

    companion object {
        const val NODE = "node"
        const val GATE = "gate"
        const val PAUSED = "paused"
        const val DONE = "done"
    }
}

sealed interface ClusterUpgradeEvent {
    data class Progress(val progress: ClusterUpgradeProgress) : ClusterUpgradeEvent

    /** [error] null when every node was upgraded. */
    data class Done(val error: String?) : ClusterUpgradeEvent
}

/** What the run screen can ask the roll; each goes to StartClusterUpgrade's run handle. */
enum class ClusterUpgradeCommand { PAUSE, RESUME, ABORT }
