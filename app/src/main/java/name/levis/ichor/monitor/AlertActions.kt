package name.levis.ichor.monitor

import name.levis.ichor.model.ShareTarget

/**
 * The buttons of an alert's notification. [inApp] ones change the cluster: they never run from
 * the lock screen or in the background, they open the alert's screen on its cluster (like its
 * share link), once unlocked, on that screen's own confirmation. Wake and Snooze run in the
 * background.
 */
enum class AlertAction(val inApp: Boolean) {
    WAKE(false),
    REBOOT(true),
    SYNC(true),
    RECONCILE(true),
    SILENCE(true),
    SNOOZE(false),
}

/** How long Snooze quiets an alert (Android has room for one duration only). */
const val SNOOZE_MILLIS = 8 * 3_600_000L

/** How long Silence 1 h prefills the Alertmanager silence for. */
const val SILENCE_ACTION_MINUTES = 60L

/**
 * The buttons [this] alert gets, at most three (Android's limit): none for a "resolved" or "ready
 * again" one. [canWake]: a Wake-on-LAN target is known for the node; [canReboot]: the cluster's
 * role may reboot it (a Talos cluster).
 */
fun Alert.actions(canWake: Boolean, canReboot: Boolean): List<AlertAction> {
    if (!problem) return emptyList()
    val own = when (kind) {
        AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE -> listOfNotNull(
            AlertAction.WAKE.takeIf { canWake },
            AlertAction.REBOOT.takeIf { canReboot },
        )
        AlertKind.GITOPS_PROBLEM ->
            listOf(if (GitOpsDetail.parse(detail).tool == GITOPS_FLUX) AlertAction.RECONCILE else AlertAction.SYNC)
        AlertKind.AM_FIRING -> listOf(AlertAction.SILENCE)
        else -> emptyList()
    }
    return own + AlertAction.SNOOZE
}

/**
 * What an [AlertAction.inApp] button asks the app for: [action] on [target], the node's address
 * (reboot), "namespace/name" (Argo CD app), "Kind namespace/name" (Flux object) or the alert's
 * fingerprint (Alertmanager). Comes with the alert's share link, which opens the screen.
 */
data class AlertActionRequest(val action: AlertAction, val target: String) {
    /**
     * Whether it is about the screen [link] opens: a request that does not name the same node,
     * app or Alertmanager screen is ignored, the screen opens without a dialog.
     */
    fun matches(link: ShareTarget): Boolean = target.isNotEmpty() && when (action) {
        AlertAction.REBOOT -> link.target == ShareTarget.NODE && link.addr == target
        AlertAction.SYNC -> link.target == ShareTarget.ARGO_APP && "${link.namespace}/${link.name}" == target
        AlertAction.RECONCILE -> link.target == ShareTarget.FLUX_APP && "${link.kind} ${link.namespace}/${link.name}" == target
        AlertAction.SILENCE -> link.target == ShareTarget.ALERTS
        AlertAction.WAKE, AlertAction.SNOOZE -> false
    }

    companion object {
        /** An action name and target as an intent carries them; null for anything else. */
        fun parse(action: String?, target: String?): AlertActionRequest? {
            val known = AlertAction.entries.firstOrNull { it.name == action && it.inApp } ?: return null
            return target?.takeIf { it.isNotEmpty() }?.let { AlertActionRequest(known, it) }
        }
    }
}

/** The [AlertActionRequest] of an in-app [action] on [this] alert: what it names (see there). */
fun Alert.actionRequest(action: AlertAction): AlertActionRequest? {
    val target = when (action) {
        // The node's address, the Alertmanager alert's fingerprint.
        AlertAction.REBOOT, AlertAction.SILENCE -> key.substringAfter(':')
        AlertAction.SYNC, AlertAction.RECONCILE -> subject
        AlertAction.WAKE, AlertAction.SNOOZE -> return null
    }
    return AlertActionRequest(action, target)
}
