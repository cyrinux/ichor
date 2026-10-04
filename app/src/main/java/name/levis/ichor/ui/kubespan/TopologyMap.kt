package name.levis.ichor.ui.kubespan

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.TopologyLink
import name.levis.ichor.model.isBroken
import name.levis.ichor.model.TopologyNode
import name.levis.ichor.model.TopologySite
import name.levis.ichor.model.brokenLinks
import name.levis.ichor.model.countryFlag
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.roundToInt

/**
 * The cluster map: sites stacked as boxes (flag and zone or subnet in the header), their
 * nodes as chips, and the KubeSpan links between nodes coloured by state. Links across sites
 * bow to the right so they do not run over the sites in between.
 */
@Composable
fun TopologyMap(
    topology: ClusterTopology,
    onNode: (TopologyNode) -> Unit,
    onLink: (TopologyLink) -> Unit,
    modifier: Modifier = Modifier,
    /** Throughput last measured on a link by the network test, shown at its middle. */
    speeds: Map<TopologyLink, String> = emptyMap(),
    /** Nodes picked for a network test: the client, then the server. */
    picked: List<String> = emptyList(),
) {
    val colors = LocalStatusColors.current
    val siteFill = MaterialTheme.colorScheme.surfaceContainerHigh
    val siteBorder = MaterialTheme.colorScheme.outlineVariant
    val description = stringResource(R.string.topology_map_description)
    val nodes = topology.nodes.associateBy { it.id }

    BoxWithConstraints(modifier.fillMaxWidth()) {
        val layout = topologyLayout(topology, maxWidth.value, LocalDensity.current.fontScale)
        Box(Modifier.fillMaxWidth().height(layout.height.dp)) {
            Canvas(
                Modifier
                    .fillMaxWidth()
                    .height(layout.height.dp)
                    .semantics { contentDescription = description }
                    .pointerInput(topology, layout) {
                        detectTapGestures { tap ->
                            val at = MapPoint(tap.x / density, tap.y / density)
                            layout.linkAt(topology, at, slop = 14f)?.let { onLink(topology.links[it]) }
                        }
                    },
            ) {
                val px = density
                layout.sites.forEach { box ->
                    drawRoundRect(siteFill, Offset(0f, box.top * px), Size(size.width, box.height * px), CornerRadius(16.dp.toPx()))
                    drawRoundRect(
                        siteBorder, Offset(0f, box.top * px), Size(size.width, box.height * px), CornerRadius(16.dp.toPx()),
                        style = Stroke(1.dp.toPx()),
                    )
                }
                // Healthy links first so broken ones are drawn on top.
                topology.links.sortedBy { it.isBroken }.forEach { link ->
                    val a = layout.nodes[link.a] ?: return@forEach
                    val b = layout.nodes[link.b] ?: return@forEach
                    val c = layout.control(a, b, nodes[link.a]?.site == nodes[link.b]?.site)
                    val path = Path().apply {
                        moveTo(a.x * px, a.y * px)
                        quadraticTo(c.x * px, c.y * px, b.x * px, b.y * px)
                    }
                    val color = when (link.state) {
                        "up" -> colors.ok
                        "down" -> colors.bad
                        "degraded" -> colors.warn
                        else -> colors.muted
                    }
                    drawPath(
                        path, color.copy(alpha = if (link.isBroken) 1f else 0.55f),
                        style = Stroke(
                            width = (if (link.isBroken) 3.dp else 2.dp).toPx(),
                            pathEffect = if (link.state == "up") null else PathEffect.dashPathEffect(floatArrayOf(10f * px, 6f * px)),
                        ),
                    )
                }
            }
            layout.sites.forEach { box ->
                SiteHeader(
                    box.site,
                    Modifier.centeredAt(12f, box.top + layout.header / 2, alignStart = true).widthIn(max = (layout.width - 24f).dp),
                )
            }
            speeds.forEach { (link, speed) ->
                val a = layout.nodes[link.a] ?: return@forEach
                val b = layout.nodes[link.b] ?: return@forEach
                val c = layout.control(a, b, nodes[link.a]?.site == nodes[link.b]?.site)
                // The middle of the quadratic curve: (a + 2c + b) / 4.
                SpeedPill(
                    speed,
                    onClick = { onLink(link) },
                    modifier = Modifier.centeredAt((a.x + 2 * c.x + b.x) / 4, (a.y + 2 * c.y + b.y) / 4),
                )
            }
            topology.nodes.forEach { node ->
                val at = layout.nodes[node.id] ?: return@forEach
                NodeChip(
                    node, topology.brokenLinks(node.id), picked.indexOf(node.id), maxWidth = (layout.cellWidth - 8f).dp,
                    onClick = { onNode(node) },
                    modifier = Modifier.centeredAt(at.x, at.y),
                )
            }
        }
    }
}


