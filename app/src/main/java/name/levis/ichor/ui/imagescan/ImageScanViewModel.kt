package name.levis.ichor.ui.imagescan

import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.routePods
import name.levis.ichor.ui.LoadingViewModel

/**
 * The vulnerability reports of the app of the open detail sheet: the Trivy Operator's, read
 * when the sheet opens (nothing runs in the cluster for it), and a scan started from there
 * (kept by [ImageScanRepository], so it goes on when the sheet closes).
 */
class ImageScanViewModel(private val scans: ImageScanRepository) : LoadingViewModel<OperatorReports>() {
    private var app: InventoryApp? = null

    val session = scans.session

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

    fun scan(app: InventoryApp) = scans.start(app.id, app.routePods)

    fun stop() = scans.stop()

    /** [report] in [format]; [json] the core's own report when at hand (a scan's). */
    suspend fun export(report: ImageScanReport, json: String, format: ImageScanFormat): String =
        scans.export(json.ifEmpty { scans.encode(report) }, format)
}
