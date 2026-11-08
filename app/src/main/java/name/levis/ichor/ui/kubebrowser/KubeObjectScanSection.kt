package name.levis.ichor.ui.kubebrowser

import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ObjectScanMode
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.allows
import name.levis.ichor.model.lastOf
import name.levis.ichor.model.scanMode
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.imagescan.AppScanSection
import name.levis.ichor.ui.imagescan.AppScanUi
import name.levis.ichor.ui.imagescan.ImageScanReportOpen
import name.levis.ichor.ui.imagescan.ObjectImageScanViewModel
import name.levis.ichor.ui.imagescan.ObjectScanTarget
import name.levis.ichor.ui.uiText

/** Whether [ref]'s object screen offers the scan: a kind it takes, for a role with the Kubernetes API (os:admin). */
@Composable
fun offersObjectScan(ref: KubeObjectRef): Boolean {
    if (ref.scanMode == null) return false
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    return config?.activeSummary?.allows(Feature.WORKLOADS) == true
}

/**
 * The vulnerability scan of [ref]'s images on its summary (a pod's or a workload's, see
 * [ObjectImageScanViewModel]), the same section as an app's sheet: the scan running, its last
 * or kept report, the report itself over the screen. [onScanReports] opens the reports kept for
 * a cluster (its context name). Names reach the core as shown: it reveals masked ones itself.
 */
@Composable
fun KubeObjectScanSection(ref: KubeObjectRef, onScanReports: (String) -> Unit) {
    val context = LocalContext.current
    val application = context.applicationContext as TalosApp
    val vm: ObjectImageScanViewModel = viewModel(
        key = "kube-object-scan-${ref.group}/${ref.resource}/${ref.namespace}/${ref.name}",
        factory = factory { ObjectImageScanViewModel(app.imageScanRepository, app.kubeRepository, ref) },
    )
    val config by application.configRepository.config.collectAsStateWithLifecycle()
    val target by vm.state.collectAsStateWithLifecycle()
    val session by vm.session.collectAsStateWithLifecycle()
    val scanner by application.uiPreferences.imageScanner.collectAsStateWithLifecycle()
    val saved by vm.saved.collectAsStateWithLifecycle()
    val cluster = config?.activeContext
    LaunchedEffect(cluster) { vm.load(cluster) }
    // The kept reports, read again when a scan ends or the cluster changes.
    LaunchedEffect(cluster, session?.running) { cluster?.let(vm::loadSaved) }
    var reportOpen by remember { mutableStateOf<Pair<ImageScanReport, String>?>(null) }
    val scope = rememberCoroutineScope()

    val own = session?.takeIf { it.appId == vm.id && it.context == cluster }
    val workload = vm.mode == ObjectScanMode.WORKLOAD
    val read = target
    AppScanSection(
        AppScanUi(
            operator = operatorState(read, workload),
            session = own,
            busyElsewhere = session?.let { it.running && (it.appId != vm.id || it.context != cluster) } == true,
            scanner = scanner,
            onScan = vm::scan,
            onStop = vm::stop,
            onOpen = { report, json -> reportOpen = report to json },
            saved = saved.lastOf(vm.id),
            onOpenSaved = { kept ->
                scope.launch {
                    runCatching { vm.openSaved(kept.id) }
                        .onSuccess { reportOpen = it }
                        .onFailure { e ->
                            val reason = e.uiText().resolve(context)
                            Toast.makeText(context, context.getString(R.string.imagescan_saved_open_failed, reason), Toast.LENGTH_LONG).show()
                        }
                }
            },
            onSavedList = { cluster?.let(onScanReports) },
            note = when {
                !workload -> null
                read is UiState.Failed -> stringResource(R.string.imagescan_failed, read.message.asString())
                else -> stringResource(R.string.imagescan_object_refs_note)
            },
            canStart = !workload || (read as? UiState.Loaded)?.data?.images?.isNotEmpty() == true,
        ),
    )
    reportOpen?.let { (report, json) ->
        ImageScanReportOpen(report, json, ref.name, vm::export, onDismiss = { reportOpen = null })
    }
}

/** A workload has no pods to ask the Trivy Operator about: its failure is the image refs' (the note). */
private val NO_OPERATOR_REPORTS: UiState<OperatorReports> = UiState.Loaded(OperatorReports())

/** The Trivy Operator's reports on a pod as [target] read them; none for a workload. */
private fun operatorState(target: UiState<ObjectScanTarget>, workload: Boolean): UiState<OperatorReports> = when {
    workload -> NO_OPERATOR_REPORTS
    target is UiState.Loaded -> UiState.Loaded(target.data.operator, fetchedAt = target.fetchedAt)
    target is UiState.Failed -> target
    else -> UiState.Loading
}
