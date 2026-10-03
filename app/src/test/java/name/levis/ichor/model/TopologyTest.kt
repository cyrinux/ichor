package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class TopologyTest {

    @Test
    fun flagFromCountryCode() {
        assertEquals("🇫🇷", countryFlag("FR"))
        assertEquals("🇩🇰", countryFlag("dk"))
        assertEquals("", countryFlag(""))
        assertEquals("", countryFlag("FRA"))
        assertEquals("", countryFlag("F1"))
        assertEquals("", countryFlag("éé"))
    }

    @Test
    fun decodesTheGoCoreJson() {
        val json = """
            {"nodes":[{"id":"cp-1","node":"10.0.0.1","hostname":"cp-1","role":"controlplane","addresses":["10.0.0.1"],
              "zone":"fr-par-1","country":"FR","site":"site-1","kubespan":true,"queried":true},
              {"id":"vps","hostname":"vps","addresses":[],"site":"site-2","kubespan":false,"queried":false}],
             "links":[{"a":"cp-1","b":"vps","state":"degraded","sides":[{"from":"cp-1","to":"vps","state":"up",
              "endpoint":"203.0.113.5:51820","private":false,"rx":10,"tx":20,"lastHandshake":1800000000}]}],
             "sites":[{"id":"site-1","label":"fr-par-1","kind":"zone","country":"FR","nodes":["cp-1"]},
              {"id":"site-2","label":"","kind":"node","nodes":["vps"]}]}
        """.trimIndent()

        val topology = TalosJson.decodeFromString(ClusterTopology.serializer(), json)

        assertEquals(2, topology.nodes.size)
        assertEquals("FR", topology.sites[0].country)
        assertEquals("", topology.nodes[1].node)
        assertEquals(1, topology.brokenLinks("vps"))
        assertEquals(0, topology.brokenLinks("nobody"))
        assertEquals("203.0.113.5:51820", topology.links[0].sides[0].endpoint)
    }
}
