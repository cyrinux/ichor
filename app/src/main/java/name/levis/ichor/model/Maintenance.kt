package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/maintenance.go and kube_drain.go.

@Serializable
data class MaintenancePlan(
    val node: String,
    val hostname: String = "",
    val kubeNode: String = "",
    val controlPlane: Boolean = false,
    val cordoned: Boolean = false,
    val pods: List<DrainPod> = emptyList(),
    /** Refuse a reboot or shutdown (a drain alone is always allowed). */
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    /** To confirm before a reboot or shutdown: the core refuses unless acknowledged. */
    val acknowledge: List<String> = emptyList(),
)

@Serializable
data class DrainPod(
    val namespace: String,
    val name: String,
    /** "ReplicaSet/web-5d8f", "" when none. */
    val owner: String = "",
    /** [KIND_EVICT], [KIND_BARE], [KIND_DAEMONSET] or [KIND_STATIC]. */
    val kind: String = KIND_EVICT,
    /** Keeps data in an emptyDir volume, lost when evicted. */
    val emptyDir: Boolean = false,
    /** The PodDisruptionBudget covering the pod, "" when none. */
    val pdb: String = "",
    /** Disruptions [pdb] allows right now; -1 without a budget. */
    val pdbAllowed: Int = -1,
    /** During a run: [STATE_PENDING], [STATE_EVICTING], [STATE_BLOCKED] or [STATE_GONE]. */
    val state: String = "",
    val reason: String = "",
) {
    val key: String get() = "$namespace/$name"

    companion object {
        const val KIND_EVICT = "evict"
        const val KIND_BARE = "bare"
        const val KIND_DAEMONSET = "daemonset"
        const val KIND_STATIC = "static"
        const val STATE_PENDING = "pending"
        const val STATE_EVICTING = "evicting"
        const val STATE_BLOCKED = "blocked"
        const val STATE_GONE = "gone"
    }
}

@Serializable
data class MaintenanceProgress(
    val phase: String,
    val message: String = "",
    val at: Long = 0,
    val pods: List<DrainPod> = emptyList(),
)

/** What follows the drain. */
enum class MaintenanceAction(val wire: String) {
    REBOOT("reboot"),
    SHUTDOWN("shutdown"),
    NONE("none"),
}

/** Run phases, in the order they happen. */
enum class MaintenancePhase(val wire: String) {
    CORDON("cordon"),
    DRAIN("drain"),
    REBOOT("reboot"),
    SHUTDOWN("shutdown"),
    WAITING("waiting"),
    UNCORDON("uncordon"),
    ;

    companion object {
        fun of(wire: String): MaintenancePhase? = entries.firstOrNull { it.wire.equals(wire, ignoreCase = true) }
    }
}

/** The phases a run of [action] goes through. */
fun maintenancePhases(action: MaintenanceAction): List<MaintenancePhase> = when (action) {
    MaintenanceAction.REBOOT -> listOf(
        MaintenancePhase.CORDON, MaintenancePhase.DRAIN, MaintenancePhase.REBOOT, MaintenancePhase.WAITING, MaintenancePhase.UNCORDON,
    )
    MaintenanceAction.SHUTDOWN -> listOf(MaintenancePhase.CORDON, MaintenancePhase.DRAIN, MaintenancePhase.SHUTDOWN)
    MaintenanceAction.NONE -> listOf(MaintenancePhase.CORDON, MaintenancePhase.DRAIN)
}

/** The plan's pods by what the drain does with them. */
data class DrainGroups(val evict: List<DrainPod>, val bare: List<DrainPod>, val leftAlone: List<DrainPod>)

fun MaintenancePlan.drainGroups(): DrainGroups = DrainGroups(
    evict = pods.filter { it.kind == DrainPod.KIND_EVICT },
    bare = pods.filter { it.kind == DrainPod.KIND_BARE },
    // Unknown kinds from a newer core are left alone too: the core decides, the app only shows.
    leftAlone = pods.filter { it.kind != DrainPod.KIND_EVICT && it.kind != DrainPod.KIND_BARE },
)

/** A PodDisruptionBudget that allows no disruption now: the drain waits for it. */
val DrainPod.pdbBlocks: Boolean get() = pdb.isNotEmpty() && pdbAllowed == 0

/**
 * Start needs no other run in the app, and for a reboot or shutdown no blocker and every
 * acknowledgment ticked ([ticked]: how many). A drain alone is always allowed.
 */
fun maintenanceCanStart(plan: MaintenancePlan, action: MaintenanceAction, ticked: Int, otherRunning: Boolean): Boolean = when {
    otherRunning -> false
    action == MaintenanceAction.NONE -> true
    else -> plan.blockers.isEmpty() && ticked >= plan.acknowledge.size
}

/** Whether the run may go on with the plan's acknowledgments: only once they were all ticked. */
fun maintenanceAcknowledged(plan: MaintenancePlan, action: MaintenanceAction, ticked: Int): Boolean =
    action != MaintenanceAction.NONE && plan.acknowledge.isNotEmpty() && ticked >= plan.acknowledge.size

data class MaintenanceStep(val phase: MaintenancePhase, val status: StepStatus, val at: Long = 0, val message: String = "")

/**
 * The phase timeline of [action] from the [events] so far, like [upgradeTimeline]: phases
 * before the latest one are done, the latest is current (failed when [failed]), the rest
 * pending; all done once [finished] without failure.
 */
fun maintenanceTimeline(action: MaintenanceAction, events: List<MaintenanceProgress>, finished: Boolean, failed: Boolean): List<MaintenanceStep> {
    val phases = maintenancePhases(action)
    val known = events.mapNotNull { e -> MaintenancePhase.of(e.phase)?.takeIf { it in phases }?.let { it to e } }
    val reached = known.maxOfOrNull { phases.indexOf(it.first) } ?: if (failed) 0 else -1
    return phases.mapIndexed { i, phase ->
        val own = known.filter { it.first == phase }.map { it.second }
        val status = when {
            finished && !failed -> StepStatus.DONE
            i < reached -> StepStatus.DONE
            i == reached && failed -> StepStatus.FAILED
            i == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
        MaintenanceStep(phase, status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
    }
}

/**
 * Whether the node is cordoned after a run of [action] that reached [phase] (null: not even
 * cordoned) and ended (not [running]) with or without [failed]; null when unknown. Only a
 * successful reboot uncordons it, and not a node [wasCordoned] before the run.
 */
fun cordonedAfter(action: MaintenanceAction, phase: MaintenancePhase?, running: Boolean, failed: Boolean, wasCordoned: Boolean): Boolean? = when {
    phase == null || phase == MaintenancePhase.CORDON -> null // the cordon itself may not have happened
    !running && !failed && action == MaintenanceAction.REBOOT -> wasCordoned
    else -> true
}
