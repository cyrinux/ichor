package name.levis.ichor.ui.operations

import android.content.Context
import androidx.annotation.StringRes
import name.levis.ichor.R
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.FollowedRun

/**
 * A notification's text, read in the app's language only when it is posted ([resolve]): the
 * presenters stay plain functions of a run, testable without Android.
 */
sealed interface OperationText {
    /** A string resource; an argument that is itself an [OperationText] is resolved first. */
    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : OperationText

    /** Text as the core or the cluster gave it. */
    data class Raw(val text: String) : OperationText

    /** [parts] joined by [separator], the empty ones left out. */
    data class Joined(val parts: List<OperationText>, val separator: String) : OperationText
}

fun res(@StringRes id: Int, vararg args: Any): OperationText = OperationText.Res(id, args.toList())

fun raw(text: String): OperationText = OperationText.Raw(text)

fun OperationText.resolve(res: Context): String = when (this) {
    is OperationText.Res -> if (args.isEmpty()) {
        res.getString(id)
    } else {
        res.getString(id, *args.map { if (it is OperationText) it.resolve(res) else it }.toTypedArray())
    }
    is OperationText.Raw -> text
    is OperationText.Joined -> parts.map { it.resolve(res) }.filter { it.isNotEmpty() }.joinToString(separator)
}

/** How far a run is: [done] of [total] steps (nodes, images), or a spinner when [total] is 0. */
data class OperationProgress(val done: Int, val total: Int) {
    companion object {
        val INDETERMINATE = OperationProgress(0, 0)

        /** The bar of a run on step [index] (from 0) of [total]. */
        fun step(index: Int, total: Int) = if (total > 0) OperationProgress(index.coerceIn(0, total), total) else INDETERMINATE
    }
}

/**
 * A button on a running operation's notification. With the app lock on, those that
 * [opensAppWhenLocked] open the run's screen instead (unlocked first), like the screen's own
 * buttons; the others act from the shade once the device is unlocked.
 */
enum class OperationAction(@StringRes val label: Int, val opensAppWhenLocked: Boolean) {
    STOP(R.string.operation_stop, true),
    STOP_FOLLOWING(R.string.upgrade_stop_following, false),
    PAUSE(R.string.cluster_upgrade_pause, false),
    RESUME(R.string.cluster_upgrade_resume, false),
    ABORT(R.string.cluster_upgrade_abort, true),
    KEEP(R.string.machine_config_keep, true),
    REVERT(R.string.machine_config_revert_now, false),
    TERMINATE(R.string.argo_terminate, true),
    ;

    companion object {
        fun named(name: String?): OperationAction? = entries.firstOrNull { it.name == name }
    }
}

/**
 * What a running operation's notification shows: its [progress] bar (null for none), a
 * countdown to [countdownTo] (epoch ms, 0 for none) and its [actions].
 */
data class OperationNotification(
    val title: OperationText,
    val text: OperationText,
    val progress: OperationProgress? = OperationProgress.INDETERMINATE,
    val countdownTo: Long = 0,
    val actions: List<OperationAction> = emptyList(),
)

/** The notification once the run ended: how it went. */
data class OperationResult(val title: OperationText, val text: OperationText)

/** The words of one kind of run, for its notifications. */
interface OperationPresenter<T : FollowedRun> {
    fun progress(run: T): OperationNotification

    /** Title and detail once the run succeeded (or was stopped). */
    fun done(run: T): OperationResult

    /** The title once the run failed; [FollowedRun.error] is the detail. */
    fun failedTitle(run: T): OperationText

    fun result(run: T): OperationResult = run.error?.let { OperationResult(failedTitle(run), raw(it)) } ?: done(run)
}

/**
 * The runs the app follows app-wide, each with its notification ids (those of the service that
 * followed it before; a join had the image pull's), what the lock screen says with the app lock
 * on (never a node's name), and the screen its notification opens (null: the app as it was).
 */
enum class OperationKind(
    val progressId: Int,
    val resultId: Int,
    @StringRes val publicTitle: Int,
    @StringRes val resultPublicTitle: Int,
    val deepLink: DeepLink?,
) {
    UPGRADE(0x0b6e, 0x0b6f, R.string.upgrade_notification_public, R.string.upgrade_notification_result_public, DeepLink.UPGRADE),
    MAINTENANCE(0x0b70, 0x0b71, R.string.maintenance_notification_public, R.string.maintenance_notification_result_public, DeepLink.MAINTENANCE),
    CONFIG_TRY(0x0b74, 0x0b75, R.string.config_try_notification_public, R.string.config_try_notification_result_public, DeepLink.CONFIG_TRY),
    CONFIG_MULTI(0x0b76, 0x0b77, R.string.config_multi_notification_public, R.string.config_multi_notification_result_public, DeepLink.CONFIG_MULTI),
    CLUSTER_UPGRADE(0x0b78, 0x0b79, R.string.cluster_upgrade_notification_public, R.string.cluster_upgrade_notification_result_public, DeepLink.CLUSTER_UPGRADE),
    K8S_UPGRADE(0x0b7a, 0x0b7b, R.string.k8s_upgrade_notification_public, R.string.k8s_upgrade_notification_result_public, DeepLink.K8S_UPGRADE),
    IMAGE_PULL(0x0b80, 0x0b81, R.string.image_pull_notification_public, R.string.image_pull_notification_result_public, null),
    JOIN(0x0b82, 0x0b83, R.string.join_notification_public, R.string.join_notification_result_public, DeepLink.JOIN_NODE),
    IMAGE_SCAN(0x0b90, 0x0b91, R.string.imagescan_notification_public, R.string.imagescan_notification_result_public, null),
    ARGO_SYNC(0x0b92, 0x0b93, R.string.argo_sync_notification_public, R.string.argo_sync_notification_result_public, DeepLink.ARGO_APP),
    FLUX_RECONCILE(0x0b94, 0x0b95, R.string.flux_reconcile_notification_public, R.string.flux_reconcile_notification_result_public, DeepLink.FLUX_APP),
    ROLLOUT(0x0b96, 0x0b97, R.string.rollout_notification_public, R.string.rollout_notification_result_public, DeepLink.WORKLOAD),
    NETPERF(0x0b98, 0x0b99, R.string.netperf_notification_public, R.string.netperf_notification_result_public, DeepLink.NETPERF),
    ;

    companion object {
        fun named(name: String?): OperationKind? = entries.firstOrNull { it.name == name }
    }
}
