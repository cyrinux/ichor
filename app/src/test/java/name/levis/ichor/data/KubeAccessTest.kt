package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.allows
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeAccessTest {

    private val summary = mergeSummaries(
        ConfigSummary(
            "prod",
            listOf(
                ContextSummary("prod", fingerprint = "t1", roles = listOf("os:reader")),
                ContextSummary("lab", fingerprint = "t2", roles = listOf("os:admin")),
                ContextSummary("demo", fingerprint = "d1", demo = true),
            ),
        ),
        ConfigSummary("oidc", listOf(ContextSummary("oidc", fingerprint = "k1", kind = KIND_KUBE))),
    )

    @Test
    fun validLinksGoFromATalosClusterToAKubeconfigCluster() {
        val links = mapOf("t1" to "k1", "t2" to "gone", "k1" to "k1", "d1" to "k1", "removed" to "k1")
        assertEquals(mapOf("t1" to "k1"), validKubeAccess(links, summary))
    }

    @Test
    fun linkedSummarySetsTheKubeAccess() {
        val linked = withKubeAccess(summary, mapOf("t1" to "k1", "t2" to "gone"))

        assertEquals(listOf("k1", "", "", ""), linked.contexts.map { it.kubeAccess })
        // Unlinked again: cleared.
        assertEquals(listOf("", "", "", ""), withKubeAccess(linked, emptyMap()).contexts.map { it.kubeAccess })
    }

    @Test
    fun kubeCallsOfALinkedClusterUseTheKubeconfig() {
        val linked = withKubeAccess(summary, mapOf("t1" to "k1"))
        val servers = mapOf("t1" to "https://talos-lb:6443", "k1" to "https://oidc-lb:6443")

        assertEquals(KubeTarget("kube-yaml", "oidc", "https://oidc-lb:6443"), kubeTarget(StoredConfig("talos-yaml", "kube-yaml", linked, "prod"), servers))
        assertEquals(KubeTarget("talos-yaml", "lab", ""), kubeTarget(StoredConfig("talos-yaml", "kube-yaml", linked, "lab"), servers))
        assertEquals(KubeTarget("kube-yaml", "oidc", "https://oidc-lb:6443"), kubeTarget(StoredConfig("talos-yaml", "kube-yaml", linked, "oidc"), servers))
    }

    @Test
    fun unlinkedClusterKeepsItsOwnTarget() {
        val servers = mapOf("t1" to "https://talos-lb:6443")
        assertEquals(KubeTarget("talos-yaml", "prod", "https://talos-lb:6443"), kubeTarget(StoredConfig("talos-yaml", "kube-yaml", summary, "prod"), servers))
    }

    @Test
    fun linkedClusterAllowsKubernetesWithoutAdmin() {
        val reader = withKubeAccess(summary, mapOf("t1" to "k1")).contexts.first()

        assertTrue(reader.allows(Feature.WORKLOADS))
        assertFalse(reader.allows(Feature.KUBECONFIG))
        assertFalse(reader.allows(Feature.POWER))
        assertFalse(summary.contexts.first().allows(Feature.WORKLOADS))
    }
}
