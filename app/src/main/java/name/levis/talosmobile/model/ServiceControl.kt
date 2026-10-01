package name.levis.talosmobile.model

import androidx.annotation.StringRes
import name.levis.talosmobile.R

/** `talosctl service ID start|stop|restart`. */
enum class ServiceAction(
    val cli: String,
    @StringRes val label: Int,
    @StringRes val confirmTitle: Int,
    @StringRes val done: Int,
) {
    RESTART("restart", R.string.service_action_restart, R.string.service_confirm_restart, R.string.service_done_restart),
    STOP("stop", R.string.service_action_stop, R.string.service_confirm_stop, R.string.service_done_stop),
    START("start", R.string.service_action_start, R.string.service_confirm_start, R.string.service_done_start),
}

/** Services whose restart or stop may cut the app's connection or disturb the node. */
val CRITICAL_SERVICES = setOf("apid", "trustd", "etcd", "kubelet", "machined", "containerd", "cri")

fun isCriticalService(id: String): Boolean = id in CRITICAL_SERVICES

/**
 * Actions that make sense for the service's state: restart/stop when running, start when
 * stopped (finished, failed, skipped or not started yet), nothing while it is changing state.
 */
fun ServiceInfo.offeredActions(): List<ServiceAction> = when (state.lowercase()) {
    "running" -> listOf(ServiceAction.RESTART, ServiceAction.STOP)
    "finished", "failed", "skipped", "initialized" -> listOf(ServiceAction.START)
    else -> emptyList()
}
