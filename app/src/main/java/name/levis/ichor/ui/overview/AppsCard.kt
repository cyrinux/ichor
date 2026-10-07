package name.levis.ichor.ui.overview

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.argocd.ArgoTileBadge
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.attentionCount
import name.levis.ichor.model.overviewTiles
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppIconPlaceholder
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

private const val TILES = 6
private val TILE = 36.dp

/**
 * The cluster's apps at a glance, opening the Apps screen: how many run, how many need a
 * look, and a few icons. A skeleton while loading; nothing on failure or without apps, so
 * the inventory never gets in the way of the overview. [argoBadges] marks the apps whose Argo
 * CD app is critical or OutOfSync (by inventory id); its "needs a look" pill opens the Apps
 * screen on that chip with [onAttention].
 */
@Composable
fun AppsCard(state: UiState<Inventory>, onOpen: () -> Unit, argoBadges: Map<String, ServiceHealth> = emptyMap(), onAttention: () -> Unit = onOpen) {
    when (state) {
        UiState.Loading -> AppsCardFrame(subtitle = null, attention = 0, onOpen = onOpen, onAttention = onAttention) {
            repeat(TILES) { AppIconPlaceholder(size = TILE) }
        }
        is UiState.Failed -> Unit
        is UiState.Loaded -> {
            val apps = state.data.apps
            if (apps.isEmpty()) return
            val running = apps.count { !it.system }
            val containers = apps.sumOf { it.containers }
            val subtitle = listOf(
                pluralStringResource(R.plurals.apps_running, running, running),
                pluralStringResource(R.plurals.apps_containers, containers, containers),
            ).joinToString(" · ")
            AppsCardFrame(subtitle, apps.attentionCount, onOpen, onAttention) {
                val tiles = apps.overviewTiles(TILES)
                tiles.forEach { app ->
                    Box {
                        AppIconTile(app, size = TILE)
                        argoBadges[app.id]?.let { level ->
                            ArgoTileBadge(level == ServiceHealth.CRITICAL, Modifier.align(Alignment.BottomEnd).offset(x = 3.dp, y = 3.dp), size = 14.dp)
                        }
                    }
                }
                val rest = running - tiles.size
                if (rest > 0) MoreTile(rest)
            }
        }
    }
}

@Composable
private fun AppsCardFrame(subtitle: String?, attention: Int, onOpen: () -> Unit, onAttention: () -> Unit, tiles: @Composable () -> Unit) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.apps_title), style = MaterialTheme.typography.titleMedium)
                    if (subtitle != null) {
                        MutedText(subtitle)
                    }
                }
                if (attention > 0) {
                    StatusPill(
                        pluralStringResource(R.plurals.apps_attention, attention, attention),
                        LocalStatusColors.current.warn,
                        Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = onAttention),
                    )
                }
                Icon(
                    Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 4.dp),
                )
            }
            Row(Modifier.padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) { tiles() }
        }
    }
}

/** "+12": the apps the row has no room for. */
@Composable
private fun MoreTile(count: Int) {
    Surface(shape = RoundedCornerShape(TILE * 0.3f), color = MaterialTheme.colorScheme.surfaceContainerHigh, modifier = Modifier.size(TILE)) {
        Box(contentAlignment = Alignment.Center) {
            Text("+$count", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
    }
}
