package name.levis.ichor.monitor

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.data.FleetRepository
import name.levis.ichor.data.FleetSources
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.FleetFinding
import name.levis.ichor.model.FleetTone
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.subjectShown
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FleetInputTest {
    private val lab = ContextSummary(name = "admin@lab", fingerprint = "fp-lab", clusterId = "c-lab")
    private val labReader = ContextSummary(name = "reader@lab", fingerprint = "fp-lab-reader", clusterId = "c-lab")
    private val dev = ContextSummary(name = "kind-dev", fingerprint = "fp-dev", kind = KIND_KUBE)
    private val demo = ContextSummary(name = "demo", fingerprint = "fp-demo", demo = true)

    private fun snapshot(context: String, fingerprint: String) = ClusterSnapshot(
        context = context,
        takenAt = 1_000,
        fingerprint = fingerprint,
        nodes = mapOf(
            "10.0.0.1" to NodeState("node-a", NodeHealth.READY),
            "10.0.0.2" to NodeState("node-b", NodeHealth.UNREACHABLE),
        ),
        etcdChecked = true,
        amChecked = true,
        amIssues = mapOf("f1" to "warning|KubePodCrashLooping|shop/checkout"),
    )

    @Test
    fun oneEntryPerClusterFromAnyOfItsContextsSnapshot() {
        val state = MonitorState(
            clusters = mapOf("fp-lab-reader" to snapshot("reader@lab", "fp-lab-reader")),
            reach = mapOf("fp-lab-reader" to Reach(failures = 2)),
        )
        val entries = fleetEntries(state, ConfigSummary("admin@lab", listOf(lab, labReader, dev, demo)), names = emptyMap(), masked = false)

        assertEquals(listOf("c-lab", "fp-dev"), entries.map { it.key })
        val first = entries[0]
        assertEquals("reader@lab", first.context)
        assertEquals(1_000L, first.takenAt)
        assertEquals(2, first.failures)
        assertEquals("node-b", first.nodes.getValue("10.0.0.2").hostname)
        assertTrue(first.etcdChecked && first.amChecked)
        assertEquals("warning|KubePodCrashLooping|shop/checkout", first.amIssues["f1"])
    }

    @Test
    fun aClusterNeverCheckedHasNoTime() {
        val entries = fleetEntries(MonitorState.EMPTY, ConfigSummary("kind-dev", listOf(dev)), names = emptyMap(), masked = false)

        assertEquals(1, entries.size)
        assertEquals(0L, entries[0].takenAt)
        assertTrue(entries[0].kube)
        assertTrue(entries[0].nodes.isEmpty())
    }

    @Test
    fun namesAreTheGivenOnesUnlessMasked() {
        val state = MonitorState(clusters = mapOf("fp-lab" to snapshot("admin@lab", "fp-lab")))
        val summary = ConfigSummary("admin@lab", listOf(lab))
        val names = mapOf("fp-lab" to "Home lab")

        assertEquals("Home lab", fleetEntries(state, summary, names, masked = false)[0].name)
        val masked = fleetEntries(state, summary, names, masked = true)[0]
        assertEquals("admin@lab", masked.name)
        // The snapshot keeps the hostnames as read: none in screenshot mode.
        assertTrue(masked.nodes.values.all { it.hostname.isEmpty() })
    }

    @Test
    fun withoutTheConfigEverySnapshotIsAnEntry() {
        val state = MonitorState(clusters = mapOf("fp-lab" to snapshot("admin@lab", "fp-lab")))
        val entries = fleetEntries(state, summary = null, names = mapOf("fp-lab" to "Home lab"), masked = false)

        assertEquals(listOf("fp-lab"), entries.map { it.key })
        assertEquals("Home lab", entries[0].name)
        assertEquals("admin@lab", entries[0].context)
    }

    @Test
    fun theDemoAloneShowsTheDemoFleet() {
        assertTrue(demoFleetOnly(ConfigSummary("demo", listOf(demo))))
        assertFalse(demoFleetOnly(ConfigSummary("demo", listOf(demo, lab))))
        assertFalse(demoFleetOnly(ConfigSummary("", emptyList())))
        assertFalse(demoFleetOnly(null))
    }

    @Test
    fun theRepositorySendsTheEntriesAndDecodesTheSummary() = runBlocking {
        var sent = ""
        val repository = FleetRepository(
            rank = { entries, _ ->
                sent = entries
                """{"clusters":[{"key":"c-lab","name":"lab","context":"admin@lab","tone":"bad","nodes":2,"ready":1,"unreachable":1,
                   "topFinding":{"track":"nodes","severity":"critical","subject":"node-b unreachable"}}],
                   "counts":{"bad":1,"warn":0,"unknown":0,"ok":0},"worst":"bad","groups":[]}"""
            },
            demo = { error("not the demo") },
        )
        val state = MonitorState(clusters = mapOf("fp-lab" to snapshot("admin@lab", "fp-lab")))
        val summary = repository.summary(FleetSources(state, ConfigSummary("admin@lab", listOf(lab)), emptyMap(), masked = false), now = 2_000)

        val entry = Json.parseToJsonElement(sent).jsonObject.getValue("clusters").jsonArray[0].jsonObject
        assertEquals("1000", entry.getValue("takenAt").jsonPrimitive.content)
        assertEquals("UNREACHABLE", entry.getValue("nodes").jsonObject.getValue("10.0.0.2").jsonObject.getValue("health").jsonPrimitive.content)
        // Defaults written out: the core reads an absent track as unchecked anyway, but the shape stays explicit.
        assertEquals("false", entry.getValue("pluginChecked").jsonPrimitive.content)
        assertEquals(FleetTone.BAD, summary.worst)
        assertEquals("node-b unreachable", summary.clusters[0].topFinding?.subject)
    }

    @Test
    fun theDemoFleetComesFromTheCore() = runBlocking {
        val repository = FleetRepository(
            rank = { _, _ -> error("not ranked") },
            demo = { """{"clusters":[{"key":"demo-lab","name":"lab","context":"lab","tone":"unknown"}],"worst":"unknown"}""" },
        )
        val summary = repository.summary(FleetSources(MonitorState.EMPTY, ConfigSummary("demo", listOf(demo)), emptyMap(), masked = false), now = 0)

        assertEquals(listOf("demo-lab"), summary.clusters.map { it.key })
    }

    @Test
    fun aMaskedRowShowsOnlyTheGenericSubjects() {
        val nodes = FleetFinding(track = "nodes", severity = "critical", subject = "2 of 3 nodes unreachable")
        val alert = FleetFinding(track = "alertmanager", subject = "KubePodCrashLooping · shop/checkout")

        assertTrue(nodes.subjectShown(masked = true))
        assertFalse(alert.subjectShown(masked = true))
        assertTrue(alert.subjectShown(masked = false))
        assertFalse(FleetFinding(track = "etcd").subjectShown(masked = false))
    }
}
