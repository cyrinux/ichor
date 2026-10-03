package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class OfflineCacheTest {

    @get:Rule
    val folder = TemporaryFolder()

    /** Reverses the bytes: enough to tell sealed files from plain ones, no Keystore needed. */
    private class FakeSealer : OfflineCache.Sealer {
        var forgotten = false
        override fun write(file: File, bytes: ByteArray) = file.writeBytes(bytes.reversedArray())
        override fun read(file: File): ByteArray? = if (file.exists()) file.readBytes().reversedArray() else null
        override fun forget() {
            forgotten = true
        }
    }

    private var now = 10_000_000L
    private var enabled = true
    private val sealer = FakeSealer()
    private val directory by lazy { File(folder.root, "offline") }

    private fun cache() = OfflineCache(directory, sealer, enabled = { enabled }, now = { now })

    @Test
    fun aSavedResultIsReadBackAfterARestart() = runBlocking {
        cache().save("fp", "overview", """{"nodes":[]}""", at = now, epoch = 0)
        val restarted = cache()
        assertNull(restarted.peek("fp", "overview"))
        restarted.load("fp")
        assertEquals(TalosRepository.Timed("""{"nodes":[]}""", now), restarted.peek("fp", "overview"))
    }

    @Test
    fun clustersAreKeptApart() = runBlocking {
        val cache = cache()
        cache.save("one", "overview", "1", now, epoch = 0)
        cache.save("two", "overview", "2", now, epoch = 0)
        assertEquals("1", cache.peek("one", "overview")?.value)
        assertEquals("2", cache.peek("two", "overview")?.value)
    }

    @Test
    fun noNodeAddressOrClusterShowsInFileNamesOrContent() = runBlocking {
        cache().save("cluster-fingerprint", "services|10.0.0.1", "secret-host", now, epoch = 0)
        val files = directory.walk().toList()
        assertTrue(files.none { "10.0.0.1" in it.name || "cluster-fingerprint" in it.name })
        assertTrue(files.filter { it.isFile }.none { "secret-host" in it.readText() })
    }

    @Test
    fun resultsOlderThanADayAreDropped() = runBlocking {
        cache().save("fp", "overview", "old", at = now, epoch = 0)
        now += OfflineCache.MAX_AGE_MS + 1
        val restarted = cache()
        restarted.load("fp")
        assertNull(restarted.peek("fp", "overview"))
        assertTrue(directory.walk().none { it.isFile })
    }

    @Test
    fun aResultSavedBeforeLoadDoesNotHideTheStoredOnes() = runBlocking {
        cache().save("fp", "etcd", "stored", now, epoch = 0)
        val restarted = cache()
        restarted.save("fp", "overview", "fresh", now, epoch = 0)
        restarted.load("fp")
        assertEquals("stored", restarted.peek("fp", "etcd")?.value)
        assertEquals("fresh", restarted.peek("fp", "overview")?.value)
    }

    @Test
    fun nothingIsSavedOrShownWhileTurnedOff() = runBlocking {
        val cache = cache()
        cache.save("fp", "overview", "1", now, epoch = 0)
        enabled = false
        assertNull(cache.peek("fp", "overview"))
        cache.save("fp", "etcd", "2", now, epoch = 0)
        enabled = true
        val restarted = cache()
        restarted.load("fp")
        assertNull(restarted.peek("fp", "etcd"))
    }

    @Test
    fun removedClustersAreDeleted() = runBlocking {
        val cache = cache()
        cache.save("kept", "overview", "1", now, epoch = 0)
        cache.save("removed", "overview", "2", now, epoch = 0)
        cache.retain(listOf("kept"))
        assertNull(cache.peek("removed", "overview"))
        val restarted = cache()
        restarted.load("kept")
        restarted.load("removed")
        assertEquals("1", restarted.peek("kept", "overview")?.value)
        assertNull(restarted.peek("removed", "overview"))
    }

    @Test
    fun clearDeletesEverythingAndTheKey() = runBlocking {
        val cache = cache()
        cache.save("fp", "overview", "1", now, epoch = 0)
        cache.clear()
        assertNull(cache.peek("fp", "overview"))
        assertFalse(directory.exists())
        assertTrue(sealer.forgotten)
    }

    @Test
    fun anUnreadableFileIsDeleted() = runBlocking {
        cache().save("fp", "overview", "1", now, epoch = 0)
        directory.walk().filter { it.isFile }.forEach { it.writeText("garbage") }
        val restarted = cache()
        restarted.load("fp")
        assertNull(restarted.peek("fp", "overview"))
        assertTrue(directory.walk().none { it.isFile })
    }

    @Test
    fun aResultFetchedBeforeAClearIsNotSaved() = runBlocking {
        val cache = cache()
        val epoch = cache.epoch // read when the fetch starts
        cache.clear() // e.g. screenshot mode turned on meanwhile
        cache.save("fp", "overview", "real names", now, epoch)
        assertNull(cache.peek("fp", "overview"))
        assertFalse(directory.exists())
        cache.save("fp", "overview", "masked", now, cache.epoch)
        assertEquals("masked", cache.peek("fp", "overview")?.value)
    }
}
