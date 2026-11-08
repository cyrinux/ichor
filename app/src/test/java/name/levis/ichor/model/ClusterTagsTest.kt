package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterTagsTest {
    @Test
    fun aTypedLineBecomesCleanTags() {
        assertEquals(listOf("prod", "eu"), normalizeTags("  Prod, eu  PROD,,"))
        assertTrue(normalizeTags(" , ").isEmpty())
    }

    @Test
    fun tagsAreCappedInLengthAndNumber() {
        assertEquals(listOf("a".repeat(CLUSTER_TAG_MAX)), normalizeTags("a".repeat(CLUSTER_TAG_MAX + 5)))
        val many = (1..CLUSTER_TAGS_MAX + 3).joinToString(",") { "t$it" }
        assertEquals(CLUSTER_TAGS_MAX, normalizeTags(many).size)
    }

    @Test
    fun removedClustersForgetTheirTags() {
        val saved = mapOf("fp-a" to listOf("prod"), "fp-gone" to listOf("staging"))
        assertEquals(mapOf("fp-a" to listOf("prod")), keepClusterTags(saved, listOf("fp-a", "")))
    }

    @Test
    fun aClusterHasTheTagsOfAllItsContexts() {
        val saved = mapOf("fp-a" to listOf("prod", "eu"), "fp-b" to listOf("eu", "edge"))
        assertEquals(listOf("prod", "eu", "edge"), clusterTagsOf(saved, listOf("fp-a", "fp-b", "fp-none")))
    }

    @Test
    fun suggestionsAreTheOtherClustersTagsNotYetTyped() {
        val saved = mapOf("fp-a" to listOf("prod"), "fp-b" to listOf("staging", "eu"), "fp-c" to listOf("eu"))
        assertEquals(listOf("staging"), tagSuggestions(saved, own = listOf("fp-a"), typed = "EU"))
        assertEquals(listOf("prod", "staging"), tagSuggestions(saved, own = listOf("fp-c"), typed = "eu"))
    }

    private fun row(key: String) = FleetRow(key = key, name = key, context = key)

    private val rows = listOf(row("a"), row("b"), row("c"))
    private val groups = listOf(
        FleetGroup(tag = "prod", keys = listOf("a")),
        FleetGroup(tag = "eu", keys = listOf("a", "b")),
        FleetGroup(tag = "", keys = listOf("c")),
    )

    @Test
    fun noSelectionShowsEveryRow() {
        assertEquals(rows, fleetRowsWithTags(rows, groups, emptySet()))
    }

    @Test
    fun theSelectionIsAnOrAndKeepsTheRanking() {
        assertEquals(listOf("a", "c"), fleetRowsWithTags(rows, groups, setOf("", "prod")).map { it.key })
    }

    @Test
    fun aTagNoLongerUsedDoesNotHideTheFleet() {
        assertEquals(emptySet<String>(), activeTagFilter(groups, setOf("gone")))
        assertEquals(rows, fleetRowsWithTags(rows, groups, setOf("gone")))
        assertEquals(listOf("a", "b"), fleetRowsWithTags(rows, groups, setOf("gone", "eu")).map { it.key })
    }
}
