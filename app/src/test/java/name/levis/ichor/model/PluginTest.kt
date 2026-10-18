package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The render tree as the Go core writes it: the goldens of go/plugin/testdata/render, shared
 * with the Go tests (and the iOS ones), decode into [PluginTree] with every primitive.
 */
class PluginTest {
    private val goldens = File("../go/plugin/testdata/render")

    private fun golden(name: String): String = File(goldens, name).readText()

    private fun decode(json: String): PluginTree = TalosJson.decodeFromString(PluginTree.serializer(), json)

    private fun sections(tree: PluginTree): List<PluginSection> = tree.cards.flatMap { c -> c.rows.flatMap { it.detail } }

    @Test
    fun everyGoldenDecodes() {
        val files = goldens.listFiles { f -> f.name.endsWith(".json") }.orEmpty()
        assertTrue("no goldens in $goldens", files.isNotEmpty())
        files.forEach { f ->
            val tree = decode(f.readText())
            assertTrue(f.name, tree.plugin.id.isNotEmpty())
            assertTrue(f.name, tree.cards.isNotEmpty())
            assertFalse(f.name, sections(tree).any { it is PluginSection.Unknown })
        }
    }

    @Test
    fun kubeGreenSamples() {
        val tree = decode(golden("kube-green-samples.json"))
        assertEquals("kube-green", tree.plugin.id)
        assertEquals(PLUGIN_CATEGORY_DATA_SERVICES, tree.plugin.category)
        assertTrue(tree.active)
        assertEquals(ServiceHealth.WARNING, tree.serviceHealth)
        assertEquals("2 sleep schedules · 1 asleep · 1 warnings", tree.summary.line())

        val list = tree.cards.first { it.id == "list" }
        assertTrue(list.groupsByNamespace())
        val weekend = list.rows.first { it.title == "weekend" }
        assertEquals("Asleep", weekend.status?.text)
        assertEquals(ServiceHealth.WARNING, weekend.serviceHealth)
        assertEquals("sleepinfos", weekend.ref?.resource)

        val kv = weekend.detail.filterIsInstance<PluginSection.KeyValues>().single()
        assertEquals("Weekdays", kv.items.first().label)
        assertEquals("", kv.items.first { it.label == "Wake up at" }.value)
        val badges = weekend.detail.filterIsInstance<PluginSection.Badges>().single()
        assertEquals(listOf("ok", "ok", "idle"), badges.items.map { it.tone })
        val secret = weekend.detail.filterIsInstance<PluginSection.Relations>().single().items.single().ref
        assertNotNull(secret)
        assertEquals("secrets", secret!!.resource)
        assertEquals(KubeObjectRef("", "v1", "secrets", "Secret", "demo-lab", "sleepinfo-weekend", editable = false), secret.objectRef("Secret"))

        val wake = weekend.detail.filterIsInstance<PluginSection.Actions>().single().items.single()
        assertTrue(wake.enabled)
        assertTrue(wake.usable)
        assertNull(wake.allowed)
        val nightly = list.rows.first { it.title == "nightly" }
        val asleepOnly = nightly.detail.filterIsInstance<PluginSection.Actions>().single().items.single()
        assertFalse(asleepOnly.usable)
        assertEquals("not asleep", asleepOnly.disabledReason)

        // A data service's rows: no object, no health, never grouped.
        val namespaces = tree.cards.first { it.id == "namespaces" }
        assertTrue(namespaces.rows.all { it.ref == null && it.health == null })
        assertFalse(namespaces.groupsByNamespace())
        assertEquals(1, tree.findings.size)
    }

    @Test
    fun clusterTreeCarriesItsErrors() {
        val tree = decode(golden("kube-green-cluster.json"))
        assertEquals("api", tree.detectedVia)
        val error = tree.errors.single()
        assertEquals("secrets", error.subject)
        assertTrue(error.message.contains("forbidden"))
    }

    @Test
    fun widgetsEveryPrimitive() {
        val tree = decode(golden("widgets-fr.json"))
        val all = sections(tree)
        val gauge = all.filterIsInstance<PluginSection.Gauge>().first { it.unit == "bytes" }
        assertEquals("Utilisé", gauge.label)
        assertEquals(0f, gauge.fraction)
        assertEquals("0 B / 512 MiB", gauge.text)
        val replicas = all.filterIsInstance<PluginSection.Gauge>().first { it.label == "Replicas" && it.value == 1.0 }
        assertEquals(0.1f, replicas.fraction!!, 0.0001f)

        val text = all.filterIsInstance<PluginSection.Text>().first()
        assertTrue(text.mono)
        assertEquals("info", text.tone)

        val condition = all.filterIsInstance<PluginSection.Conditions>().first().items.first()
        assertEquals("Ready", condition.type)
        assertEquals(ServiceHealth.CRITICAL, pluginHealth(condition.tone))

        val table = all.filterIsInstance<PluginSection.Table>().first()
        assertEquals(listOf("Namespace", "Widgets"), table.columns.map { it.label })
        assertEquals(listOf("demo-lab", "1"), table.rows.first())

        val relation = all.filterIsInstance<PluginSection.Relations>().first().items.first()
        assertEquals("HUB", relation.text)
        assertEquals("", relation.ref?.namespace)

        val kv = all.filterIsInstance<PluginSection.KeyValues>().first().items
        assertEquals("", kv.first().value)
        assertTrue(kv.last().mono)

        assertEquals(listOf("gadgets", "fields.widget.broken"), tree.errors.map { it.subject })
        assertEquals("workloads", tree.plugin.category)
    }

