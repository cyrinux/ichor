package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterNameTest {

    private val prod = ContextSummary(name = "admin@talos-prod-eu-west-1", fingerprint = "fp-prod")
    private val lab = ContextSummary(name = "admin@lab", fingerprint = "fp-lab")

    @Test
    fun givenNameReplacesTheContextName() {
        val labels = ClusterLabels(mapOf("fp-prod" to "Prod"))
        assertEquals("Prod", labels.of(prod))
        assertEquals("admin@lab", labels.of(lab))
    }

    @Test
    fun screenshotModeShowsTheMaskedContextName() {
        val labels = ClusterLabels(mapOf("fp-prod" to "Prod"), masked = true)
        assertEquals("admin@talos-prod-eu-west-1", labels.of(prod))
        assertNull(labels.given(prod))
    }

    @Test
    fun noFingerprintNoGivenName() {
        val anonymous = ContextSummary(name = "admin@old")
        assertEquals("admin@old", ClusterLabels(mapOf("" to "Old")).of(anonymous))
    }

    @Test
    fun typedNamesAreTrimmedAndBlankResets() {
        assertEquals("Prod", normalizeClusterName("  Prod "))
        assertNull(normalizeClusterName("   "))
        assertEquals(CLUSTER_NAME_MAX, normalizeClusterName("x".repeat(100))!!.length)
    }

    @Test
    fun namesOfRemovedClustersAreForgotten() {
        val saved = mapOf("fp-prod" to "Prod", "fp-gone" to "Gone")
        assertEquals(mapOf("fp-prod" to "Prod"), keepClusterNames(saved, listOf("fp-prod", "fp-lab")))
    }
}
