package name.levis.ichor.ui

import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.TopologyLink
import name.levis.ichor.model.TopologyNode
import name.levis.ichor.model.TopologySite
import name.levis.ichor.ui.kubespan.MapPoint
import name.levis.ichor.ui.kubespan.TopologyLayout
import name.levis.ichor.ui.kubespan.linkAt
import name.levis.ichor.ui.kubespan.topologyLayout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TopologyLayoutTest {

    private val topology = ClusterTopology(
        nodes = listOf("a", "b", "c", "d", "e").map { TopologyNode(id = it, site = if (it == "e") "s2" else "s1") },
        sites = listOf(TopologySite("s1", nodes = listOf("a", "b", "c", "d")), TopologySite("s2", nodes = listOf("e"))),
        links = listOf(TopologyLink("a", "b", "up"), TopologyLink("a", "e", "down")),
    )

    @Test
    fun nodesSitInsideTheirSite() {
        val layout = topologyLayout(topology, width = 300f)

        assertEquals(2, layout.sites.size)
        layout.sites.forEach { box ->
            box.site.nodes.forEach { id ->
                val p = layout.nodes.getValue(id)
                assertTrue("$id above its site", p.y > box.top + TopologyLayout.HEADER - 1)
                assertTrue("$id below its site", p.y < box.top + box.height)
                assertTrue("$id outside the width", p.x in 0f..300f)
            }
        }
        // Four nodes: a row of three, then the fourth centred on the next row.
        assertEquals(150f, layout.nodes.getValue("d").x, 0.01f)
        assertEquals(100f, layout.cellWidth, 0.01f)
        assertTrue(layout.sites[1].top >= layout.sites[0].top + layout.sites[0].height)
        assertEquals(layout.sites[1].top + layout.sites[1].height, layout.height, 0.01f)
    }

    @Test
    fun crossSiteLinksBowToTheRight() {
        val layout = topologyLayout(topology, width = 300f)
        val a = layout.nodes.getValue("a")
        val e = layout.nodes.getValue("e")

        assertEquals(MapPoint((a.x + e.x) / 2, (a.y + e.y) / 2), layout.control(a, e, sameSite = true))
        assertTrue(layout.control(a, e, sameSite = false).x > (a.x + e.x) / 2)
    }

    @Test
    fun tapFindsTheClosestLink() {
        val layout = topologyLayout(topology, width = 300f)
        val a = layout.nodes.getValue("a")
        val b = layout.nodes.getValue("b")
        val midAB = MapPoint((a.x + b.x) / 2, (a.y + b.y) / 2)

        assertEquals(0, layout.linkAt(topology, midAB, slop = 10f))
        assertNull(layout.linkAt(topology, MapPoint(0f, layout.height + 200f), slop = 10f))
    }

    @Test
    fun emptyTopology() {
        val layout = topologyLayout(ClusterTopology(), width = 300f)

        assertEquals(0f, layout.height, 0f)
        assertTrue(layout.nodes.isEmpty())
    }
}
