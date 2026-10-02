package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class NodeDiscoveryTest {

    private val json = """
        {"context":"prod","nodes":[
          {"address":"10.0.0.1","addresses":["10.0.0.1"],"hostname":"cp-1","role":"controlplane","known":true},
          {"address":"10.0.0.11","addresses":["fe80::1","10.0.0.11"],"hostname":"worker-a","role":"worker","known":false},
          {"address":"","addresses":[],"hostname":"ghost","role":"worker","known":false},
          {"address":"10.0.0.12","addresses":["10.0.0.12"],"hostname":"worker-b","role":"worker","known":false}
        ]}
    """.trimIndent()

    private val discovery = TalosJson.decodeFromString(NodeDiscovery.serializer(), json)

    @Test
    fun missingSkipsKnownMembersAndThoseWithoutAddress() {
        assertEquals(listOf("worker-a", "worker-b"), discovery.missing.map { it.hostname })
    }

    @Test
    fun dismissedMembersAreNotOfferedAgain() {
        assertEquals(listOf("10.0.0.12"), discovery.toOffer(setOf("10.0.0.11")).map { it.address })
    }

    @Test
    fun allKnownOffersNothing() {
        val known = discovery.copy(nodes = discovery.nodes.map { it.copy(known = true) })
        assertEquals(emptyList<DiscoveredNode>(), known.toOffer(emptySet()))
    }
}
