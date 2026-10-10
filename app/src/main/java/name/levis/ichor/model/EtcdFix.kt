package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/etcdfix.go (StartEtcdNospaceFix).

const val ETCD_ALARM_NOSPACE = "NOSPACE"

/** The NOSPACE alarm is active: the one-tap fix is offered. */
val EtcdOverview.hasNospace: Boolean get() = alarms.any { it.alarm == ETCD_ALARM_NOSPACE }

@Serializable
data class EtcdFixMember(
    val node: String = "",
    val hostname: String = "",
    /** [STATE_PENDING], [STATE_RUNNING], [STATE_DONE] or [STATE_FAILED]. */
    val state: String = STATE_PENDING,
    /** What its database shrank by, known once etcd was read again. */
    val reclaimedBytes: Long = 0,
) {
    companion object {
        const val STATE_PENDING = "pending"
        const val STATE_RUNNING = "running"
        const val STATE_DONE = "done"
        const val STATE_FAILED = "failed"
    }
}

@Serializable
data class EtcdFixProgress(
    val phase: String = "",
    val message: String = "",
    val at: Long = 0,
    /** 1-based. */
    val step: Int = 0,
    val steps: Int = 0,
    val members: List<EtcdFixMember> = emptyList(),
)

/** The fix's steps, in order. */
enum class EtcdFixPhase(val wire: String) {
    SNAPSHOT("snapshot"),
    DEFRAG("defrag"),
    DISARM("disarm"),
    RECHECK("recheck"),
}

/**
 * Each step's status after [events]: those before the latest are done, the latest is current
 * (failed when [failed]); all done once [finished] without failure.
 */
fun etcdFixTimeline(events: List<EtcdFixProgress>, finished: Boolean, failed: Boolean): List<Pair<EtcdFixPhase, StepStatus>> {
    val reached = events.mapNotNull { e -> EtcdFixPhase.entries.firstOrNull { it.wire == e.phase } }.maxOfOrNull { it.ordinal }
        ?: if (failed) 0 else -1
    return EtcdFixPhase.entries.map { phase ->
        phase to when {
            finished && !failed -> StepStatus.DONE
            phase.ordinal < reached -> StepStatus.DONE
            phase.ordinal == reached && failed -> StepStatus.FAILED
            phase.ordinal == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
    }
}
