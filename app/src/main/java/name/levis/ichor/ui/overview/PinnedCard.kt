package name.levis.ichor.ui.overview

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PushPin
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoHealth
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSync
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.Pin
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.argocd.color
import name.levis.ichor.ui.argocd.label
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.flux.color
import name.levis.ichor.ui.flux.label
import name.levis.ichor.ui.search.kindIcon
import name.levis.ichor.ui.search.kindLabel
import name.levis.ichor.ui.theme.LocalStatusColors

/** What the overview already knows about the pinned objects, for their chips. */
class PinnedData(val nodes: List<NodeOverview>, val argo: ArgoStatus?, val flux: FluxStatus?)

/**
 * The overview's Pinned card: one row per object pinned on this cluster, in the order pinned,
 * with its state when the overview already holds it (nodes; Argo CD and Flux apps when the role
 * reads them). A tap opens it like its share link; a long press offers Unpin.
 */
@Composable
fun PinnedCard(
    pins: List<Pin>,
    data: PinnedData,
    onOpen: (Pin) -> Unit,
    onUnpin: (Pin) -> Unit,
    titleModifier: Modifier = Modifier,
) {
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Row(titleModifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.PushPin, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(18.dp))
                Spacer(Modifier.size(8.dp))
                Text(stringResource(R.string.overview_card_pinned), style = MaterialTheme.typography.titleMedium)
            }
            pins.forEach { pin -> PinnedRow(pin, data, onOpen = { onOpen(pin) }, onUnpin = { onUnpin(pin) }) }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun PinnedRow(pin: Pin, data: PinnedData, onOpen: () -> Unit, onUnpin: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val unpin = stringResource(R.string.pin_remove)
    Box {
        Row(
            Modifier.fillMaxWidth()
                .combinedClickable(onClick = onOpen, onLongClick = { menu = true }, onLongClickLabel = unpin)
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            val kind = pin.target.target
            Icon(kindIcon(kind), contentDescription = kindLabel(kind), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(pin.label, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                pinDetail(pin.target).takeIf { it.isNotEmpty() }?.let { MutedText(it, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
            pinChip(pin.target, data)?.let { (label, color) -> StatusPill(label, color, Modifier.padding(start = 8.dp)) }
        }
        DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
            DropdownMenuItem(
                text = { Text(unpin) },
                leadingIcon = { Icon(Icons.Outlined.PushPin, null) },
                onClick = {
                    menu = false
                    onUnpin()
                },
            )
        }
    }
}

/** The line under a pin's name: its kind and namespace, or a node's address. */
private fun pinDetail(target: ShareTarget): String = when (target.target) {
    ShareTarget.NODE -> target.addr
    else -> listOf(target.kind, target.namespace).filter { it.isNotEmpty() }.joinToString(" · ")
}

/** The state chip of a pin, from what the overview holds; null when it holds nothing on it. */
@Composable
private fun pinChip(target: ShareTarget, data: PinnedData): Pair<String, Color>? {
    val colors = LocalStatusColors.current
    return when (target.target) {
        ShareTarget.NODE -> data.nodes.firstOrNull { n ->
            target.addr.isNotEmpty() && n.node == target.addr || target.host.isNotEmpty() && n.hostname == target.host
        }?.let { n ->
            when {
                !n.reachable -> stringResource(R.string.common_status_unreachable) to colors.bad
                n.ready -> stringResource(R.string.common_status_ready) to colors.ok
                else -> stringResource(R.string.common_status_not_ready) to colors.warn
            }
        }
        ShareTarget.ARGO_APP -> data.argo?.apps?.firstOrNull { it.namespace == target.namespace && it.name == target.name }?.let { app ->
            val health = ArgoHealth.from(app.health)
            val sync = ArgoSync.from(app.sync)
            // Out of sync only shows when it is healthy: a broken app's health says more.
            if (health == ArgoHealth.HEALTHY && sync != ArgoSync.SYNCED) sync.label() to sync.color() else health.label() to health.color()
        }
        ShareTarget.FLUX_APP -> data.flux?.apps
            ?.firstOrNull { it.kind == target.kind && it.namespace == target.namespace && it.name == target.name }
            ?.let { app -> app.state.label() to app.state.color() }
        else -> null
    }
}
