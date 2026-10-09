package name.levis.ichor.model

import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ActivityTest {

    private val entries = listOf(
        ActivityEntry(at = 3, cluster = "prod", namespace = "web", obj = "Deployment/api", action = "scale", params = "replicas=3"),
        ActivityEntry(at = 2, cluster = "lab", node = "10.0.0.2", action = "reboot", outcome = "failed", error = "unreachable"),
        ActivityEntry(at = 1, cluster = "prod", node = "10.0.0.3", action = "rollout-restart"),
    )

    @Test
    fun decodesTheCoreJson() {
        val json = """[{"at":1,"cluster":"prod","namespace":"web","object":"Pod/api-1","action":"delete-pod","outcome":"failed","error":"forbidden","demo":true}]"""
        val e = TalosJson.decodeFromString(ListSerializer(ActivityEntry.serializer()), json).single()
        assertEquals("Pod/api-1", e.obj)
        assertTrue(e.failed)
        assertTrue(e.demo)
        assertEquals("web/Pod/api-1", e.target)
    }

    @Test
    fun labelsTheActionKey() {
        assertEquals("Rollout restart", entries[2].actionLabel)
        assertEquals("Scale", entries[0].actionLabel)
    }

    @Test
    fun filtersByClusterActionAndFailure() {
        assertEquals(3, entries.count { it.matches(ActivityFilter()) })
        assertEquals(listOf(3L, 1L), entries.filter { it.matches(ActivityFilter(cluster = "prod")) }.map { it.at })
        assertEquals(listOf(1L), entries.filter { it.matches(ActivityFilter(cluster = "prod", action = "rollout-restart")) }.map { it.at })
        assertEquals(listOf(2L), entries.filter { it.matches(ActivityFilter(failedOnly = true)) }.map { it.at })
        assertFalse(entries[0].matches(ActivityFilter(cluster = "lab")))
    }

    @Test
    fun distinctValuesKeepTheNewestFirst() {
        assertEquals(listOf("prod", "lab"), entries.distinctOf { it.cluster })
        assertEquals(listOf("scale", "reboot", "rollout-restart"), entries.distinctOf { it.action })
    }
}