    @Test
    fun anUnknownSectionTypeIsSkippedNotFatal() {
        val json = golden("kube-green-samples.json").replaceFirst(
            "\"detail\": [",
            "\"detail\": [{\"type\": \"sparkline\", \"points\": [1, 2, 3], \"label\": \"Later\"}, {\"items\": []},",
        ).replaceFirst("\"active\": true,", "\"active\": true, \"somethingNew\": {\"x\": 1},")
        val tree = decode(json)
        val weekend = tree.cards.first().rows.first { it.title == "weekend" }
        assertEquals(PluginSection.Unknown("sparkline"), weekend.detail[0])
        assertEquals(PluginSection.Unknown(""), weekend.detail[1])
        // The rest still renders.
        assertEquals(6, weekend.detail.size)
        assertTrue(weekend.detail[2] is PluginSection.KeyValues)
    }

    @Test
    fun aBrokenSectionIsSkippedNotFatal() {
        // A known type whose field has the wrong shape, a wrongly typed scalar, and entries that are not objects.
        val json = golden("kube-green-samples.json").replaceFirst(
            "\"detail\": [",
            "\"detail\": [{\"type\": \"keyValues\", \"items\": \"oops\"}, {\"type\": \"gauge\", \"value\": {\"a\": 1}}, 42, \"text\", [1],",
        )
        val weekend = decode(json).cards.first().rows.first { it.title == "weekend" }
        assertEquals(PluginSection.Unknown("keyValues"), weekend.detail[0])
        assertEquals(PluginSection.Unknown("gauge"), weekend.detail[1])
        (2..4).forEach { assertEquals(PluginSection.Unknown(), weekend.detail[it]) }
        // The rest still renders.
        assertEquals(9, weekend.detail.size)
        assertTrue(weekend.detail[5] is PluginSection.KeyValues)
        assertTrue(weekend.detail.last() is PluginSection.Actions)
    }

    @Test
    fun tonesMapToServiceHealth() {
        assertEquals(ServiceHealth.OK, pluginHealth("ok"))
        assertEquals(ServiceHealth.IDLE, pluginHealth("idle"))
        assertEquals(ServiceHealth.IDLE, pluginHealth("info"))
        assertEquals(ServiceHealth.WARNING, pluginHealth("warning"))
        assertEquals(ServiceHealth.CRITICAL, pluginHealth("critical"))
        assertEquals(ServiceHealth.UNKNOWN, pluginHealth("unknown"))
        assertEquals(ServiceHealth.UNKNOWN, pluginHealth("purple"))
        // Without a status, the row's health gives its tone.
        assertEquals(ServiceHealth.CRITICAL, PluginRow(health = PluginHealth("critical")).serviceHealth)
    }

    @Test
    fun detectSplitsDataServicesFromHomeTiles() {
        val detect = TalosJson.decodeFromString(
            PluginDetect.serializer(),
            """
            {"items": [
              {"id": "kube-green", "name": "kube-green", "icon": "kube-green", "category": "data-services", "screen": "", "active": true, "detectedVia": "demo",
               "summary": {"title": "2 sleep schedules", "health": "warning", "partial": false, "badges": []}},
              {"id": "demo-widgets", "name": "Demo Widgets", "category": "workloads", "active": true, "summary": null},
              {"id": "demo-off", "name": "Off", "category": "networking", "active": false, "summary": null},
              {"id": "demo-native", "name": "Native", "category": "data-services", "screen": "native:longhorn", "active": true}
            ]}
            """.trimIndent(),
        )
        assertEquals(listOf("kube-green", "demo-native"), detect.dataServicePlugins.map { it.id })
        assertEquals(listOf("demo-widgets"), detect.homeTiles.map { it.id })
        assertEquals(ServiceHealth.WARNING, detect.items.first().summary?.serviceHealth)
        assertEquals("longhorn", detect.items.last().nativeScreen)
        assertNull(detect.items.first().nativeScreen)
    }

    @Test
    fun nativeScreens() {
        assertEquals("argocd", nativePluginScreen("native:argocd"))
        assertEquals("argocd", nativePluginScreen(" native: argocd "))
        assertNull(nativePluginScreen(""))
        assertNull(nativePluginScreen("native:"))
        assertNull(nativePluginScreen("argocd"))
    }

    @Test
    fun catalogNamesTheKinds() {
        val catalog = TalosJson.decodeFromString(
            PluginCatalog.serializer(),
            """{"items": [{"id": "kube-green", "name": "kube-green", "category": "data-services", "screen": "", "version": "ichor.dev/plugin/v1alpha1",
               "resources": [{"group": "kube-green.com", "version": "v1alpha1", "resource": "sleepinfos", "kind": "SleepInfo"},
                             {"group": "", "version": "v1", "resource": "secrets", "kind": "Secret"}],
               "actions": [{"id": "wakeNow", "label": "Wake up now", "verb": "mergePatch"}]}]}""",
        )
        assertEquals("Secret", catalog.kindOf("kube-green", PluginRef("", "v1", "secrets", "demo-lab", "x")))
        assertEquals("SleepInfo", catalog.kindOf("kube-green", PluginRef("kube-green.com", "v1alpha1", "sleepinfos", "demo-lab", "x")))
        assertEquals("", catalog.kindOf("kube-green", PluginRef("", "v1", "pods", "demo-lab", "x")))
        assertEquals("", catalog.kindOf("other", PluginRef("", "v1", "secrets", "demo-lab", "x")))
    }

    @Test
    fun aGaugeWithoutMaximumHasNoBar() {
        assertNull(PluginSection.Gauge(value = 3.0).fraction)
        assertNull(PluginSection.Gauge(value = 3.0, max = 0.0).fraction)
        assertEquals(1f, PluginSection.Gauge(value = 30.0, max = 10.0).fraction)
    }
}
