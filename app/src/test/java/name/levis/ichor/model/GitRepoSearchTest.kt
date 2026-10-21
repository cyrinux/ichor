package name.levis.ichor.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class GitRepoSearchTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun decodesTheGoAnswer() {
        val search = json.decodeFromString(
            GitRepoSearch.serializer(),
            """{"sha":"abc","hits":[{"path":"talos/nodes/cp-1.yaml","line":3,"text":"hostname: cp-1"}],"searched":9,"truncated":true}""",
        )
        assertEquals(GitSearchHit("talos/nodes/cp-1.yaml", 3, "hostname: cp-1"), search.hits.single())
        assertEquals(9, search.searched)
        assertTrue(search.truncated)
    }

    @Test
    fun groupsHitsByFileInOrder() {
        val hits = listOf(GitSearchHit("b.yaml", 4, "x"), GitSearchHit("a.yaml", 9, "y"), GitSearchHit("b.yaml", 1, "z"), GitSearchHit("a.yaml", 2, "w"))
        val groups = groupSearchHits(hits)
        assertEquals(listOf("a.yaml", "b.yaml"), groups.map { it.first })
        assertEquals(listOf(2, 9), groups[0].second.map { it.line })
        assertEquals(listOf(1, 4), groups[1].second.map { it.line })
    }
}
