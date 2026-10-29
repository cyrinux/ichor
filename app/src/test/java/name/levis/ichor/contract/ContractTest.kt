package name.levis.ichor.contract

import java.io.File
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.ClusterUpdates
import name.levis.ichor.model.updateSections
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.KubeCronJobList
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubePodPage
import name.levis.ichor.model.KubeSpanOverview
import name.levis.ichor.model.KubeWorkloadList
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The facade contract (CYR-440): each golden fixture Go writes (go/ichorgo/testdata/contract,
 * `just contract-update`) is decoded as the app decodes it, then every key Go wrote must be one
 * the model reads. A renamed Go json tag shows up here as an unread key, instead of a silent
 * default in the app. [Row.ignored] lists the keys the app does not read on purpose.
 */
class ContractTest {
    private class Row<T>(val serializer: KSerializer<T>, val ignored: Set<String> = emptySet(), val check: (T) -> Unit = {})

    private val rows: Map<String, Row<*>> = mapOf(
        "cluster-overview" to Row(ClusterOverview.serializer()) { assertEquals("demo", it.context); assertTrue(it.nodes.isNotEmpty()) },
        "kube-pods" to Row(KubePodPage.serializer()) { assertTrue(it.pods.first().name.isNotEmpty()) },
        "kube-workloads" to Row(KubeWorkloadList.serializer()) { assertTrue(it.workloads.first().name.isNotEmpty()) },
        "kube-cronjobs" to Row(KubeCronJobList.serializer()) { assertTrue(it.cronJobs.first().name.isNotEmpty()) },
        "kube-nodes" to Row(KubeNodesOverview.serializer()) { assertTrue(it.nodes.isNotEmpty()) },
        "argocd" to Row(ArgoStatus.serializer()) { assertTrue(it.installed && it.apps.isNotEmpty()) },
        "flux" to Row(FluxStatus.serializer()) { assertTrue(it.installed) },
        "alerts" to Row(AmAlerts.serializer()) { assertTrue(it.groups.isNotEmpty()) },
        "checkup" to Row(CheckupReport.serializer()),
        "inventory" to Row(Inventory.serializer()),
        "kubespan" to Row(KubeSpanOverview.serializer()) { assertTrue(it.nodes.isNotEmpty()) },
        "topology" to Row(ClusterTopology.serializer()),
        "cluster-updates" to Row(ClusterUpdates.serializer()) { assertTrue(updateSections(it).images.isNotEmpty()) },
    )

    /** Every default written out, so the re-encoded object names every key the model reads. */
    private val everyKey = Json(TalosJson) { encodeDefaults = true; explicitNulls = true }

    private val dir: File = listOf("../go/ichorgo/testdata/contract", "go/ichorgo/testdata/contract").map(::File).first { it.isDirectory }

    @Test
    fun everyFixtureHasARow() {
        val fixtures = dir.listFiles { f -> f.extension == "json" }.orEmpty().map { it.nameWithoutExtension }.toSet()
        assertEquals("fixtures without a row here (or rows without a fixture)", fixtures, rows.keys)
    }

    @Test
    fun everyKeyGoWritesIsOneTheModelReads() {
        val problems = rows.flatMap { (name, row) -> unread(name, row) }
        assertTrue("keys Go writes that the Kotlin models do not read:\n" + problems.joinToString("\n"), problems.isEmpty())
    }

    private fun <T> unread(name: String, row: Row<T>): List<String> {
        val text = File(dir, "$name.json").readText()
        val value = TalosJson.decodeFromString(row.serializer, text)
        row.check(value)
        val fixture = TalosJson.parseToJsonElement(text)
        val model = everyKey.encodeToJsonElement(row.serializer, value)
        return missing(fixture, model, "").filter { path -> row.ignored.none { path == it || path.startsWith("$it.") } }.map { "$name: $it" }
    }

    /** The paths of [fixture]'s keys that [model] lacks (arrays: their first item). */
    private fun missing(fixture: JsonElement, model: JsonElement?, path: String): List<String> = when (fixture) {
        is JsonObject -> fixture.flatMap { (key, child) ->
            val at = if (path.isEmpty()) key else "$path.$key"
            val modelObject = model as? JsonObject
            if (modelObject == null || key !in modelObject) listOf(at) else missing(child, modelObject[key], at)
        }
        is JsonArray -> fixture.firstOrNull()?.let { first -> missing(first, (model as? JsonArray)?.firstOrNull(), "$path[]") }.orEmpty()
        else -> emptyList()
    }
}
