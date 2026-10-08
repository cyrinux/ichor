package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanProgress
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.RoutePod
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.ImageScanListener
import name.levis.ichorgo.ImageScanRun
import name.levis.ichorgo.Ichorgo

/**
 * A scan of one app's images: [running] until the core reports it done. [reportJson] is the
 * core's report as is, what the exports are written from.
 */
data class ImageScanSession(
    /** The cluster context and app it scans. */
    val context: String,
    val appId: String,
    val progress: ImageScanProgress? = null,
    val report: ImageScanReport? = null,
    val reportJson: String = "",
    val running: Boolean = true,
    /** Stop was asked: the core is deleting the scan Job. */
    val stopping: Boolean = false,
    /** Ended by Stop: its report holds only what was scanned before. */
    val stopped: Boolean = false,
    val error: String? = null,
) {
    /** The finished scan's report, when it scanned at least one image (not stopped or failed early). */
    val usableReport: ImageScanReport?
        get() = report?.takeIf { !running && !stopped && error == null && it.images.any { image -> image.error.isEmpty() } }
}

/**
 * Vulnerability scans of an app's images, through the Kubernetes API (os:admin): a Trivy Job
 * in the cluster, or the Trivy Operator's reports. One scan at a time, kept app-wide so it
 * goes on while the user leaves the Apps screen; the core calls back on its own threads.
 */
class ImageScanRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    private val _session = MutableStateFlow<ImageScanSession?>(null)
    val session: StateFlow<ImageScanSession?> = _session.asStateFlow()

    @Volatile private var run: ImageScanRun? = null

    /** The Trivy Operator's reports on [pods]' images; available false without it. */
    suspend fun operatorReports(pods: List<RoutePod>): OperatorReports = withContext(Dispatchers.IO) {
        val target = target()
        val json = Ichorgo.imageScanOperatorReports(target.yaml, target.context, target.server, podsJson(pods))
        TalosJson.decodeFromString(OperatorReports.serializer(), json)
    }

    /**
     * Scans [pods]' images, replacing the last scan. Ignored while one runs. It returns at once:
     * the core runs the scan in the background and [session] follows it.
     */
    fun start(appId: String, pods: List<RoutePod>) {
        if (_session.value?.running == true) return
        val target = try {
            target()
        } catch (e: Exception) {
            _session.value = ImageScanSession(configs.config.value?.activeContext.orEmpty(), appId, running = false, error = e.message)
            return
        }
        _session.value = ImageScanSession(target.context, appId)
        run = Ichorgo.startImageScan(target.yaml, target.context, target.server, podsJson(pods), "", listener())
    }

    /** Stops the running scan; the session ends once the core deleted its Job. */
    fun stop() {
        val r = run ?: return
        _session.update { it?.copy(stopping = true) }
        r.cancel()
    }

    /** [reportJson] (a scan's or the operator's) written in [format]. */
    suspend fun export(reportJson: String, format: ImageScanFormat): String = withContext(Dispatchers.IO) {
        Ichorgo.imageScanExport(reportJson, format.id)
    }

    fun encode(report: ImageScanReport): String = TalosJson.encodeToString(ImageScanReport.serializer(), report)

    private fun listener() = object : ImageScanListener {
        override fun onProgress(json: String) {
            runCatching { TalosJson.decodeFromString(ImageScanProgress.serializer(), json) }
                .onSuccess { p -> _session.update { it?.copy(progress = p) } }
        }

        override fun onDone(reportJSON: String, errMessage: String) {
            val report = runCatching { TalosJson.decodeFromString(ImageScanReport.serializer(), reportJSON) }.getOrNull()
            _session.update { s ->
                s?.copy(
                    report = report,
                    reportJson = reportJSON,
                    running = false,
                    stopped = s.stopping,
                    // Stopping is not a failure, whatever the core reports for it.
                    error = errMessage.ifEmpty { null }?.takeUnless { s.stopping }?.let(::goErrorText),
                )
            }
            run = null
        }
    }

    private fun podsJson(pods: List<RoutePod>) = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)

    /** The config to call with and the API address, through the cluster's Kubernetes access when set (K5). */
    private fun target(): KubeTarget = kubeServers.targetFor(configs.forCall())
}
