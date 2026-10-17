package name.levis.ichor.ui.imagescan

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ObjectScanMode
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.SavedScan
import name.levis.ichor.model.scanId
import name.levis.ichor.model.scanMode
import name.levis.ichor.model.scanPods
import name.levis.ichor.model.scanWorkload
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState

/** What an object's scan reads first: a workload's image refs, the Trivy Operator's reports on a pod. */
data class ObjectScanTarget(
    val images: List<String> = emptyList(),
    val operator: OperatorReports = OperatorReports(),
)

/**
 * The vulnerability scan of one Kubernetes object's images, from its object screen: a pod's
 * through the pod (by digest), with the Trivy Operator's reports on it, a workload's by the
 * image refs of its spec, both read as the screen opens ([state]). The scan is kept by
 * [ImageScanRepository], under [id], so it goes on when the screen closes and its saved report
 * reopens here.
 */
class ObjectImageScanViewModel(
    private val scans: ImageScanRepository,
    private val kube: KubeRepository,
    private val ref: KubeObjectRef,
) : LoadingViewModel<ObjectScanTarget>() {
    val mode: ObjectScanMode? = ref.scanMode

    /** What the core keeps the report under. */
    val id: String = ref.scanId

    val session = scans.session

    private val _saved = MutableStateFlow<List<SavedScan>>(emptyList())

    /** The cluster's kept reports, newest first; empty until [loadSaved]. */
    val saved: StateFlow<List<SavedScan>> = _saved.asStateFlow()

    private var loaded = false
    private var cluster: String? = null

    /** Reads what to scan for [cluster] unless it is already read (or being read) for it. */
    fun load(cluster: String?) {
        if (loaded && cluster == this.cluster && state.value !is UiState.Failed) return
        loaded = true
        this.cluster = cluster
        refresh(reset = true)
    }

    override suspend fun fetch(): ObjectScanTarget = when (mode) {
        ObjectScanMode.WORKLOAD -> ObjectScanTarget(images = kube.workloadsNamed(listOf(ref.scanWorkload)).flatMap { it.images }.distinct())
        ObjectScanMode.POD -> ObjectScanTarget(operator = scans.operatorReports(ref.scanPods))
        null -> ObjectScanTarget()
    }

    /** Reads the reports kept for [cluster]: a file on the phone, no cluster call. */
    fun loadSaved(cluster: String) {
        viewModelScope.launch {
            _saved.value = runCatching { scans.history(cluster) }.getOrDefault(emptyList())
        }
    }

    /** The kept report [id] with the core's JSON, to open and export. */
    suspend fun openSaved(id: String): Pair<ImageScanReport, String> = scans.saved(id)

    /** Scans the pod, or the workload's image refs once read; nothing before. */
    fun scan() {
        when (mode) {
            ObjectScanMode.POD -> scans.start(id, ref.name, ref.scanPods)
            ObjectScanMode.WORKLOAD -> {
                val images = (state.value as? UiState.Loaded)?.data?.images.orEmpty()
                if (images.isNotEmpty()) scans.start(id, ref.name, emptyList(), images)
            }
            null -> Unit
        }
    }

    fun stop() = scans.stop()

    /** [report] in [format]; [json] the core's own report when at hand (a scan's). */
    suspend fun export(report: ImageScanReport, json: String, format: ImageScanFormat): String =
        scans.export(json.ifEmpty { scans.encode(report) }, format)
}
