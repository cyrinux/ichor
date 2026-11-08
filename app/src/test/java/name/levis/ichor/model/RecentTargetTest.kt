package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RecentTargetTest {
    private val cluster = "aaaaaaaaaaaaaaaa"

    private fun node(host: String, tab: Int = 0, at: Long = 0) =
        RecentTarget(ShareTarget.node("10.0.0.$host", "node-$host", tab).copy(cluster = cluster), "node-$host", at = at)

    @Test
    fun newestFirstWithoutDuplicates() {
        var recent = emptyList<RecentTarget>()
        recent = RecentTargets.adding(recent, node("1", at = 1))
        recent = RecentTargets.adding(recent, node("2", at = 2))
        // The same node opened on another tab is the same object.
        recent = RecentTargets.adding(recent, node("1", tab = 2, at = 3))
        assertEquals(listOf("node-1", "node-2"), recent.map { it.label })
        assertEquals(3L, recent.first().at)
    }

    @Test
    fun keepsTwenty() {
        var recent = emptyList<RecentTarget>()
        (1..25).forEach { recent = RecentTargets.adding(recent, node("$it", at = it.toLong())) }
        assertEquals(RecentTargets.MAX, recent.size)
        assertEquals("node-25", recent.first().label)
        assertEquals("node-6", recent.last().label)
    }

    @Test
    fun recordsOnlyObjectsOfAKnownCluster() {
        val screen = RecentTarget(ShareTarget.screen(ShareTarget.ETCD).copy(cluster = cluster), "etcd")
        val noCluster = RecentTarget(ShareTarget.argoApp("argocd", "web"), "web")
        val unnamed = RecentTarget(ShareTarget.pod("default", "api-0").copy(cluster = cluster), " ")
        listOf(screen, noCluster, unnamed).forEach { assertTrue(RecentTargets.adding(emptyList(), it).isEmpty()) }
    }

    @Test
    fun removingAndRemovedClusters() {
        val other = RecentTarget(ShareTarget.fluxApp("Kustomization", "flux-system", "apps").copy(cluster = "bbbbbbbbbbbbbbbb"), "apps")
        val recent = listOf(node("1"), other)
        assertEquals(listOf("apps"), RecentTargets.removing(recent, node("1", tab = 3).target).map { it.label })
        assertEquals(listOf("node-1"), RecentTargets.known(recent, setOf(cluster)).map { it.label })
    }

    @Test
    fun roundTripsAndDropsWhatItCannotRead() {
        val recent = listOf(node("1", at = 5), RecentTarget(ShareTarget.workload("Deployment", "default", "api").copy(cluster = cluster), "api", "default", 4))
        assertEquals(recent, RecentTargets.parse(RecentTargets.encode(recent)))
        assertTrue(RecentTargets.parse("not json").isEmpty())
        assertTrue(RecentTargets.parse(null).isEmpty())
    }

    @Test
    fun aFocusedKubernetesScreenNamesItsObject() {
        assertEquals(ShareTarget.workload("Deployment", "default", "api"), KubeFocus(0, "Deployment/default/api", "default", "api").shareTarget())
        assertEquals(ShareTarget.pod("default", "api-0"), KubeFocus(1, "default/api-0", "default", "api-0").shareTarget())
        assertEquals(ShareTarget.cronJob("ops", "backup"), KubeFocus(2, "ops/backup", "ops", "backup").shareTarget())
        assertNull(KubeFocus(0).shareTarget())
    }
}
