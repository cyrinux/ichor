package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.ui.FollowedRun

/** The rollout the app follows after a restart: at most one at a time. */
data class RolloutRunState(
    val workload: KubeWorkload,
    /** As last read; null before the first read. */
    val status: KubeRolloutStatus? = null,
    override val finished: Boolean = false,
    override val error: String? = null,
    /** The user stopped following it: the rollout goes on in the cluster. */
    val stopped: Boolean = false,
) : FollowedRun {
    override val hostname: String get() = workload.name

    /** The pods the workload wants. */
    val total: Int get() = (status?.workload ?: workload).desired

    /** New pods ready, out of [total]: all of them once the rollout is done. */
    val ready: Int
        get() {
            val s = status ?: return 0
            return if (s.done) total else s.pods.count { it.updated && it.healthy }.coerceAtMost(total)
        }

    /** The rollout ended on its own: done, stalled, or nothing to replace (OnDelete). */
    val ended: Boolean get() = status?.let { it.done || it.failed || it.manual } == true
}

/** The live rollout of a workload ([KubeRepository.rolloutWatch]); a fake flow in the tests. */
typealias RolloutWatcher = (KubeWorkload) -> Flow<StreamItem<KubeRolloutStatus>>

/**
 * A rollout restart's rollout, followed app-wide like [ImagePullManager] until every pod runs
 * the new template and is ready (or the rollout stalls): leaving the screen keeps it followed.
 * [onStarted] runs once one is followed (to keep the app alive meanwhile).
 */
class RolloutManager(
    private val watch: RolloutWatcher,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<RolloutRunState?>(null)
    val current: StateFlow<RolloutRunState?> = _current.asStateFlow()
    private var job: Job? = null

    /** Follows [workload]'s rollout, just restarted, unless one is followed; returns false then. */
    @Synchronized
    fun start(workload: KubeWorkload): Boolean {
        if (_current.value?.running == true) return false
        _current.value = RolloutRunState(workload)
        job = scope.launch { follow(workload) }
        onStarted()
        return true
    }

    private suspend fun follow(workload: KubeWorkload) {
        val failure = try {
            // Until the rollout ends or the watch does; firstOrNull stops the watch then.
            watch(workload).firstOrNull { apply(workload, it) }
            null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            e.message ?: e.toString()
        }
        // A watch that ended without an error leaves the rollout where it was.
        _current.update { if (it?.workload?.key == workload.key && it.running) it.copy(finished = true, error = failure ?: it.error) else it }
    }

    /** Takes one item of the watch; true once the run is over. */
    private fun apply(workload: KubeWorkload, item: StreamItem<KubeRolloutStatus>): Boolean = when (item) {
        is StreamItem.Item -> {
            _current.update { if (it?.workload?.key == workload.key && it.running) it.copy(status = item.value) else it }
            _current.value?.ended == true
        }
        is StreamItem.Done -> {
            _current.update { if (it?.workload?.key == workload.key && it.running) it.copy(error = item.error) else it }
            true
        }
    }

    /** Stops following the rollout: it goes on in the cluster. */
    @Synchronized
    fun stop() {
        if (_current.value?.running != true) return
        _current.update { it?.copy(finished = true, stopped = true) }
        job?.cancel()
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            job = null
            _current.value = null
        }
    }
}
