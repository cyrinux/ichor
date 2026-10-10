package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeSpanDiagnosticsTest {

    private val json = """{"nodes":[
      {"node":"10.0.0.1","hostname":"cp-1","config":{"enabled":true,"mtu":1420,"forceRouting":false,"advertiseKubernetesNetworks":true,
        "endpointFilters":[],"harvestExtraEndpoints":true},"linkMtu":1420,
       "peers":[{"publicKey":"k2","label":"w-1","state":"down","address":"fd00::2","allowedIPs":["fd00::2/128"],
         "endpointsTried":["203.0.113.5:51820"],"endpoint":"","lastUsedEndpoint":"","lastHandshake":0,"lastEndpointChange":0,"rx":0,"tx":0,
         "verdicts":[{"kind":"staleHandshake","message":"no handshake ever completed (tried 203.0.113.5:51820)"}]}],
       "siderolink":{"host":"omni.example.invalid:8090","connected":true,"linkName":"siderolink","grpcTunnel":false,"nodeAddress":"","mtu":1280},
       "errors":{}},
      {"node":"10.0.0.2","hostname":"w-1","config":null,"linkMtu":0,"peers":[],"siderolink":null,"errors":{"config":"unreachable"}}
    ]}"""

    @Test
    fun decodesTheCoreJson() {
        val all = TalosJson.decodeFromString(KubeSpanDiagAll.serializer(), json)
        val peer = all.peer("10.0.0.1", "k2")!!
        assertEquals("staleHandshake", peer.verdicts.single().kind)
        assertEquals(listOf("203.0.113.5:51820"), peer.endpointsTried)
        assertTrue(all.node("10.0.0.1")!!.siderolink!!.connected)
        assertNull(all.node("10.0.0.2")!!.config)
        assertEquals("unreachable", all.node("10.0.0.2")!!.errors["config"])
        assertNull(all.peer("10.0.0.9", "k2"))
    }
}
