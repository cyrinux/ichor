package name.levis.ichor.ui.images

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.ImageScanRepository
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.SystemImage
import name.levis.ichor.model.shortDigest
import name.levis.ichor.model.systemImagesScanId
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.imagescan.IMAGESCAN_NAMESPACE
import name.levis.ichor.ui.imagescan.ScanProgressLines
import name.levis.ichor.ui.imagescan.ScanSummary
import name.levis.ichor.ui.imagescan.coreErrorText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The images Talos runs on [node] outside of any app (kubelet, etcd, the control plane…), and
 * their vulnerability scan: the same Trivy Job as an app's, kept by [ImageScanRepository].
 */
class SystemImagesViewModel(
    private val talos: TalosRepository,
    private val scans: ImageScanRepository,
    private val node: String,
) : LoadingViewModel<List<SystemImage>>() {
    val session = scans.session

    override suspend fun fetch() = talos.systemImages(node)

    /** Scans [images] by ref (repo@digest when the node has it). */
    fun scan(images: List<SystemImage>) = scans.start(systemImagesScanId(node), emptyList(), images.map { it.ref })

    fun stop() = scans.stop()

    suspend fun export(report: ImageScanReport, json: String, format: ImageScanFormat): String =
        scans.export(json.ifEmpty { scans.encode(report) }, format)
}

/** What the section needs; [canScan] false for a role without the Kubernetes API (no scan offered). */
data class SystemImagesUi(
    val state: UiState<List<SystemImage>>,
    /** The scan of this node's system images running or last run, null when none. */
    val session: ImageScanSession?,
    /** Another scan runs: one at a time. */
    val busyElsewhere: Boolean,
    val canScan: Boolean,
    val onScan: (List<SystemImage>) -> Unit,
    val onStop: () -> Unit,
    val onOpen: (ImageScanReport, String) -> Unit,
)

/** "System images": their list, then the scan running, its last report, or how to start one. */
@Composable
fun SystemImagesSection(ui: SystemImagesUi) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SectionTitle(stringResource(R.string.imagescan_system_title))
        when (val state = ui.state) {
            UiState.Loading -> MutedText(stringResource(R.string.imagescan_system_loading))
            is UiState.Failed -> MutedText(stringResource(R.string.imagescan_system_unavailable, state.message.asString()))
            is UiState.Loaded -> {
                state.data.forEach { SystemImageRow(it) }
                if (ui.canScan && state.data.isNotEmpty()) ScanControls(ui, state.data)
            }
        }
    }
}

@Composable
private fun ScanControls(ui: SystemImagesUi, images: List<SystemImage>) {
    val session = ui.session
    if (session?.running == true) {
        ScanProgressLines(session, ui.onStop)
        return
    }
    session?.error?.let { Text(stringResource(R.string.imagescan_failed, coreErrorText(it)), color = LocalStatusColors.current.bad) }
    val report = session?.usableReport
    if (report != null) {
        ScanSummary(report)
    } else {
        MutedText(stringResource(R.string.imagescan_system_hint, IMAGESCAN_NAMESPACE))
    }
    if (ui.busyElsewhere) MutedText(stringResource(R.string.imagescan_busy_elsewhere))
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (report != null) {
            FilledTonalButton(onClick = { ui.onOpen(report, session.reportJson) }) {
                Icon(Icons.Outlined.Description, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.imagescan_open), modifier = Modifier.padding(start = 6.dp))
            }
        }
        OutlinedButton(onClick = { ui.onScan(images) }, enabled = !ui.busyElsewhere) {
            Icon(Icons.Outlined.Security, contentDescription = null, modifier = Modifier.size(18.dp))
            Text(
                stringResource(if (report != null) R.string.imagescan_scan_again else R.string.imagescan_system_scan),
                modifier = Modifier.padding(start = 6.dp),
            )
        }
    }
}

/** "kubelet  kubelet:v1.34.1  sha256:0123456789ab". */
@Composable
private fun SystemImageRow(image: SystemImage) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(image.role, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(end = 8.dp))
        Text(
            image.image.substringAfterLast('/'),
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (image.digest.isNotEmpty()) {
            Text(
                shortDigest(image.digest),
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 8.dp),
            )
        }
    }
}
