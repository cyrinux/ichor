package name.levis.ichor.ui.network

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.LldpNeighbor
import name.levis.ichor.model.label
import name.levis.ichor.model.switchName
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.copyToClipboard

/**
 * One switch heard over LLDP: "eth0 → sw-a", its port, VLANs and management addresses. The
 * texts come from the network: shown as plain text, never as links; a long press copies them.
 */
@OptIn(ExperimentalFoundationApi::class, ExperimentalLayoutApi::class)
@Composable
internal fun NeighborRow(neighbor: LldpNeighbor) {
    val context = LocalContext.current
    val port = neighborPort(neighbor)
    val vlans = neighbor.vlans.map { stringResource(R.string.network_neighbor_vlan, it.label) }
    val management = neighbor.managementAddresses.map { stringResource(R.string.network_neighbor_management, it) }
    val copyText = (listOf("${neighbor.link} → ${neighbor.switchName}", port) + vlans + management).joinToString("\n")
    Column(
        Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = {},
                onLongClick = { copyToClipboard(context, neighbor.switchName, copyText) },
                onLongClickLabel = stringResource(R.string.network_neighbor_copy),
            )
            .padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            "${neighbor.link} → ${neighbor.switchName}",
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Bold,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        MutedText(port)
        if (neighbor.systemDescription.isNotEmpty()) {
            Text(
                neighbor.systemDescription,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (vlans.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                vlans.forEach { Tag(it, mono = true) }
            }
        }
        management.forEach {
            Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** "port Ethernet12 · rack 3 uplink": the port id, then its description when it says more. */
@Composable
internal fun neighborPort(neighbor: LldpNeighbor): String {
    val port = stringResource(R.string.network_neighbor_port, neighbor.portId.ifEmpty { "?" })
    val description = neighbor.portDescription.takeIf { it.isNotEmpty() && it != neighbor.portId }
    return listOfNotNull(port, description).joinToString(" · ")
}

/** The second line of a link row: the switch and port each neighbour on it announces. */
@Composable
internal fun LinkNeighbors(neighbors: List<LldpNeighbor>) {
    neighbors.forEach { n ->
        Text(
            "→ ${n.switchName} · ${stringResource(R.string.network_neighbor_port, n.portId.ifEmpty { "?" })}",
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.primary,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
