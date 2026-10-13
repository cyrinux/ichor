package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ResultCacheTest {
    private var generation = 0
    private val cache = ResultCache(offline = null, scope = { "$generation|ctx|" }, cluster = { null })

    @Test
    fun remembersTheLastResultOfItsScope() = runBlocking {
        assertNull(cache.cached<String>("k"))
        assertEquals("v", cache.remember("k") { last -> assertNull(last()); "v" })
        assertEquals("v", cache.cached<String>("k")?.value)
        cache.remember("k") { last -> assertEquals("v", last()?.value); "w" }
        assertEquals("w", cache.cached<String>("k")?.value)

        // Importing or switching cluster changes the scope: nothing of the previous one shows.
        generation++
        assertNull(cache.cached<String>("k"))
    }

    @Test
    fun keeperStoresForTheScopeTheLoadStartedIn() = runBlocking {
        val keep = cache.keeper("list")
        generation++
        keep(listOf(1, 2))
        assertNull(cache.cached<List<Int>>("list"))
        generation--
        assertEquals(listOf(1, 2), cache.cached<List<Int>>("list")?.value)
    }

    @Test
    fun theLastKnownOverviewIsThereBeforeTheClusterWasRestored() = runBlocking {
        val sealer = object : OfflineCache.Sealer {
            override fun write(file: java.io.File, bytes: ByteArray) = file.writeBytes(bytes)
            override fun read(file: java.io.File): ByteArray? = if (file.exists()) file.readBytes() else null
            override fun forget() = Unit
        }
        val dir = kotlin.io.path.createTempDirectory("offline").toFile()
        try {
            val json = """{"context":"lab","nodes":[{"node":"192.0.2.10","hostname":"cp-1","reachable":true}]}"""
            OfflineCache(dir, sealer, enabled = { true }).save("fp", OVERVIEW, json, at = System.currentTimeMillis(), epoch = 0)
            // A new launch: restoreOffline has not run yet.
            val launch = ResultCache(OfflineCache(dir, sealer, enabled = { true }), scope = { "1|lab|" }, cluster = { "fp" })
            val cached = launch.cached<name.levis.ichor.model.ClusterOverview>(OVERVIEW)
            assertEquals("cp-1", cached?.value?.nodes?.single()?.hostname)
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun invalidateDropsEverythingAndSignals() = runBlocking {
        cache.remember("a") { "1" }
        cache.remember("b|features|n1") { "2" }
        cache.forgetContaining("|features|")
        assertNull(cache.cached<String>("b|features|n1"))
        assertEquals("1", cache.cached<String>("a")?.value)
        cache.forget("a")
        assertNull(cache.cached<String>("a"))

        cache.remember("a") { "1" }
        val before = cache.invalidations.value
        cache.invalidate()
        assertNull(cache.cached<String>("a"))
        assertEquals(before + 1, cache.invalidations.value)
    }
}
