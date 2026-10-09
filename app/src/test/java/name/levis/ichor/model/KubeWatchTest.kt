package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeWatchTest {
    private val key: (String) -> String = { it.substringBefore(':') }

    @Test
    fun appliesChangesInPlaceAndNewRowsLast() {
        val rows = listOf("a:1", "b:1")
        assertEquals(listOf("a:1", "b:1", "c:1"), rows.applying(KubeWatchEvent.Added("c:1"), key))
        assertEquals(listOf("a:2", "b:1"), rows.applying(KubeWatchEvent.Modified("a:2"), key))
        // A change to a row the list did not have yet (added while the list loaded) joins it.
        assertEquals(listOf("a:1", "b:1", "d:1"), rows.applying(KubeWatchEvent.Modified("d:1"), key))
        assertEquals(listOf("b:1"), rows.applying(KubeWatchEvent.Deleted("a:9"), key))
        assertEquals(listOf("z:1"), rows.applying(KubeWatchEvent.Sync(listOf("z:1")), key))
    }

    @Test
    fun aSyncMakesTheLoadCompleteAndAChangeKeepsItsPages() {
        val load = PagedLoad(items = listOf("a:1"), continueToken = "t", remaining = 5, pages = 1, done = false, capped = true)
        val synced = load.applying(KubeWatchEvent.Sync(listOf("a:1", "b:1")), key)
        assertTrue(synced.done)
        assertEquals(listOf("a:1", "b:1"), synced.items)
        val changed = load.applying(KubeWatchEvent.Added("b:1"), key)
        assertTrue(changed.hasMore)
        assertEquals("t", changed.continueToken)
        assertEquals(listOf("a:1", "b:1"), changed.items)
    }

    @Test
    fun decodesWhatTheListenerGot() {
        val item: (String) -> String = { json -> json.trim('"').also { require(it.isNotEmpty()) } }
        val list: (String) -> List<String> = { json -> json.trim('[', ']').split(',').map { it.trim('"') } }
        assertEquals(KubeWatchEvent.Sync(listOf("a", "b")), KubeWatchEvent.decode("SYNC", """["a","b"]""", item, list))
        assertEquals(KubeWatchEvent.Added("a"), KubeWatchEvent.decode("ADDED", "\"a\"", item, list))
        assertEquals(KubeWatchEvent.Deleted("a"), KubeWatchEvent.decode("DELETED", "\"a\"", item, list))
        assertNull(KubeWatchEvent.decode("BOOKMARK", "\"a\"", item, list))
        // What the models cannot read is dropped, not an error.
        assertNull(KubeWatchEvent.decode("ADDED", "", item, list))
    }
}
