package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.ShareTarget
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class SearchRepositoryTest {

    @get:Rule
    val folder = TemporaryFolder()

    private class PlainSealer : OfflineCache.Sealer {
        override fun write(file: File, bytes: ByteArray) = file.writeBytes(bytes)
        override fun read(file: File): ByteArray? = if (file.exists()) file.readBytes() else null
        override fun forget() = Unit
    }

    private val now = System.currentTimeMillis()
    private var offlineOn = true
    private val offline by lazy { OfflineCache(File(folder.root, "offline"), PlainSealer(), enabled = { offlineOn }) }
    private val results by lazy { ResultCache(offline = null, scope = { "1|alpha|" }, cluster = { null }) }

    private val alpha = ContextSummary(name = "alpha", fingerprint = "fp-alpha", clusterId = "aaaaaaaaaaaaaaaa")
    private val beta = ContextSummary(name = "beta", fingerprint = "fp-beta", clusterId = "bbbbbbbbbbbbbbbb")
    private val noId = ContextSummary(name = "gamma", fingerprint = "fp-gamma")
    private val demo = ContextSummary(name = "Demo cluster", fingerprint = "fp-demo", clusterId = "dddddddddddddddd", demo = true)

    /** The query and entries the ranker was given, last call first. */
    private var ranked: Pair<String, String>? = null

    private fun repository(contexts: List<ContextSummary>, active: String = "alpha", demoEntries: String = """{"entries":[]}""") =
        SearchRepository(
            summary = { ConfigSummary(active, contexts) },
            active = { active },
            offline = offline,
            offlineEnabled = { offlineOn },
            results = results,
            demoEntries = { demoEntries },
            rank = { entries, query, _ ->
                ranked = entries to query
                """{"results":[{"cluster":"bbbbbbbbbbbbbbbb","context":"beta","kind":"pod","namespace":"web","name":"shop-api-1",""" +
                    """"detail":"Pod · Running","at":5,"score":0.8,"target":{"cluster":"bbbbbbbbbbbbbbbb","target":"pod","ns":"web","name":"shop-api-1"}},""" +
                    """{"cluster":"aaaaaaaaaaaaaaaa","context":"alpha","kind":"node","name":"cp-1","detail":"Node","at":4,"score":0.6}],"total":7}"""
            },
        )

    private fun entries(json: String) = Json.parseToJsonElement(json).jsonObject["entries"]!!.jsonArray.map { it.jsonObject }

    @Test
    fun entriesComeFromEveryClustersLastKnownStateAndTheActiveClustersMemory() = runBlocking {
        offline.save("fp-beta", "pods|*", """[{"namespace":"web","name":"shop-api-1"}]""", now, epoch = 0)
        offline.save("fp-beta", "services|10.0.0.1", "[]", now, epoch = 0)
        offline.save("fp-gamma", "overview", """{"context":"gamma","nodes":[]}""", now, epoch = 0)
        results.remember(PODS + "|web") { listOf(KubePod(namespace = "web", name = "cart-1")) }

        val snapshot = repository(listOf(alpha, beta, noId)).snapshot()

        val got = entries(snapshot.entries).map { Triple(it["cluster"]!!.jsonPrimitive.content, it["context"]!!.jsonPrimitive.content, it["key"]!!.jsonPrimitive.content) }
        assertEquals(
            listOf(
                Triple("aaaaaaaaaaaaaaaa", "alpha", "pods|web"),
                Triple("bbbbbbbbbbbbbbbb", "beta", "pods|*"),
                // No cluster id: its fingerprint stands in (its results carry no share target).
                Triple("fp-gamma", "gamma", "overview"),
            ),
            got,
        )
        // The active cluster's results in memory are re-encoded as the cache would keep them.
        assertTrue("cart-1" in entries(snapshot.entries)[0]["json"]!!.jsonPrimitive.content)
        assertFalse(snapshot.othersNeedOfflineCache)
        assertFalse(snapshot.isEmpty)
    }

    @Test
    fun theWholeListHidesTheNamespacesOnesOfTheSameKind() = runBlocking {
        offline.save("fp-beta", "pods|*", "[]", now, epoch = 0)
        offline.save("fp-beta", "pods|web", "[]", now, epoch = 0)
        offline.save("fp-beta", "workloads|web", "[]", now, epoch = 0)

        val keys = entries(repository(listOf(beta), active = "beta").snapshot().entries).map { it["key"]!!.jsonPrimitive.content }

        assertEquals(listOf("pods|*", "workloads|web"), keys)
    }

    @Test
    fun aRepositorysWholeListHidesTheNamespacesOnes() = runBlocking {
        offline.save("fp-beta", "cronjobs", "[]", now, epoch = 0)
        offline.save("fp-beta", "cronjobs|web", "[]", now, epoch = 0)
        offline.save("fp-beta", "overview", "{}", now, epoch = 0)

        val keys = entries(repository(listOf(beta), active = "beta").snapshot().entries).map { it["key"]!!.jsonPrimitive.content }

        assertEquals(listOf("cronjobs", "overview"), keys)
    }

    @Test
    fun aClusterWithTwoContextsIsSearchedOnceThroughTheActiveOne() = runBlocking {
        val readOnly = beta.copy(name = "beta-ro", fingerprint = "fp-beta-ro")
        offline.save("fp-beta", "pods|*", "[]", now, epoch = 0)
        offline.save("fp-beta-ro", "pods|*", "[]", now, epoch = 0)

        val contexts = entries(repository(listOf(beta, readOnly), active = "beta-ro").snapshot().entries).map { it["context"]!!.jsonPrimitive.content }

        assertEquals(listOf("beta-ro"), contexts)
    }

    @Test
    fun theDemoSearchesItsBuiltInEntriesNotItsMemory() = runBlocking {
        results.remember(PODS + "|*") { listOf(KubePod(namespace = "web", name = "cart-1")) }
        val builtIn = """{"entries":[{"cluster":"dddddddddddddddd","context":"Talos demo","key":"pods","at":1,"json":"{}"}]}"""

        val snapshot = repository(listOf(demo.copy(name = "alpha")), demoEntries = builtIn).snapshot()

        val only = entries(snapshot.entries).single()
        assertEquals("pods", only["key"]!!.jsonPrimitive.content)
        assertEquals("alpha", only["context"]!!.jsonPrimitive.content)
        assertEquals("dddddddddddddddd", only["cluster"]!!.jsonPrimitive.content)
    }

    @Test
    fun withoutLastKnownStateOnlyTheActiveClusterIsSearched() = runBlocking {
        offline.save("fp-beta", "pods|*", "[]", now, epoch = 0)
        offlineOn = false

        val snapshot = repository(listOf(alpha, beta)).snapshot()

        assertTrue(snapshot.isEmpty)
        assertTrue(snapshot.othersNeedOfflineCache)
    }

    @Test
    fun freshListsReplaceTheCachedOnes() = runBlocking {
        results.remember(PODS + "|*") { listOf(KubePod(namespace = "web", name = "old-1")) }

        val snapshot = repository(listOf(alpha)).snapshot(fresh = mapOf(PODS + "|*" to listOf(KubePod(namespace = "web", name = "new-1"))))

        val json = entries(snapshot.entries).single()["json"]!!.jsonPrimitive.content
        assertTrue("new-1" in json)
        assertFalse("old-1" in json)
    }

    @Test
    fun searchDecodesTheRankersResults() = runBlocking {
        val repository = repository(listOf(alpha, beta))
        val snapshot = repository.snapshot()

        val got = repository.search(snapshot, "  shop api ")

        assertEquals("shop api", ranked?.second)
        assertEquals(7, got.total)
        assertEquals(ShareTarget.pod("web", "shop-api-1").copy(cluster = "bbbbbbbbbbbbbbbb"), got.results[0].target)
        assertEquals(null, got.results[1].target)
        assertEquals("cp-1", got.results[1].name)
    }

    @Test
    fun aBlankQueryDoesNotRank() = runBlocking {
        val got = repository(listOf(alpha)).search(SearchSnapshot.EMPTY, "   ")
        assertEquals(0, got.total)
        assertEquals(null, ranked)
    }
}
