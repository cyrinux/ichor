package name.levis.talosmobile.shortcuts

import name.levis.talosmobile.model.ClusterLabels
import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.model.ContextSummary
import name.levis.talosmobile.model.DEFAULT_CLUSTER_SEED
import org.junit.Assert.assertEquals
import org.junit.Test

class ShortcutSpecTest {

    private val summary = ConfigSummary(
        current = "admin@prod",
        contexts = listOf(
            ContextSummary(name = "admin@prod", fingerprint = "fp-prod"),
            ContextSummary(name = "admin@legacy"),
            ContextSummary(name = "admin@lab", fingerprint = "fp-lab"),
            ContextSummary(name = "admin@edge", fingerprint = "fp-edge"),
        ),
    )

    @Test
    fun oneShortcutPerClusterInConfigOrder() {
        val specs = clusterShortcuts(summary, ClusterLabels(mapOf("fp-lab" to "Homelab")), mapOf("fp-prod" to 0x112233), max = 10)
        assertEquals(listOf("cluster-fp-prod", "cluster-fp-lab", "cluster-fp-edge"), specs.map { it.id })
        assertEquals(listOf("admin@prod", "Homelab", "admin@edge"), specs.map { it.label })
        assertEquals(listOf(0, 1, 2), specs.map { it.rank })
        assertEquals(0x112233, specs[0].color)
        assertEquals(DEFAULT_CLUSTER_SEED, specs[1].color)
    }

    @Test
    fun capsAtWhatTheLauncherShows() {
        assertEquals(2, clusterShortcuts(summary, ClusterLabels(), emptyMap(), max = 2).size)
        assertEquals(0, clusterShortcuts(summary, ClusterLabels(), emptyMap(), max = -1).size)
    }

    @Test
    fun idsCoverClustersPastTheCap() {
        assertEquals(setOf("cluster-fp-prod", "cluster-fp-lab", "cluster-fp-edge"), clusterShortcutIds(summary))
    }

    @Test
    fun screenshotModeLabelsWithMaskedNames() {
        val specs = clusterShortcuts(summary, ClusterLabels(mapOf("fp-lab" to "Homelab"), masked = true), emptyMap(), max = 10)
        assertEquals("admin@lab", specs[1].label)
    }

    @Test
    fun initialIsTheClusterPartsFirstLetter() {
        assertEquals("P", shortcutInitial("admin@prod"))
        assertEquals("H", shortcutInitial("Homelab"))
        assertEquals("3", shortcutInitial("@ 3rd"))
        assertEquals("A", shortcutInitial("admin@"))
        assertEquals("T", shortcutInitial("@@"))
    }
}
