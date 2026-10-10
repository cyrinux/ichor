package name.levis.ichor.ui.kubespan

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.KubeSpanDiag
import name.levis.ichor.model.KubeSpanDiagAll
import name.levis.ichor.model.KubeSpanDiagPeer
import name.levis.ichor.model.SiderolinkDiag
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors

/** Every node's KubeSpan diagnosis, read once the peers tab shows (KubeSpanDiagnosticsAll). */
class KubeSpanDiagViewModel(private val talos: TalosRepository) : LoadingViewModel<KubeSpanDiagAll>() {
    override suspend fun fetch() = talos.kubespanDiagnostics()

    fun load() {
        if (state.value == UiState.Loading) refresh()
    }
}

/** The peer as [node] sees it, with why its link is down. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PeerSheet(nodeName: String, peerName: String, diag: UiState<KubeSpanDiagAll>, node: String, publicKey: String, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text("$nodeName → $peerName", style = MaterialTheme.typography.titleMedium)
            when (diag) {
                UiState.Loading -> MutedText(stringResource(R.string.kubespan_diag_loading))
                is UiState.Failed -> Text(diag.message.asString(), color = LocalStatusColors.current.bad)
                is UiState.Loaded -> {
                    val all = diag.data
                    val ofNode = all.nodes.firstOrNull { it.node == node }
                    val peer = ofNode?.peers?.firstOrNull { it.publicKey == publicKey }
                    if (peer == null) {
                        MutedText(stringResource(R.string.kubespan_diag_unknown))
                    } else {
                        PeerDiagnosis(peer, ofNode)
                    }
                }
            }
        }
    }
}

@Composable
private fun PeerDiagnosis(peer: KubeSpanDiagPeer, node: KubeSpanDiag) {
    val colors = LocalStatusColors.current
    val now = System.currentTimeMillis() / 1000
    if (peer.verdicts.isEmpty()) {
        Text(stringResource(R.string.kubespan_diag_ok), color = colors.ok)
    } else {
        peer.verdicts.forEach { Text(it.message, color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
    }

    SectionTitle(stringResource(R.string.kubespan_diag_endpoints))
    if (peer.endpointsTried.isEmpty()) {
        MutedText(stringResource(R.string.kubespan_diag_none))
    } else {
        peer.endpointsTried.forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
    }
    peer.endpoint.ifBlank { null }?.let { MutedText(stringResource(R.string.kubespan_diag_endpoint, it)) }
    peer.lastHandshake.takeIf { it > 0 }?.let { MutedText(stringResource(R.string.kubespan_handshake_ago, localizedDuration(now - it))) }
    peer.lastEndpointChange.takeIf { it > 0 }?.let { MutedText(stringResource(R.string.kubespan_diag_endpoint_change, localizedDuration(now - it))) }
    node.config?.let { config ->
        val mtu = if (config.mtu > 0) config.mtu.toString() else stringResource(R.string.kubespan_diag_default)
        MutedText(stringResource(R.string.kubespan_diag_mtu, mtu, node.linkMtu.takeIf { it > 0 }?.toString() ?: "—"))
    }

    var details by remember { mutableStateOf(false) }
    TextButton(onClick = { details = !details }) { Text(stringResource(R.string.kubespan_diag_details)) }
    if (details) {
        val lines = listOfNotNull(
            "publicKey: ${peer.publicKey}",
            peer.address.ifBlank { null }?.let { "address: $it" },
            "allowedIPs: ${peer.allowedIPs.joinToString(", ")}",
            peer.lastUsedEndpoint.ifBlank { null }?.let { "lastUsedEndpoint: $it" },
            "state: ${peer.state}",
        )
        lines.forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
    }
}

/** A node's SideroLink connection (Omni), shown in its card. */
@Composable
fun SiderolinkRow(link: SiderolinkDiag) {
    val colors = LocalStatusColors.current
    Column {
        StatusPill(
            stringResource(R.string.kubespan_siderolink) + " · " +
                stringResource(if (link.connected) R.string.kubespan_siderolink_connected else R.string.kubespan_siderolink_disconnected),
            if (link.connected) colors.ok else colors.bad,
        )
        MutedText(link.host)
    }
}
