package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeNodesTest {

    @Test
    fun decodesTheCoreJson() {
        val json = """
            {"serverVersion":"v1.31.2","nodes":[
              {"name":"cp-1","roles":["control-plane"],"ready":true,"cordoned":false,"internalIP":"10.0.0.2",
               "kubelet":"v1.31.2","cpu":4,"memory":8.0E9,"podLimit":110,"pressure":null,"created":1790000000},
              {"name":"w-1","roles":null,"ready":false,"cordoned":true,"externalIP":"203.0.113.9",
               "pool":"general","poolKind":"karpenter","instanceType":"m6i.large","capacity":"spot",
               "cpu":2,"memory":4.0E9,"podLimit":110,"pressure":["DiskPressure"],"created":1790000000}
            ]}
        """.trimIndent()

        val overview = TalosJson.decodeFromString(KubeNodesOverview.serializer(), json)

        assertEquals("v1.31.2", overview.serverVersion)
        assertEquals(1, overview.readyCount)
        assertFalse(overview.forbidden)
        assertEquals("10.0.0.2", overview.nodes[0].address)
        assertEquals("203.0.113.9", overview.nodes[1].address)
        assertTrue(overview.nodes[0].healthy)
        assertFalse(overview.nodes[1].healthy)
        assertEquals(emptyList<String>(), overview.nodes[1].roles)
        // Where the cloud put the node: absent for the bare-metal one.
        assertEquals("", overview.nodes[0].pool)
        assertEquals("", overview.nodes[0].capacity)
        assertEquals("general", overview.nodes[1].pool)
        assertEquals("karpenter", overview.nodes[1].poolKind)
        assertEquals("m6i.large", overview.nodes[1].instanceType)
        assertEquals("spot", overview.nodes[1].capacity)
    }

    @Test
    fun forbiddenListsNoNodes() {
        val overview = TalosJson.decodeFromString(KubeNodesOverview.serializer(), """{"serverVersion":"","nodes":[],"forbidden":true}""")

        assertTrue(overview.forbidden)
        assertEquals(0, overview.readyCount)
    }
}
