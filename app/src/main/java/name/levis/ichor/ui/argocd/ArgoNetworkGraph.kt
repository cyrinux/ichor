package name.levis.ichor.ui.argocd

import android.provider.Settings
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.model.ArgoNetEdge
import name.levis.ichor.model.ArgoNetNode
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.theme.LocalStatusColors

internal val CARD_WIDTH = 136.dp
internal val CARD_HEIGHT = 56.dp
private val ROW_GAP = 10.dp
private val COLUMN_GAP = 44.dp
private val HEADER_HEIGHT = 26.dp
private val CHIP_HEIGHT = 28.dp

/** Where one box sits on the canvas, its top-left corner. */
private data class CardSlot(val node: ArgoNetNode, val x: Dp, val y: Dp)

/** A folded column's "+N more" chip; [hidden] is 0 once unfolded (it then folds back). */
private data class ChipSlot(val hidden: Int, val x: Dp, val y: Dp)

private data class HeaderSlot(val layer: Int, val count: Int, val x: Dp)

/** Where a hop can end: a card, or the "+N more" chip standing for the folded pods. */
private data class Anchor(val x: Dp, val y: Dp, val height: Dp)

private data class GraphLayout(val width: Dp, val height: Dp, val cards: List<CardSlot>, val chips: List<ChipSlot>, val headers: List<HeaderSlot>) {
    val anchors: Map<String, Anchor> = cards.associate { it.node.id to Anchor(it.x, it.y, CARD_HEIGHT) } +
        chips.filter { it.hidden > 0 }.associate { ArgoNetwork.MORE_PODS to Anchor(it.x, it.y, CHIP_HEIGHT) }
}

/**
 * Lays the graph out in columns, one per layer that has boxes, each centred vertically under
 * its header. Fixed card sizes keep it a pure computation: the edges need no measuring.
 */
private fun layoutGraph(network: ArgoNetwork, expanded: Boolean): GraphLayout {
    val columns = network.layers.map { network.column(it, expanded) }
    // Only the pods fold; their chip stays once unfolded, to fold them back.
    val foldable = columns.map { it.layer == ArgoNetNode.LAYER_POD && network.nodesIn(it.layer).size > ArgoNetwork.COLUMN_LIMIT }
    val heights = columns.mapIndexed { i, c ->
        CARD_HEIGHT * c.nodes.size + ROW_GAP * (c.nodes.size - 1).coerceAtLeast(0) + if (foldable[i]) ROW_GAP + CHIP_HEIGHT else 0.dp
    }
    val body = heights.maxOrNull() ?: 0.dp
    val cards = mutableListOf<CardSlot>()
    val chips = mutableListOf<ChipSlot>()
    columns.forEachIndexed { i, c ->
        val x = (CARD_WIDTH + COLUMN_GAP) * i
        val top = HEADER_HEIGHT + (body - heights[i]) / 2
        c.nodes.forEachIndexed { row, node -> cards += CardSlot(node, x, top + (CARD_HEIGHT + ROW_GAP) * row) }
        if (foldable[i]) chips += ChipSlot(c.hidden, x, top + heights[i] - CHIP_HEIGHT)
    }
    val width = CARD_WIDTH * columns.size + COLUMN_GAP * (columns.size - 1).coerceAtLeast(0)
    val headers = columns.mapIndexed { i, c -> HeaderSlot(c.layer, network.layerCounts[c.layer] ?: 0, (CARD_WIDTH + COLUMN_GAP) * i) }
    return GraphLayout(width, HEADER_HEIGHT + body, cards, chips, headers)
}

/**
 * The app's network as a left-to-right layered graph on a horizontally scrolling canvas:
 * hosts, Gateways, routes, Services, pods and nodes, joined by Bézier hops coloured by health,
 * with traffic flowing along the ones that serve. Tapping a box highlights everything its
 * traffic crosses ([selected]); tapping it again or the background clears it.
 */
