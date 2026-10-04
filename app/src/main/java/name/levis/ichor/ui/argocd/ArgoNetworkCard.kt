package name.levis.ichor.ui.argocd

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.AltRoute
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.Router
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoNetNode
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.dataservices.color

/**
 * One box of the graph: a 3 dp accent bar in its health colour, the kind's icon, its name and
 * a mono detail line. Someone else's Gateway or route is outlined instead of filled; the app's
 * own resources carry a small dot.
 */
@Composable
fun ArgoNetCard(node: ArgoNetNode, selected: Boolean, dimmed: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val accent = node.healthState.color()
    val scheme = MaterialTheme.colorScheme
    val border = when {
        selected -> BorderStroke(1.5.dp, accent)
        node.shared -> BorderStroke(1.dp, scheme.outlineVariant)
        else -> null
    }
    Surface(
        onClick = onClick,
        modifier = modifier.width(CARD_WIDTH).height(CARD_HEIGHT).alpha(if (dimmed) DIM_ALPHA else 1f),
        shape = RoundedCornerShape(12.dp),
        color = if (node.shared) scheme.surface else scheme.surfaceContainerHigh,
        border = border,
    ) {
        Row {
            Box(Modifier.width(3.dp).fillMaxHeight().background(accent))
            Column(Modifier.padding(start = 8.dp, end = 8.dp).weight(1f).fillMaxHeight(), verticalArrangement = Arrangement.Center) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(node.kindIcon, contentDescription = node.kindLabel(), tint = accent, modifier = Modifier.size(14.dp))
                    Text(
                        node.name,
                        style = MaterialTheme.typography.labelLarge,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(start = 5.dp).weight(1f, fill = false),
                    )
                    if (node.managed) {
                        Box(
                            Modifier.padding(start = 4.dp).size(6.dp).background(scheme.primary, CircleShape),
                        )
                    }
                }
                if (node.detail.isNotEmpty()) {
                    Text(
                        node.detail,
                        style = MaterialTheme.typography.labelSmall,
                        fontFamily = FontFamily.Monospace,
                        color = scheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
        }
    }
}

/** "Pods · 3" above a column. */
@Composable
fun ArgoNetColumnHeader(layer: Int, count: Int, modifier: Modifier = Modifier) {
    val label = stringResource(layerLabel(layer))
    Text(
        if (count > 1) "$label · $count" else label,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier,
    )
}

/** "+5 more" under a folded column, "Show less" once unfolded. */
@Composable
fun ArgoNetMoreChip(hidden: Int, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Surface(
        onClick = onClick,
        modifier = modifier.height(28.dp),
        shape = RoundedCornerShape(50),
        color = MaterialTheme.colorScheme.secondaryContainer,
    ) {
        Box(contentAlignment = Alignment.Center) {
            Text(
                if (hidden > 0) stringResource(R.string.argo_net_more, hidden) else stringResource(R.string.argo_net_less),
                style = MaterialTheme.typography.labelMedium,
            )
        }
    }
}

/** The three edge colours and what they mean. */
@Composable
fun ArgoNetLegend(modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
        LegendChip(ServiceHealth.OK.color(), stringResource(R.string.argo_net_legend_ok))
        LegendChip(ServiceHealth.WARNING.color(), stringResource(R.string.argo_net_legend_warning))
        LegendChip(ServiceHealth.CRITICAL.color(), stringResource(R.string.argo_net_legend_critical))
    }
}

@Composable
private fun LegendChip(color: Color, label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(14.dp).height(4.dp).background(color, RoundedCornerShape(2.dp)))
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 6.dp))
    }
}

/** Column title of a layer: Internet, Gateway, Routes, Services, Pods, Nodes. */
fun layerLabel(layer: Int): Int = when (layer) {
    ArgoNetNode.LAYER_HOST -> R.string.argo_net_layer_internet
    ArgoNetNode.LAYER_GATEWAY -> R.string.argo_net_layer_gateway
    ArgoNetNode.LAYER_ROUTE -> R.string.argo_net_layer_routes
    ArgoNetNode.LAYER_SERVICE -> R.string.argo_net_layer_services
    ArgoNetNode.LAYER_POD -> R.string.argo_net_layer_pods
    else -> R.string.argo_net_layer_nodes
}

val ArgoNetNode.kindIcon: ImageVector
    get() = when (kind) {
        ArgoNetNode.HOST -> Icons.Outlined.Public
        ArgoNetNode.LOAD_BALANCER -> Icons.Outlined.Router
        ArgoNetNode.GATEWAY -> Icons.Outlined.Hub
        ArgoNetNode.INGRESS, ArgoNetNode.HTTP_ROUTE -> Icons.AutoMirrored.Outlined.AltRoute
        ArgoNetNode.SERVICE -> Icons.Outlined.Lan
        ArgoNetNode.POD -> Icons.Outlined.ViewInAr
        else -> Icons.Outlined.Computer
    }

/** Kubernetes kinds keep their name; a host is ours to translate. */
@Composable
fun ArgoNetNode.kindLabel(): String = if (kind == ArgoNetNode.HOST) stringResource(R.string.argo_net_kind_host) else kind
