package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/upgrade*.go.

@Serializable
data class UpgradePlan(
    val node: String,
    val hostname: String = "",
    val controlPlane: Boolean = false,
    val currentVersion: String = "",
    val currentImage: String = "",
    val schematic: String = "",
    val etcd: EtcdUpgradeCheck? = null,
    /** Hard stops: the core refuses to start while there are any (unless [forceable] and forced). */
    val blockers: List<String> = emptyList(),
    val warnings: List<String> = emptyList(),
    /** Risks the user must accept to start: the core refuses unless acknowledged (force does not skip them). */
    val acknowledge: List<String> = emptyList(),
    /** Every blocker is an etcd check, which "force" skips like `talosctl upgrade --force`. */
    val forceable: Boolean = false,
)

@Serializable
data class EtcdUpgradeCheck(
    val members: Int = 0,
    val healthy: Int = 0,
    val thisNodeMember: Boolean = false,
    /** Whether etcd keeps quorum while this member is down. */
    val quorumAfterLoss: Boolean = true,
)

@Serializable
data class TalosRelease(val version: String, val date: String = "", val prerelease: Boolean = false)

@Serializable
data class UpgradeProgress(val phase: String, val message: String = "", val at: Long = 0)

/** Upgrade phases, in the order they happen. */
enum class UpgradePhase(val wire: String) {
    REQUESTED("requested"),
    INSTALLING("installing"),
    REBOOTING("rebooting"),
    WAITING("waiting for node"),
    BOOTED("booted"),
    DONE("done"),
    ;

    companion object {
        fun of(wire: String): UpgradePhase? = entries.firstOrNull { it.wire.equals(wire, ignoreCase = true) }
    }
}

/** Force is offered only when blockers exist and all of them are etcd checks. */
val UpgradePlan.etcdBlocked: Boolean get() = blockers.isNotEmpty() && forceable

/** What the upgrade screen allows for the current choices. */
data class UpgradeGate(
    /** The overflow "Force" option is offered (only for etcd blockers). */
    val showForce: Boolean,
    val canStart: Boolean,
)

/**
 * Start needs a target [version] whose [image] could be built, no other upgrade running in the
 * app, and no blocker unless [force] (which only overrides etcd blockers).
 */
fun upgradeGate(plan: UpgradePlan, version: String, image: String, force: Boolean, otherRunning: Boolean): UpgradeGate {
    val showForce = plan.etcdBlocked
    val blocked = plan.blockers.isNotEmpty() && !(force && showForce)
    return UpgradeGate(
        showForce = showForce,
        canStart = version.isNotBlank() && image.isNotBlank() && !blocked && !otherRunning,
    )
}

/** What the user must accept to start: the plan's risks, then the chosen version's [versionRisk] if any. */
fun upgradeRisks(plan: UpgradePlan, versionRisk: String): List<String> =
    plan.acknowledge + listOfNotNull(versionRisk.trim().takeIf { it.isNotEmpty() })

/** The confirmation allows starting only once the user ticked that they [understood] the [risks], if any. */
fun upgradeAcknowledged(risks: List<String>, understood: Boolean): Boolean = risks.isEmpty() || understood

/** Releases suggested in the version picker: stable first, newest first as listed by Go. */
fun releaseSuggestions(releases: List<TalosRelease>, includePrerelease: Boolean = true): List<TalosRelease> =
    releases.filter { includePrerelease || !it.prerelease }.sortedBy { it.prerelease }

enum class StepStatus { DONE, CURRENT, PENDING, FAILED }

data class TimelineStep(val phase: UpgradePhase, val status: StepStatus, val at: Long = 0, val message: String = "")

/**
 * The phase timeline from the [events] received so far: phases before the latest one are
 * done, the latest is current (failed when [failed]), the rest pending; all done once
 * [finished] without failure.
 * Unknown phases are ignored; each step keeps its first time and its last message.
 */
fun upgradeTimeline(events: List<UpgradeProgress>, finished: Boolean, failed: Boolean): List<TimelineStep> {
    val known = events.mapNotNull { e -> UpgradePhase.of(e.phase)?.let { it to e } }
    val reached = known.maxOfOrNull { it.first.ordinal } ?: -1
    return UpgradePhase.entries.map { phase ->
        val own = known.filter { it.first == phase }.map { it.second }
        val status = when {
            finished && !failed -> StepStatus.DONE // the node runs the new version: every step happened
            phase.ordinal < reached -> StepStatus.DONE
            phase.ordinal == reached && failed -> StepStatus.FAILED
            phase.ordinal == reached && phase == UpgradePhase.DONE -> StepStatus.DONE
            phase.ordinal == reached -> StepStatus.CURRENT
            else -> StepStatus.PENDING
        }
        TimelineStep(phase, status, own.firstOrNull()?.at ?: 0, own.lastOrNull()?.message.orEmpty())
    }
}
