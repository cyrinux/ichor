package name.levis.ichor.model

import kotlinx.serialization.builtins.ListSerializer
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

    @Test
    fun historyKeepsTheNewestFirstAndAtMostTheLimit() {
        val history = listOf(3L, 2L, 1L).map { NetPerfReport(started = it) }
        val added = history.withReport(NetPerfReport(started = 4), limit = 3)
        assertEquals(listOf(4L, 3L, 2L), added.map { it.started })

        // The same test again (same start) replaces the saved one rather than duplicating it.
        val replaced = added.withReport(NetPerfReport(started = 3, client = "b"), limit = 3)
        assertEquals(listOf(3L, 4L, 2L), replaced.map { it.started })
        assertEquals("b", replaced[0].client)
    }

    @Test
    fun savedHistoryReadsBackWithItsSetup() {
        val report = NetPerfReport(
            server = "a", client = "b", hostNetwork = true, seconds = 5, started = 1,
            results = listOf(NetPerfResult(NETPERF_PATH_POD, NETPERF_LATENCY, transactionRate = 9.0, latency = NetPerfLatency(p50 = 58.0))),
        )
        val serializer = ListSerializer(NetPerfReport.serializer())
        val read = TalosJson.decodeFromString(serializer, TalosJson.encodeToString(serializer, listOf(report)))
        assertEquals(listOf(report), read)
        assertEquals(NetPerfSetup(server = "a", client = "b", hostNetwork = true, seconds = 5), report.setup)
    }

    @Test
    fun trendFollowsOnePairOldestFirst() {
        fun report(started: Long, client: String, server: String, vararg results: NetPerfResult) =
            NetPerfReport(server = server, client = client, started = started, results = results.toList())
        val history = listOf(
            report(
                3, "a", "b",
                NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, throughputMbps = 900.0),
                NetPerfResult(NETPERF_PATH_HOST, NETPERF_THROUGHPUT, throughputMbps = 5000.0),
                NetPerfResult(NETPERF_PATH_POD, NETPERF_LATENCY, latency = NetPerfLatency(p50 = 60.0)),
            ),
            report(2, "b", "a", NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, throughputMbps = 1.0)),
            report(
                1, "a", "b",
                NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, error = "refused"),
                NetPerfResult(NETPERF_PATH_POD, NETPERF_LATENCY, latency = NetPerfLatency(p50 = 80.0)),
            ),
        )
        val trend = history.trendOf("a", "b")
        assertEquals(listOf(1L, 3L), trend.map { it.started })
        assertEquals(listOf(null, 900.0), trend.map { it.throughputMbps })
        assertEquals(listOf(80.0, 60.0), trend.map { it.p50Us })
        assertTrue(history.trendOf("b", "a").size == 1)
    }

    @Test
    fun latestBetweenTakesEitherDirectionWithAThroughput() {
        val ok = listOf(NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, throughputMbps = 900.0))
        val failed = listOf(NetPerfResult(NETPERF_PATH_POD, NETPERF_THROUGHPUT, error = "refused"))
        val history = listOf(
            NetPerfReport(client = "a", server = "b", started = 4, results = failed),
            NetPerfReport(client = "b", server = "a", started = 3, results = ok),
            NetPerfReport(client = "a", server = "b", started = 2, results = ok),
            NetPerfReport(client = "a", server = "c", started = 5, results = ok),
        )
        assertEquals(3L, history.latestBetween("a", "b")?.started)
        assertEquals(3L, history.latestBetween("b", "a")?.started)
        assertNull(history.latestBetween("b", "c"))
        assertEquals(900.0, history.latestBetween("c", "a")?.podThroughputMbps)
    }

    @Test
    fun pickingOnTheMapTakesTheClientThenTheServer() {
        assertEquals(listOf("a"), emptyList<String>().pickNode("a"))
        assertEquals(listOf("a", "b"), listOf("a").pickNode("b"))
        // Tapping the picked node again drops it.
        assertEquals(emptyList<String>(), listOf("a").pickNode("a"))
        assertEquals(listOf("a"), listOf("a", "b").pickNode("b"))
        // A third node starts a new pair.
        assertEquals(listOf("c"), listOf("a", "b").pickNode("c"))
    }

    @Test
    fun betweenKeepsOnlyReadyNodes() {
        val nodes = listOf(node("w-1"), node("w-2"), node("w-3", ready = false))
        val setup = NetPerfSetup(seconds = 20).between("w-2", "w-1", nodes)
        assertEquals("w-2", setup.client)
        assertEquals("w-1", setup.server)
        assertEquals(20, setup.seconds)
        // Unknown or not ready: the default pair fills in, like a refreshed node list.
        val fallback = NetPerfSetup().between("w-3", "gone", nodes)
        assertEquals("w-2", fallback.client)
        assertEquals("w-1", fallback.server)
        // Node list not loaded yet: kept as asked, checked once it loads.
        assertEquals("x", NetPerfSetup().between("x", "y", null).client)
    }
}
