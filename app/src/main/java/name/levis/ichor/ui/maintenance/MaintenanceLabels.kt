package name.levis.ichor.ui.maintenance

import androidx.annotation.StringRes
import name.levis.ichor.R
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.model.DrainPod
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenancePhase

@get:StringRes
val MaintenanceAction.label: Int
    get() = when (this) {
        MaintenanceAction.REBOOT -> R.string.power_reboot
        MaintenanceAction.SHUTDOWN -> R.string.power_shut_down
        MaintenanceAction.NONE -> R.string.maintenance_action_none
        MaintenanceAction.UPGRADE -> R.string.common_feature_upgrade
    }

@get:StringRes
val MaintenanceAction.description: Int
    get() = when (this) {
        MaintenanceAction.REBOOT -> R.string.maintenance_action_reboot_desc
        MaintenanceAction.SHUTDOWN -> R.string.maintenance_action_shutdown_desc
        MaintenanceAction.NONE -> R.string.maintenance_action_none_desc
        MaintenanceAction.UPGRADE -> R.string.maintenance_action_upgrade_desc
    }

/** The result of a successful run, with the hostname. */
@get:StringRes
val MaintenanceRunState.doneText: Int
    get() = when (action) {
        MaintenanceAction.REBOOT, MaintenanceAction.UPGRADE -> if (wasCordoned) R.string.maintenance_done_reboot_cordoned else R.string.maintenance_done_reboot
        MaintenanceAction.SHUTDOWN -> R.string.maintenance_done_shutdown
        MaintenanceAction.NONE -> R.string.maintenance_done_none
    }

/** Label of the step in the run timeline. */
@get:StringRes
val MaintenancePhase.label: Int
    get() = when (this) {
        MaintenancePhase.CORDON -> R.string.maintenance_phase_cordon
        MaintenancePhase.DRAIN -> R.string.maintenance_phase_drain
        MaintenancePhase.REBOOT -> R.string.maintenance_phase_reboot
        MaintenancePhase.SHUTDOWN -> R.string.maintenance_phase_shutdown
        MaintenancePhase.UPGRADE -> R.string.maintenance_phase_upgrade
        MaintenancePhase.WAITING -> R.string.maintenance_phase_waiting
        MaintenancePhase.UNCORDON -> R.string.maintenance_phase_uncordon
    }

/** The pod's run state, for TalkBack; null outside a run. */
@get:StringRes
val DrainPod.stateLabel: Int?
    get() = when (state) {
        DrainPod.STATE_PENDING -> R.string.maintenance_pod_pending
        DrainPod.STATE_EVICTING -> R.string.maintenance_pod_evicting
        DrainPod.STATE_BLOCKED -> R.string.maintenance_pod_blocked
        DrainPod.STATE_GONE -> R.string.maintenance_pod_gone
        else -> null
    }
