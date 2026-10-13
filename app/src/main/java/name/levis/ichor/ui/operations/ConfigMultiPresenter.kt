package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ConfigMultiRunState
import name.levis.ichor.model.MultiConfigNodeState

/** A config change applied to several nodes, one after the other: a bar per node, and Stop. */
object ConfigMultiPresenter : OperationPresenter<ConfigMultiRunState> {
    override fun progress(run: ConfigMultiRunState): OperationNotification {
        val progress = run.run.progress
        val title = res(R.string.config_multi_notification_title, run.hostname)
        val actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP)
        if (progress == null || progress.total == 0) {
            return OperationNotification(title, res(R.string.machine_config_apply_applying), actions = actions)
        }
        val node = progress.nodes.firstOrNull { it.state == MultiConfigNodeState.APPLYING } ?: progress.nodes.getOrNull(progress.index)
        val step = res(R.string.machine_config_multi_progress, (progress.index + 1).coerceAtMost(progress.total), progress.total)
        val detail = OperationText.Joined(listOf(raw(node?.name.orEmpty()), raw(progress.message.trim())), ": ")
        return OperationNotification(
            title = title,
            text = OperationText.Joined(listOf(step, detail), " · "),
            progress = OperationProgress.step(progress.index, progress.total),
            actions = actions,
        )
    }

    override fun done(run: ConfigMultiRunState): OperationResult {
        val done = run.run.progress?.nodes.orEmpty().filter { it.state == MultiConfigNodeState.DONE }.map { it.name }
        val title = if (run.stopping) R.string.config_multi_notification_stopped else R.string.config_multi_notification_done
        return OperationResult(res(title, run.hostname), raw(done.joinToString(", ")))
    }

    override fun failedTitle(run: ConfigMultiRunState) = res(R.string.config_multi_notification_failed, run.hostname)
}
