package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.ImagePullNamespace
import name.levis.ichor.model.ImagePullProgress
import name.levis.ichor.ui.FollowedRun

/** The image pull the app follows: at most one at a time. */
data class ImagePullRunState(
    val image: String,
    val namespace: ImagePullNamespace,
    val progress: ImagePullProgress = ImagePullProgress(),
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Stopped by the user: the pulls in flight were cancelled. */
    val stopping: Boolean = false,
) : FollowedRun {
    /** The notification names the image: the run spans several nodes. */
    override val hostname: String get() = image
}

/**
 * Pulls an image on several nodes through the Go core, app-wide like [MaintenanceManager]:
 * leaving the screen keeps it going. [onStarted] runs once a pull is followed (to keep the
 * app alive meanwhile).
 */
class ImagePullManager(
    private val talos: TalosRepository,
    private val onStarted: () -> Unit = {},
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val _current = MutableStateFlow<ImagePullRunState?>(null)
    val current: StateFlow<ImagePullRunState?> = _current.asStateFlow()
    private var job: Job? = null

    /**
     * Starts pulling [image] on [nodes] (every node of the cluster when empty) unless a pull is
     * already followed; returns false then.
     */
    @Synchronized
    fun start(image: String, namespace: ImagePullNamespace, nodes: List<String> = emptyList()): Boolean {
        if (_current.value?.running == true) return false
        val trimmed = image.trim()
        _current.value = ImagePullRunState(trimmed, namespace)
        job = scope.launch {
            talos.imagePull(nodes, trimmed, namespace).collect { event ->
                when (event) {
                    is ImagePullEvent.Progress -> _current.update { it?.copy(progress = event.progress) }
                    is ImagePullEvent.Done -> _current.update { it?.copy(finished = true, error = event.error) }
                }
            }
        }
        onStarted()
        return true
    }

    /** Cancels the pulls in flight: the run ends as stopped, the nodes not reached stay pending. */
    @Synchronized
    fun stop() {
        if (_current.value?.running != true) return
        job?.cancel()
        _current.update { it?.copy(finished = true, stopping = true) }
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
