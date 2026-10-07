package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NeedsEndpointTest {

    @Test
    fun talosContextWithoutEndpointsNeedsOne() {
        // The core sends `null` for a context generated without endpoints.
        val summary = TalosJson.decodeFromString<ContextSummary>("""{"name":"lab","endpoints":null,"nodes":null}""")
        assertTrue(summary.needsEndpoint)
    }

    @Test
    fun othersDoNot() {
        assertFalse(ContextSummary("lab", endpoints = listOf("10.0.0.1")).needsEndpoint)
        assertFalse(ContextSummary("demo", demo = true).needsEndpoint)
        assertFalse(ContextSummary("kube", kind = KIND_KUBE).needsEndpoint)
    }
}