@Composable
fun ArgoNetworkGraph(network: ArgoNetwork, selected: String?, onSelect: (String?) -> Unit, modifier: Modifier = Modifier) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    val layout = remember(network, expanded) { layoutGraph(network, expanded) }
    val hidden = remember(network, expanded) { network.hiddenIds(expanded) }
    val hops = remember(network, hidden) { network.edgesHiding(hidden) }
    val path = remember(network, selected, hidden) { selected?.let { network.pathThrough(it, hidden) } }
    val phase = flowPhase(animate = hops.any { it.healthState == ServiceHealth.OK || it.healthState == ServiceHealth.WARNING })
    val palette = edgePalette()
    Box(modifier.horizontalScroll(rememberScrollState())) {
        Box(
            Modifier.size(layout.width, layout.height)
                .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { onSelect(null) },
        ) {
            Canvas(Modifier.size(layout.width, layout.height)) {
                hops.forEach { edge ->
                    val from = layout.anchors[edge.from] ?: return@forEach
                    val to = layout.anchors[edge.to] ?: return@forEach
                    val dim = path != null && !path.contains(edge)
                    drawHop(edge, from, to, palette, phase?.value, if (dim) DIM_ALPHA else 1f)
                }
            }
            layout.headers.forEach { h ->
                ArgoNetColumnHeader(h.layer, h.count, Modifier.offset(x = h.x).width(CARD_WIDTH))
            }
            layout.cards.forEach { slot ->
                ArgoNetCard(
                    slot.node,
                    selected = slot.node.id == selected,
                    dimmed = path != null && !path.contains(slot.node.id),
                    onClick = { onSelect(if (slot.node.id == selected) null else slot.node.id) },
                    modifier = Modifier.offset(slot.x, slot.y),
                )
            }
            layout.chips.forEach { chip ->
                val dimmed = path != null && !path.contains(ArgoNetwork.MORE_PODS)
                ArgoNetMoreChip(
                    chip.hidden,
                    onClick = { expanded = !expanded },
                    modifier = Modifier.offset(chip.x, chip.y).width(CARD_WIDTH).alpha(if (dimmed && chip.hidden > 0) DIM_ALPHA else 1f),
                )
            }
        }
    }
}

/** Edge colours by health: green serves, amber is degraded, red is broken, outline is idle. */
private data class EdgePalette(val ok: Color, val warn: Color, val bad: Color, val idle: Color) {
    fun of(health: ServiceHealth): Color = when (health) {
        ServiceHealth.OK -> ok
        ServiceHealth.WARNING -> warn
        ServiceHealth.CRITICAL -> bad
        ServiceHealth.IDLE, ServiceHealth.UNKNOWN -> idle
    }
}

@Composable
private fun edgePalette(): EdgePalette {
    val colors = LocalStatusColors.current
    return EdgePalette(colors.ok, colors.warn, colors.bad, MaterialTheme.colorScheme.outline)
}

/**
 * How far the traffic dots have travelled along a dash cycle, in dp; null (the dots then stand
 * still) with animations turned off in the system settings, or with nothing to [animate]:
 * no hop serving, no frame drawn for nothing.
 */
@Composable
private fun flowPhase(animate: Boolean): State<Float>? {
    val context = LocalContext.current
    val motion = remember(context) {
        Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) != 0f
    }
    if (!motion || !animate) return null
    return rememberInfiniteTransition(label = "traffic").animateFloat(
        initialValue = FLOW_CYCLE,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(FLOW_MILLIS, easing = LinearEasing), RepeatMode.Restart),
        label = "traffic-phase",
    )
}

/**
 * One hop, a cubic Bézier from the source card's right middle to the target's left middle:
 * a faint track with dots flowing along it while it serves, red dashes standing still when broken.
 */
private fun DrawScope.drawHop(edge: ArgoNetEdge, from: Anchor, to: Anchor, palette: EdgePalette, phase: Float?, alpha: Float) {
    val start = Offset((from.x + CARD_WIDTH).toPx(), (from.y + from.height / 2).toPx())
    val end = Offset(to.x.toPx(), (to.y + to.height / 2).toPx())
    val bend = (end.x - start.x) / 2
    val curve = Path().apply {
        moveTo(start.x, start.y)
        cubicTo(start.x + bend, start.y, end.x - bend, end.y, end.x, end.y)
    }
    val health = edge.healthState
    val color = palette.of(health)
    when (health) {
        ServiceHealth.CRITICAL -> drawPath(
            curve,
            color.copy(alpha = alpha),
            style = Stroke(2.dp.toPx(), pathEffect = PathEffect.dashPathEffect(floatArrayOf(6.dp.toPx(), 5.dp.toPx()))),
        )
        ServiceHealth.OK, ServiceHealth.WARNING -> {
            drawPath(curve, color.copy(alpha = TRACK_ALPHA * alpha), style = Stroke(2.dp.toPx()))
            val dots = PathEffect.dashPathEffect(floatArrayOf(0.1f, FLOW_CYCLE.dp.toPx()), (phase ?: 0f).dp.toPx())
            drawPath(curve, color.copy(alpha = alpha), style = Stroke(4.dp.toPx(), cap = StrokeCap.Round, pathEffect = dots))
        }
        ServiceHealth.IDLE, ServiceHealth.UNKNOWN -> drawPath(curve, color.copy(alpha = TRACK_ALPHA * alpha), style = Stroke(1.5.dp.toPx()))
    }
}

/** Alpha of what is off the highlighted path. */
internal const val DIM_ALPHA = 0.25f
private const val TRACK_ALPHA = 0.45f

/** Spacing between two traffic dots, in dp; one cycle per [FLOW_MILLIS]: calm, not busy. */
private const val FLOW_CYCLE = 16f
private const val FLOW_MILLIS = 900
