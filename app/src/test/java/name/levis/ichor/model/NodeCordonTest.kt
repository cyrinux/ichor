package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeCordonTest {

    @Test
    fun aCordonedReadyNodeNeedsALook() {
        val node = TalosJson.decodeFromString(
            NodeOverview.serializer(),
            """{"node":"10.0.0.5","hostname":"w-2","reachable":true,"role":"worker","stage":"running","ready":true,
               "unmetConditions":[],"cordoned":true,"cordonKnown":true}""",
        )
        assertTrue(node.cordoned)
        assertTrue(node.needsAttention)
        assertEquals(NodeStatus.ATTENTION, node.status)
    }

    @Test
    fun unknownIsNotCordoned() {
        val node = TalosJson.decodeFromString(
            NodeOverview.serializer(),
            """{"node":"10.0.0.6","hostname":"w-3","reachable":true,"role":"worker","stage":"running","ready":true,"unmetConditions":[]}""",
        )
        assertFalse(node.cordoned)
        assertFalse(node.cordonKnown)
        assertEquals(NodeStatus.READY, node.status)
    }
}
