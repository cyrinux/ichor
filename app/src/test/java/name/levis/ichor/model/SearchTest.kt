package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class SearchTest {

    private fun result(cluster: String, name: String, score: Double) =
        SearchResult(cluster = cluster, context = "ctx-$cluster", kind = "pod", name = name, score = score)

    @Test
    fun groupsKeepTheRankersOrderWithTheActiveClusterFirst() {
        val results = listOf(result("b", "b1", 0.9), result("a", "a1", 0.8), result("c", "c1", 0.7), result("b", "b2", 0.6), result("a", "a2", 0.5))

        val groups = results.groupedByCluster(activeCluster = "a")

        assertEquals(listOf("a", "b", "c"), groups.map { it.cluster })
        assertEquals(listOf("a1", "a2"), groups[0].results.map { it.name })
        assertEquals(listOf("b1", "b2"), groups[1].results.map { it.name })
        assertEquals("ctx-b", groups[1].context)
    }

    @Test
    fun anUnknownActiveClusterLeavesTheRankersOrder() {
        val groups = listOf(result("b", "b1", 0.9), result("a", "a1", 0.8)).groupedByCluster(activeCluster = null)
        assertEquals(listOf("b", "a"), groups.map { it.cluster })
    }

    @Test
    fun recentSearchesPutTheLatestFirstWithoutRepeats() {
        val recent = (1..10).map { "q$it" }

        assertEquals(listOf("q3") + recent.filter { it != "q3" }, recent.withRecent(" q3 "))
        assertEquals(listOf("new") + recent.take(9), recent.withRecent("new"))
        // Case does not make a different search.
        assertEquals(listOf("Q1") + recent.drop(1), recent.withRecent("Q1"))
        assertEquals(recent, recent.withRecent("   "))
        assertEquals("two words", recent.withRecent("two\nwords").first())
    }

    @Test
    fun aResultIsStaleAfterFiveMinutes() {
        val now = 10 * 60_000L
        assertEquals(false, SearchResult(cluster = "a", context = "a", kind = "pod", name = "p", at = now - 60_000).isStale(now))
        assertEquals(true, SearchResult(cluster = "a", context = "a", kind = "pod", name = "p", at = now - 6 * 60_000).isStale(now))
        assertEquals(false, SearchResult(cluster = "a", context = "a", kind = "pod", name = "p", at = 0).isStale(now))
    }
}
