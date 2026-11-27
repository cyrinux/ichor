package name.levis.ichor.ui.network

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.BgpPeer
import name.levis.ichor.model.sinceMillis
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * One BGP session of the node: the peer and its state, the AS numbers, the prefixes received
 * and advertised and how long it has been up. A peer restarting under graceful restart keeps
 * its routes: shown as such, not as down.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun BgpPeerRow(peer: BgpPeer) {
    val colors = LocalStatusColors.current
    val stateColor = when {
        peer.established -> colors.ok
        peer.peerRestarting -> colors.warn
        else -> colors.bad
    }
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                peer.peer.ifEmpty { "?" },
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            StatusPill(peer.state.lowercase().replace('_', ' '), stateColor)
        }
        MutedText(
            listOfNotNull(
                stringResource(R.string.network_bgp_asn, peer.peerAsn.toString(), peer.localAsn.toString()),
                peer.instance.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.network_bgp_instance, it) },
            ).joinToString("  ·  "),
        )
        val up = timeAgo(peer.sinceMillis).takeIf { peer.established && it.isNotEmpty() }
        MutedText(
            listOfNotNull(
                stringResource(R.string.network_bgp_prefixes, peer.received.toString(), peer.advertised.toString()),
                up?.let { stringResource(R.string.network_bgp_since, it) },
            ).joinToString("  ·  "),
        )
        if (peer.gracefulRestart || peer.peerRestarting) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (peer.peerRestarting) Tag(stringResource(R.string.network_bgp_restarting))
                if (peer.gracefulRestart) Tag(stringResource(R.string.network_bgp_graceful))
            }
        }
    }
}
