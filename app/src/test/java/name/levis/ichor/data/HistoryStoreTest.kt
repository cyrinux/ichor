package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.HistoryAlertSpan
import name.levis.ichor.model.HistoryNodeOutage
import name.levis.ichor.model.HistorySince
import name.levis.ichor.model.hasNews
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HistoryStoreTest {

    /** Sealed files in memory; [unreadable] ones throw as a locked Keystore would. */
    private class MemoryFiles : ClusterFiles {
        val files = mutableMapOf<String, ByteArray>()
        val unreadable = mutableSetOf<String>()

        override fun readBytes(fingerprint: String): ByteArray? {
            check(fingerprint !in unreadable) { "locked" }
            return files[fingerprint]
        }

        override fun writeBytes(fingerprint: String, bytes: ByteArray) {
            files[fingerprint] = bytes
        }

        override fun sync(fingerprints: Collection<String>) {
            files.keys.retainAll(fingerprints.toSet())
        }
    }

    /** A ring that is the records joined by newlines; "newer" stands for a ring of a newer version. */
    private val appender: HistoryAppender = { ring, record, _ ->
        val text = ring?.toString(Charsets.UTF_8).orEmpty()
        require(text != "newer") { "history ring written by a newer version" }
        require(record.startsWith("{")) { "bad record" }
        (if (text.isEmpty()) record else "$text\n$record").toByteArray()
    }

    private fun text(bytes: ByteArray?) = bytes?.toString(Charsets.UTF_8)

    @Test
    fun eachClusterHasItsOwnRing() {
        val files = MemoryFiles()
        val store = HistoryStore(files, appender)

        assertTrue(store.append("fl", "{1}", 1))
        assertTrue(store.append("fp", "{2}", 2))
        assertTrue(store.append("fl", "{3}", 3))

        assertEquals("{1}\n{3}", text(store.ring("fl")))
        assertEquals("{2}", text(store.ring("fp")))
        assertNull(store.ring("other"))
    }

    @Test
    fun aRemovedClusterDropsItsRing() {
        val files = MemoryFiles()
        val store = HistoryStore(files, appender)
        store.append("fl", "{1}", 1)
        store.append("fp", "{2}", 2)

        store.sync(listOf("fp"))
        assertNull(store.ring("fl"))
        assertEquals("{2}", text(store.ring("fp")))

        store.sync(emptyList())
        assertTrue(files.files.isEmpty())
    }

    @Test
    fun aRingOfANewerVersionIsKeptAsItIs() {
        val files = MemoryFiles()
        files.files["fl"] = "newer".toByteArray()
        val store = HistoryStore(files, appender)

        assertFalse(store.append("fl", "{1}", 1))
        assertArrayEquals("newer".toByteArray(), files.files["fl"])
    }

    @Test
    fun aRefusedRecordLeavesTheRing() {
        val files = MemoryFiles()
        val store = HistoryStore(files, appender)
        store.append("fl", "{1}", 1)

        assertFalse(store.append("fl", "not json", 2))
        assertEquals("{1}", text(files.files["fl"]))
    }

    @Test
    fun aRingThatCannotBeReadNowIsNotReplaced() {
        val files = MemoryFiles()
        files.files["fl"] = "{1}".toByteArray()
        files.unreadable += "fl"
        val store = HistoryStore(files, appender)

        assertFalse(store.append("fl", "{2}", 2))
        assertNull(store.ring("fl"))
        files.unreadable.clear()
        assertEquals("{1}", text(store.ring("fl")))
    }

    @Test
    fun lastLookedStartsAtTheFirstVisitAndNeverMovesBack() {
        val looked = LastLooked(MemoryPrefs())

        // A first visit replays nothing: it starts now.
        assertEquals(1_000L, looked.start("fl", 1_000L))
        assertEquals(1_000L, looked.start("fl", 5_000L))

        looked.mark("fl", 9_000L)
        looked.mark("fl", 2_000L)
        assertEquals(9_000L, looked.start("fl", 10_000L))
    }

    @Test
    fun lastLookedForgetsRemovedClusters() {
        val looked = LastLooked(MemoryPrefs())
        looked.mark("fl", 1_000L)
        looked.mark("fp", 1_000L)

        looked.sync(ConfigSummary(current = "prod", contexts = listOf(ContextSummary(name = "prod", fingerprint = "fp"))))

        assertEquals(setOf("fp"), looked.times.value.keys)
    }

    @Test
    fun onlyWhatHappenedSinceIsNews() {
        val from = 10_000L
        assertFalse(HistorySince(from = from).hasNews)
        // Open, or down, since before the last look: no news.
        assertFalse(
            HistorySince(
                from = from,
                alertsOpen = listOf(HistoryAlertSpan("am:a", openedAt = 5_000L)),
                nodesDown = listOf(HistoryNodeOutage("10.0.0.21", downAt = 5_000L)),
            ).hasNews,
        )
        assertTrue(HistorySince(from = from, alertsOpen = listOf(HistoryAlertSpan("am:a", openedAt = 12_000L))).hasNews)
        assertTrue(HistorySince(from = from, nodesDown = listOf(HistoryNodeOutage("10.0.0.21", downAt = 12_000L))).hasNews)
        assertTrue(HistorySince(from = from, alertsResolved = listOf(HistoryAlertSpan("am:a", openedAt = 5_000L, closedAt = 11_000L))).hasNews)
        assertTrue(HistorySince(from = from, nodesRecovered = listOf(HistoryNodeOutage("10.0.0.21", downAt = 5_000L, upAt = 11_000L))).hasNews)
    }
}
