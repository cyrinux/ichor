package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServiceKind
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageState
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.LikelyCause
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.health
import name.levis.ichor.model.summary
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.theme.LocalStatusColors

/** Product names: never translated. */
val DataServiceKind.title: String
    get() = when (this) {
        DataServiceKind.LONGHORN -> "Longhorn"
        DataServiceKind.GARAGE -> "Garage"
        DataServiceKind.CNPG -> "CloudNativePG"
        DataServiceKind.DRAGONFLY -> "Dragonfly"
    }

private val DataServiceKind.fallbackIcon: ImageVector
    get() = when (this) {
        DataServiceKind.LONGHORN -> Icons.Outlined.Storage
        DataServiceKind.GARAGE -> Icons.Outlined.Cloud
        DataServiceKind.CNPG -> Icons.Outlined.Dns
        DataServiceKind.DRAGONFLY -> Icons.Outlined.Memory
    }

/** Hostnames of the nodes Talos reports not ready or unreachable: candidates for a likely cause. */
fun ClusterOverview.downHostnames(): Set<String> =
    nodes.filter { it.health != NodeHealth.READY }.map { it.hostname }.filter { it.isNotEmpty() }.toSet()

@Composable
fun ServiceHealth.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        ServiceHealth.CRITICAL -> colors.bad
        ServiceHealth.WARNING -> colors.warn
        ServiceHealth.OK -> colors.ok
        ServiceHealth.IDLE, ServiceHealth.UNKNOWN -> colors.muted
    }
}

@Composable
fun ServiceHealth.label(): String = stringResource(
    when (this) {
        ServiceHealth.CRITICAL -> R.string.data_services_health_critical
        ServiceHealth.WARNING -> R.string.data_services_health_warning
        ServiceHealth.OK -> R.string.data_services_health_ok
        ServiceHealth.IDLE -> R.string.data_services_health_idle
        ServiceHealth.UNKNOWN -> R.string.data_services_health_unknown
    },
)

@Composable
fun GarageState.label(): String = stringResource(
    when (this) {
        GarageState.HEALTHY -> R.string.garage_status_healthy
        GarageState.DEGRADED -> R.string.garage_status_degraded
        GarageState.UNAVAILABLE -> R.string.garage_status_unavailable
        GarageState.UNKNOWN -> R.string.garage_status_unknown
    },
)

@Composable
fun HealthDot(health: ServiceHealth, modifier: Modifier = Modifier) {
    Box(modifier.size(10.dp).background(health.color(), CircleShape))
}

/** The app's own icon when the inventory has it, else a generic one. */
@Composable
fun KindIcon(kind: DataServiceKind, app: InventoryApp?, size: Dp) {
    if (app != null) {
        AppIconTile(app, size = size)
        return
    }
    Surface(shape = RoundedCornerShape(size * 0.3f), color = MaterialTheme.colorScheme.surfaceContainerHigh, modifier = Modifier.size(size)) {
        Box(contentAlignment = Alignment.Center) {
            Icon(kind.fallbackIcon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(size * 0.6f))
        }
    }
}

/** One line about a system: "12 volumes · 1 needs a look", "Degraded · 6/7 nodes up"... */
@Composable
fun summaryText(kind: DataServiceKind, services: DataServices): String {
    val summary = services.summary(kind) ?: return ""
    if (summary.error.isNotEmpty() && summary.total == 0) return stringResource(R.string.data_services_unreadable, summary.error)
    val attention = summary.attention.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.apps_attention, it, it) }
    val head = when (kind) {
        DataServiceKind.LONGHORN -> pluralStringResource(R.plurals.longhorn_volumes, summary.total, summary.total)
        DataServiceKind.CNPG -> pluralStringResource(R.plurals.cnpg_clusters, summary.total, summary.total)
        DataServiceKind.DRAGONFLY -> pluralStringResource(R.plurals.dragonfly_instances, summary.total, summary.total)
        DataServiceKind.GARAGE -> {
            val single = services.garage?.instances?.singleOrNull()
            if (single != null) {
                // One Garage cluster: its own state says more than "1 cluster".
                val nodes = single.takeIf { it.storageNodes > 0 }
                    ?.let { stringResource(R.string.garage_card_nodes, it.storageNodesUp, it.storageNodes) }
                return listOfNotNull(single.state.label(), nodes).joinToString(" · ")
            }
            pluralStringResource(R.plurals.garage_clusters, summary.total, summary.total)
        }
    }
    return listOfNotNull(head, attention).joinToString(" · ")
}

/** "worker-3 is not ready: the likely cause of 3 problems", one line per node. */
@Composable
fun LikelyCauseBanner(causes: List<LikelyCause>, modifier: Modifier = Modifier) {
    if (causes.isEmpty()) return
    val bad = LocalStatusColors.current.bad
    Surface(color = bad.copy(alpha = 0.12f), contentColor = bad, shape = RoundedCornerShape(12.dp), modifier = modifier.fillMaxWidth()) {
        Row(Modifier.padding(12.dp), verticalAlignment = Alignment.Top) {
            Icon(Icons.Outlined.WarningAmber, contentDescription = null, modifier = Modifier.size(20.dp))
            Spacer(Modifier.size(8.dp))
            Text(causes.map { it.text() }.joinToString("\n"), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun LikelyCause.text(): String = pluralStringResource(R.plurals.data_services_likely_cause, problems, node, problems)
