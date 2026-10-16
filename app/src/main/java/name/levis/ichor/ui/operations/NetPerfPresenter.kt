package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.NetPerfRunState
import name.levis.ichor.model.NETPERF_LATENCY
import name.levis.ichor.model.NETPERF_PATH_HOST
import name.levis.ichor.model.NETPERF_PHASE_CLEANING
import name.levis.ichor.model.NETPERF_PHASE_STARTING
import name.levis.ichor.model.NETPERF_PHASE_TESTING

/** A network test: its phase, a bar of the seconds measured out of the total, and Stop. */
object NetPerfPresenter : OperationPresenter<NetPerfRunState> {
    override fun progress(run: NetPerfRunState): OperationNotification {
        val setup = run.setup
        val total = setup.seconds * setup.steps
        // Each measurement runs for the setup's seconds: those done are the time elapsed.
        val elapsed = (run.results.size * setup.seconds).coerceAtMost(total)
        return OperationNotification(
            title = res(R.string.netperf_notification_title, setup.client, setup.server),
            text = OperationText.Joined(listOf(if (run.stopping) res(R.string.netperf_stopping) else phase(run), res(R.string.netperf_notification_seconds, elapsed, total)), " · "),
            progress = OperationProgress.step(elapsed, total),
            actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
        )
    }

    private fun phase(run: NetPerfRunState): OperationText {
        val p = run.progress ?: return res(R.string.netperf_phase_preparing)
        return when (p.phase) {
            NETPERF_PHASE_STARTING -> if (p.message.contains(':')) {
                res(R.string.netperf_phase_waiting, p.message)
            } else {
                res(R.string.netperf_phase_starting, run.setup.server)
            }
            NETPERF_PHASE_TESTING -> res(
                R.string.netperf_phase_testing,
                res(if (p.path == NETPERF_PATH_HOST) R.string.netperf_path_host else R.string.netperf_path_pod),
                res(if (p.test == NETPERF_LATENCY) R.string.netperf_test_latency else R.string.netperf_test_throughput),
                p.step,
                p.steps,
            )
            NETPERF_PHASE_CLEANING -> res(R.string.netperf_phase_cleaning)
            else -> res(R.string.netperf_phase_preparing)
        }
    }

    override fun done(run: NetPerfRunState) = OperationResult(
        res(if (run.stopped) R.string.netperf_notification_stopped else R.string.netperf_notification_done),
        res(R.string.netperf_summary, run.setup.client, run.setup.server, run.setup.seconds),
    )

    override fun failedTitle(run: NetPerfRunState) = res(R.string.netperf_notification_failed)
}
