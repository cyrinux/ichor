package name.levis.ichor.ui.imagescan

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.SavedScan
import name.levis.ichor.model.routePods
import name.levis.ichor.ui.LoadingViewModel

/**
 * The vulnerability reports of the app of the open detail sheet: the Trivy Operator's, read
 * when the sheet opens (nothing runs in the cluster for it), a scan started from there
 * (kept by [ImageScanRepository], so it goes on when the sheet closes), and the reports the
 * core keeps on the phone ([saved]).
 */
class ImageScanViewModel(private val scans: ImageScanRepository) : LoadingViewModel<OperatorReports>() {
    private var app: InventoryApp? = null

    val session = scans.session

    private val _saved = MutableStateFlow<List<SavedScan>>(emptyList())

    /** The cluster's kept reports, newest first; empty until [loadSaved]. */
    val saved: StateFlow<List<SavedScan>> = _saved.asStateFlow()

    override suspend fun fetch(): OperatorReports {
        val pods = app?.routePods.orEmpty()
        if (pods.isEmpty()) return OperatorReports()
        return scans.operatorReports(pods)
    }

    /** Reads the operator's reports on [app] unless they are already read for it. */
    fun load(app: InventoryApp) {
        if (app == this.app) return
        this.app = app
        refresh(reset = true)
    }

    /** Reads the reports kept for [cluster]: a file on the phone, no cluster call. */
    fun loadSaved(cluster: String) {
        viewModelScope.launch {
            _saved.value = runCatching { scans.history(cluster) }.getOrDefault(emptyList())
        }
    }

    /** The kept report [id] with the core's JSON, to open and export. */
    suspend fun openSaved(id: String): Pair<ImageScanReport, String> = scans.saved(id)

    fun scan(app: InventoryApp) = scans.start(app.id, app.name, app.routePods)

    fun stop() = scans.stop()

    /** [report] in [format]; [json] the core's own report when at hand (a scan's). */
    suspend fun export(report: ImageScanReport, json: String, format: ImageScanFormat): String =
        scans.export(json.ifEmpty { scans.encode(report) }, format)
}