/**
 * Places the element centred on (x, y) dp of its parent (top left placed), or starting at x
 * when [alignStart]: like [Modifier.offset], with the offset depending on the element's size.
 */
private fun Modifier.centeredAt(x: Float, y: Float, alignStart: Boolean = false) = layout { measurable, constraints ->
    val placeable = measurable.measure(constraints.copy(minWidth = 0, minHeight = 0))
    layout(placeable.width, placeable.height) {
        val left = if (alignStart) x.dp.toPx() else x.dp.toPx() - placeable.width / 2f
        placeable.place(left.roundToInt(), (y.dp.toPx() - placeable.height / 2f).roundToInt())
    }
}

/** A link's measured throughput, in text ink on a surface pill so it reads over the line. */
@Composable
private fun SpeedPill(speed: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Surface(
        onClick = onClick,
        modifier = modifier,
        shape = RoundedCornerShape(8.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHighest,
        tonalElevation = 1.dp,
    ) {
        Text(
            speed,
            style = MaterialTheme.typography.labelSmall,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
        )
    }
}

/** The site's flag and zone, subnet or kind, as its header on the map says. */
@Composable
fun siteTitle(site: TopologySite): String {
    val flag = countryFlag(site.country)
    val label = site.label.ifBlank {
        stringResource(if (site.kind == "lan") R.string.topology_site_lan else R.string.topology_site_alone)
    }
    return listOf(flag, label).filter { it.isNotBlank() }.joinToString("  ")
}

@Composable
private fun SiteHeader(site: TopologySite, modifier: Modifier = Modifier) {
    Text(
        siteTitle(site),
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier,
    )
}

@Composable
private fun NodeChip(node: TopologyNode, broken: Int, pick: Int, maxWidth: Dp, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val dot = when {
        node.error != null -> colors.bad
        broken > 0 -> colors.warn
        node.queried -> colors.ok
        else -> colors.muted
    }
    val controlPlane = node.role == "controlplane"
    // Picked for a network test: outlined, and its role in the test instead of its zone.
    val pickLabel = when (pick) {
        0 -> stringResource(R.string.topology_pick_client)
        1 -> stringResource(R.string.topology_pick_server)
        else -> null
    }
    Surface(
        shape = RoundedCornerShape(12.dp),
        color = if (pickLabel != null) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface,
        tonalElevation = 2.dp,
        shadowElevation = 1.dp,
        border = pickLabel?.let { BorderStroke(2.dp, MaterialTheme.colorScheme.primary) },
        modifier = modifier.widthIn(max = maxWidth).clip(RoundedCornerShape(12.dp)).clickable(onClick = onClick),
    ) {
        Row(Modifier.padding(horizontal = 10.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            // Filled for a control plane, a ring for a worker.
            Canvas(Modifier.size(10.dp)) {
                if (controlPlane) drawCircle(dot) else drawCircle(dot, radius = size.minDimension / 2 - 1.dp.toPx(), style = Stroke(2.dp.toPx()))
            }
            Spacer(Modifier.width(6.dp))
            Column {
                Text(node.hostname.ifBlank { node.id }, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val flag = countryFlag(node.country)
                val zone = node.zone.takeIf { it.isNotBlank() }?.let { listOf(flag, it).filter(String::isNotBlank).joinToString(" ") }
                Text(
                    pickLabel ?: zone ?: stringResource(if (controlPlane) R.string.topology_role_controlplane else R.string.topology_role_worker),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** What the line styles mean. */
@Composable
fun TopologyLegend(modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
        LegendItem(colors.ok, dashed = false, stringResource(R.string.kubespan_peer_up))
        LegendItem(colors.warn, dashed = true, stringResource(R.string.topology_link_degraded))
        LegendItem(colors.bad, dashed = true, stringResource(R.string.kubespan_peer_down))
    }
}

@Composable
private fun LegendItem(color: Color, dashed: Boolean, label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Canvas(Modifier.width(22.dp).height(10.dp)) {
            drawLine(
                color, Offset(0f, size.height / 2), Offset(size.width, size.height / 2), strokeWidth = 3.dp.toPx(),
                pathEffect = if (dashed) PathEffect.dashPathEffect(floatArrayOf(5.dp.toPx(), 3.dp.toPx())) else null,
            )
        }
        Spacer(Modifier.width(6.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
