package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ConfigTryRunState
import name.levis.ichor.model.ConfigTryState
import java.text.DateFormat
import java.util.Date

/**
 * A machine config tried on a node: a countdown to the node's own revert (no bar), and Keep or
 * Revert now while it waits. [time] writes the revert's time of day.
 */
class ConfigTryPresenter(
    private val time: (Long) -> String = { DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(it)) },
) : OperationPresenter<ConfigTryRunState> {
    override fun progress(run: ConfigTryRunState): OperationNotification {
        val running = run.state as? ConfigTryState.Running
        val step = when (running?.phase) {
            ConfigTryState.TRYING -> if (run.deadline > 0) {
                res(R.string.config_try_notification_reverts_at, time(run.deadline))
            } else {
                res(R.string.machine_config_try_countdown)
            }
            ConfigTryState.KEEPING -> res(R.string.machine_config_try_keeping)
            ConfigTryState.REVERTING -> res(R.string.machine_config_try_reverting)
            else -> res(R.string.machine_config_try_applying)
        }
        return OperationNotification(
            title = res(R.string.config_try_notification_title, run.hostname),
            // Why the last keep or revert failed, while the try goes on.
            text = OperationText.Joined(listOf(step, raw(running?.message.orEmpty().trim())), "\n"),
            progress = null,
            countdownTo = run.deadline,
            actions = if (run.trying) listOf(OperationAction.KEEP, OperationAction.REVERT) else emptyList(),
        )
    }

    override fun done(run: ConfigTryRunState) = if (run.state == ConfigTryState.Kept) {
        OperationResult(res(R.string.config_try_notification_kept, run.hostname), res(R.string.machine_config_kept))
    } else {
        OperationResult(res(R.string.config_try_notification_reverted, run.hostname), res(R.string.machine_config_reverted))
    }

    override fun failedTitle(run: ConfigTryRunState) = res(R.string.config_try_notification_failed, run.hostname)
}
