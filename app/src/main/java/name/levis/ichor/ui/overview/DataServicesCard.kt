package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.detected
import name.levis.ichor.model.likelyCauses
import name.levis.ichor.model.summary
import name.levis.ichor.model.worst
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.apps.AppIconPlaceholder
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.dataservices.HealthDot
import name.levis.ichor.ui.dataservices.KindIcon
import name.levis.ichor.ui.dataservices.LikelyCauseBanner
import name.levis.ichor.ui.dataservices.color
import name.levis.ichor.ui.dataservices.label
import name.levis.ichor.ui.dataservices.summaryText
import name.levis.ichor.ui.dataservices.title

private val ICON = 28.dp

/**
 * Longhorn, Garage and CloudNativePG at a glance, opening the Data services screen: one line
 * per system and, when a node that is not ready explains the problems, that node first.
 * Only composed when the inventory shows one of them; a skeleton while loading, one muted
 * line on failure so the overview stays calm.
 */
@Composable
fun DataServicesCard(
    state: UiState<DataServices>,
    hinted: List<DataServiceKind>,
    apps: Map<String, InventoryApp>,
    downNodes: Set<String>,
    onOpen: () -> Unit,
) {
    when (state) {
        UiState.Loading -> Frame(null, onOpen) {
            hinted.forEach { kind -> Line(kind, apps, text = null, health = null) }
        }
        is UiState.Failed -> Frame(null, onOpen) {
            Text(
                stringResource(R.string.data_services_unreadable, state.message.asString()),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        is UiState.Loaded -> {
            val services = state.data
            val kinds = services.detected
            if (kinds.isEmpty()) return
            val causes = remember(services, downNodes) { services.likelyCauses(downNodes) }
            Frame(services.worst, onOpen) {
                LikelyCauseBanner(causes, Modifier.padding(bottom = 4.dp))
                kinds.forEach { kind -> Line(kind, apps, summaryText(kind, services), services.summary(kind)?.health) }
            }
        }
    }
}

@Composable
private fun Frame(worst: ServiceHealth?, onOpen: () -> Unit, content: @Composable () -> Unit) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.data_services_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                if (worst != null && worst.needsAttention) StatusPill(worst.label(), worst.color())
                Icon(
                    Icons.AutoMirrored.Outlined.KeyboardArrowRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 4.dp),
                )
            }
            content()
        }
    }
}

/** A system: its icon, name and one line; a placeholder while [text] is null. */
@Composable
private fun Line(kind: DataServiceKind, apps: Map<String, InventoryApp>, text: String?, health: ServiceHealth?) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        if (text == null) AppIconPlaceholder(size = ICON) else KindIcon(kind, apps[kind.catalogId], ICON)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(kind.title, style = MaterialTheme.typography.bodyMedium)
            if (text != null) {
                Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (health != null) HealthDot(health)
    }
}
