package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NetPerfTest {

    private fun node(name: String, controlPlane: Boolean = false, ready: Boolean = true) = NetPerfNode(name, "", controlPlane, ready)

    @Test
    fun decodesTheGoJson() {
        val json = """{"server":"a","client":"b","hostNetwork":true,"seconds":10,"image":"img","started":1,"finished":2,"results":[""" +
            """{"path":"pod","test":"throughput","throughputMbps":8734.5},""" +
            """{"path":"pod","test":"latency","transactionRate":15873.2,"latencyUs":{"min":38,"mean":62.4,"max":1874,"p50":58,"p90":74,"p99":131}},""" +
            """{"path":"host","test":"throughput","error":"no answer from netserver"}]}"""
        val report = TalosJson.decodeFromString(NetPerfReport.serializer(), json)
        assertEquals(8734.5, report.results[0].throughputMbps, 0.0)
        assertEquals(131.0, report.results[1].latency!!.p99, 0.0)
        assertEquals("no answer from netserver", report.results[2].error)

        val progress = TalosJson.decodeFromString(
            NetPerfProgress.serializer(),
            """{"phase":"testing","path":"pod","test":"latency","step":2,"steps":4,"at":5,"results":[]}""",
        )
        assertEquals(NETPERF_PHASE_TESTING, progress.phase)
        assertEquals(2, progress.step)
    }

    @Test
    fun defaultPairPrefersReadyWorkers() {
        val nodes = listOf(node("cp-1", controlPlane = true), node("w-1", ready = false), node("w-2"), node("w-3"))
        assertEquals("w-2" to "w-3", defaultNetPerfPair(nodes))
        assertEquals("cp-1" to "w-2", defaultNetPerfPair(listOf(node("cp-1", controlPlane = true), node("w-2"))))
        assertEquals("solo" to "solo", defaultNetPerfPair(listOf(node("solo", controlPlane = true))))
        assertNull(defaultNetPerfPair(listOf(node("down", ready = false))))
    }

    @Test
    fun setupKeepsChosenNodesThatAreStillReady() {
        val nodes = listOf(node("a"), node("b"), node("c"), node("gone", ready = false))
        assertEquals(NetPerfSetup("c", "a"), NetPerfSetup("c", "a").withNodes(nodes))
        assertEquals(NetPerfSetup("a", "b"), NetPerfSetup("gone", "x").withNodes(nodes))
        assertFalse(NetPerfSetup().withNodes(emptyList()).ready)
        assertTrue(NetPerfSetup().withNodes(nodes).ready)
        assertEquals(4, NetPerfSetup(hostNetwork = true).steps)
    }

    @Test
    fun formatsRatesAndLatencies() {
        assertEquals("9.41 Gbit/s", formatMbps(9412.8))
        assertEquals("338 Mbit/s", formatMbps(338.15))
        assertEquals("4.2 Mbit/s", formatMbps(4.2))
        assertEquals("58 µs", formatMicros(58.0))
        assertEquals("3.54 ms", formatMicros(3542.0))
    }
}
