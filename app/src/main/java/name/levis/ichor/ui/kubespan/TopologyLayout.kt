package name.levis.ichor.ui.kubespan

import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.TopologySite
import kotlin.math.ceil
import kotlin.math.hypot
import kotlin.math.min

/** A point of the map, in dp from its top left corner. */
data class MapPoint(val x: Float, val y: Float)

data class SiteBox(val site: TopologySite, val top: Float, val height: Float)

/**
 * Where the map draws everything, in dp for a map [width] wide: sites stacked as boxes,
 * each with a header row then its nodes on a grid of up to [MAX_COLUMNS] per row.
 */
data class TopologyLayout(
    val width: Float,
    val height: Float,
    val sites: List<SiteBox>,
    val nodes: Map<String, MapPoint>,
    /** Width a node chip may take. */
    val cellWidth: Float,
    /** Height of a site header row. */
    val header: Float = HEADER,
) {
    /** Control point of link a-b: straight inside a site, bowed to the right across sites. */
    fun control(a: MapPoint, b: MapPoint, sameSite: Boolean): MapPoint {
        val mid = MapPoint((a.x + b.x) / 2, (a.y + b.y) / 2)
        if (sameSite) return mid
        val bow = min(width * 0.45f, kotlin.math.abs(b.y - a.y) * 0.35f + 24f)
        return MapPoint(min(width - 8f, mid.x + bow), mid.y)
    }

    companion object {
        const val MAX_COLUMNS = 3
        const val SITE_GAP = 16f
        const val HEADER = 36f
        const val CELL_HEIGHT = 60f
        const val SITE_PADDING = 8f
    }
}

/** [textScale]: the font scale, so rows grow with the chips in them. */
fun topologyLayout(topology: ClusterTopology, width: Float, textScale: Float = 1f): TopologyLayout {
    val scale = textScale.coerceAtLeast(1f)
    val header = TopologyLayout.HEADER * scale
    val cellHeight = TopologyLayout.CELL_HEIGHT * scale
    val boxes = mutableListOf<SiteBox>()
    val points = mutableMapOf<String, MapPoint>()
    var top = 0f
    val widest = topology.sites.maxOfOrNull { it.nodes.size.coerceAtMost(TopologyLayout.MAX_COLUMNS) } ?: 1
    topology.sites.forEach { site ->
        val columns = site.nodes.size.coerceIn(1, TopologyLayout.MAX_COLUMNS)
        val rows = ceil(site.nodes.size / columns.toFloat()).toInt().coerceAtLeast(1)
        val height = header + rows * cellHeight + TopologyLayout.SITE_PADDING
        site.nodes.forEachIndexed { i, id ->
            val row = i / columns
            val inRow = min(columns, site.nodes.size - row * columns)
            val cell = width / inRow
            points[id] = MapPoint(cell * (i % columns + 0.5f), top + header + cellHeight * (row + 0.5f))
        }
        boxes += SiteBox(site, top, height)
        top += height + TopologyLayout.SITE_GAP
    }
    val height = (top - TopologyLayout.SITE_GAP).coerceAtLeast(0f)
    return TopologyLayout(width, height, boxes, points, cellWidth = width / widest, header = header)
}

/**
 * The link whose curve passes within [slop] dp of [at], the closest one; null when none does.
 * Curves are sampled, which is plenty for a tap.
 */
fun TopologyLayout.linkAt(topology: ClusterTopology, at: MapPoint, slop: Float): Int? {
    val sites = topology.nodes.associate { it.id to it.site }
    var best: Int? = null
    var bestDistance = slop
    topology.links.forEachIndexed { index, link ->
        val a = nodes[link.a] ?: return@forEachIndexed
        val b = nodes[link.b] ?: return@forEachIndexed
        val c = control(a, b, sites[link.a] == sites[link.b])
        for (step in 0..SAMPLES) {
            val t = step / SAMPLES.toFloat()
            val p = quadratic(a, c, b, t)
            val d = hypot(p.x - at.x, p.y - at.y)
            if (d < bestDistance) {
                bestDistance = d
                best = index
            }
        }
    }
    return best
}

fun quadratic(a: MapPoint, c: MapPoint, b: MapPoint, t: Float): MapPoint {
    val u = 1 - t
    return MapPoint(u * u * a.x + 2 * u * t * c.x + t * t * b.x, u * u * a.y + 2 * u * t * c.y + t * t * b.y)
}

private const val SAMPLES = 24
