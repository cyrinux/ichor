package name.levis.ichor.model

/** Where a node stands in the rolling upgrade to the latest release. */
enum class RolloutState { PENDING, UPGRADING, WAITING_HEALTHY, DONE, FAILED }

data class RolloutRow(val node: NodeOverview, val state: RolloutState) {
    val done: Boolean get() = state == RolloutState.DONE
}

/** The upgrade the app follows, as the rollout needs it ([waiting]: the node rebooted and is coming back). */
data class RolloutRun(val node: String, val waiting: Boolean = false, val finished: Boolean = false, val failed: Boolean = false)

/** etcd members answering without errors, out of all of them. */
data class EtcdHealth(val healthy: Int, val members: Int) {
    val degraded: Boolean get() = healthy < members
}

/** Why no other node may be upgraded now. */
sealed interface RolloutHold {
    /** One at a time: [node] is being upgraded, or ([waiting]) is coming back from it. */
    data class Upgrading(val node: NodeOverview, val waiting: Boolean) : RolloutHold

    /** [nodes] do not answer or are not ready. */
    data class Unhealthy(val nodes: List<NodeOverview>) : RolloutHold

    data class Etcd(val health: EtcdHealth) : RolloutHold
}

/**
 * The rolling upgrade to the latest release as a plan: every node under its role in upgrade
 * order (control plane first, done ones last), what holds it up, and the node to do next.
 */
data class Rollout(
    val controlPlane: List<RolloutRow>,
    val workers: List<RolloutRow>,
    val hold: RolloutHold?,
    /** Shown next to an upgrade in progress; null when etcd could not be read. */
    val etcd: EtcdHealth?,
    /** The node whose upgrade the app follows (its row opens the progress). */
    val followed: String?,
) {
    val controlPlaneDone: Int get() = controlPlane.count { it.done }
    val workersDone: Int get() = workers.count { it.done }

    /** Workers are better left for later: the control plane is not fully upgraded. */
    val workersWait: Boolean get() = controlPlane.any { !it.done }

    /** What "Upgrade next" picks: control plane first; in a role, a failed node to retry, else the first pending. */
    val next: RolloutRow?
        get() = listOf(controlPlane, workers).firstNotNullOfOrNull { rows ->
            rows.firstOrNull { it.state == RolloutState.FAILED } ?: rows.firstOrNull { it.state == RolloutState.PENDING }
        }

    /**
     * Whether [row] opens its upgrade screen: the followed node always (its progress); a pending
     * or failed one only when nothing else holds the rollout up. A node that is itself the only
     * unhealthy one stays open, since upgrading it takes no second node down.
     */
    fun canOpen(row: RolloutRow): Boolean = when (row.state) {
        RolloutState.DONE -> false
        RolloutState.UPGRADING, RolloutState.WAITING_HEALTHY -> row.node.node == followed
        RolloutState.PENDING, RolloutState.FAILED -> row.node.reachable && when (val h = hold) {
            null -> true
            is RolloutHold.Unhealthy -> h.nodes.all { it.node == row.node.node }
            else -> false
        }
    }

    /** Upgrading [row] goes against the order (a worker before the control plane is done): to confirm first. */
    fun needsConfirm(row: RolloutRow): Boolean =
        workersWait && row in workers && (row.state == RolloutState.PENDING || row.state == RolloutState.FAILED)
}

private val NodeOverview.healthy: Boolean get() = reachable && ready

/**
 * Sorts like [isOlderVersion] (a pre-release before its release), unknown versions last: a
 * total order, which the rollout needs since it also lists nodes that never said their version.
 */
private fun versionRank(version: String): Long {
    val parts = versionParts(version)
    if (parts.isEmpty()) return Long.MAX_VALUE
    val numbers = parts.fold(0L) { rank, part -> rank * VERSION_PART_SPAN + part.coerceAtMost(VERSION_PART_SPAN - 1) }
    return numbers * 2 + if ('-' in version) 0 else 1
}

private const val VERSION_PART_SPAN = 100_000

private val upgradeOrder = compareBy<NodeOverview>({ versionRank(it.version) }, { it.hostname })

/**
 * The rollout of [nodes] to [latest]. [run] is the upgrade the app follows, if any; [etcd] the
 * members' health when it could be read. A node on [latest] that is not healthy yet still waits.
 */
fun rollout(nodes: List<NodeOverview>, latest: String, run: RolloutRun? = null, etcd: EtcdHealth? = null): Rollout {
    fun state(node: NodeOverview): RolloutState {
        val own = run?.takeIf { it.node == node.node }
        val upToDate = versionParts(node.version).isNotEmpty() && !isOlderVersion(node.version, latest)
        return when {
            own != null && !own.finished -> if (own.waiting) RolloutState.WAITING_HEALTHY else RolloutState.UPGRADING
            upToDate -> if (node.healthy) RolloutState.DONE else RolloutState.WAITING_HEALTHY
            own?.failed == true -> RolloutState.FAILED
            // Upgraded, but the overview still shows what the node ran before.
            own != null -> RolloutState.WAITING_HEALTHY
            else -> RolloutState.PENDING
        }
    }
    val rows = nodes.sortedWith(upgradeOrder).map { RolloutRow(it, state(it)) }.sortedBy { it.done }
    val (controlPlane, workers) = rows.partition { it.node.role == "controlplane" }
    // The followed node until it is done, or failed: also while the overview lags behind the run.
    val active = rows.firstOrNull {
        it.node.node == run?.node && (it.state == RolloutState.UPGRADING || it.state == RolloutState.WAITING_HEALTHY)
    }
    val unhealthy = rows.filter { !it.node.healthy }.map { it.node }
    val hold = when {
        active != null -> RolloutHold.Upgrading(active.node, waiting = active.state == RolloutState.WAITING_HEALTHY)
        unhealthy.isNotEmpty() -> RolloutHold.Unhealthy(unhealthy)
        etcd != null && etcd.degraded -> RolloutHold.Etcd(etcd)
        else -> null
    }
    return Rollout(controlPlane, workers, hold, etcd, followed = run?.node?.takeIf { id -> rows.any { it.node.node == id } })
}

/** The node rebooted: the followed upgrade now waits for it to come back. */
fun upgradeWaitsForNode(events: List<UpgradeProgress>): Boolean =
    events.any { (UpgradePhase.of(it.phase)?.ordinal ?: -1) >= UpgradePhase.WAITING.ordinal }

/** The members answering without errors; null when etcd could not be read. */
fun EtcdOverview.health(): EtcdHealth? {
    if (error != null || members.isEmpty()) return null
    val answering = statuses.filter { it.error == null && it.errors.isEmpty() }.map { it.memberId }.toSet()
    return EtcdHealth(healthy = members.count { it.id in answering }, members = members.size)
}
