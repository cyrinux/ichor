package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import name.levis.ichor.ui.LocalizedException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.IOException

class RoadmapRepositoryTest {

    private fun roadmap(id: String) = """{"features": [{"id": "$id", "title": {"en": "T"}}]}"""

    @Test
    fun downloadsAndCaches() = runBlocking {
        val prefs = MemoryPrefs()
        assertEquals("a", RoadmapRepository(prefs) { roadmap("a") }.load().features.single().id)
        // Offline afterwards: the last good copy is used.
        assertEquals("a", RoadmapRepository(prefs) { throw IOException("offline") }.load().features.single().id)
    }

    @Test
    fun corruptDownloadKeepsTheLastGoodCopy() = runBlocking {
        val prefs = MemoryPrefs()
        RoadmapRepository(prefs) { roadmap("a") }.load()
        assertEquals("a", RoadmapRepository(prefs) { "<html>" }.load().features.single().id)
    }

    @Test
    fun failsWithNothingToShow() {
        assertThrows(LocalizedException::class.java) {
            runBlocking { RoadmapRepository(MemoryPrefs()) { throw IOException("offline") }.load() }
        }
        assertThrows(LocalizedException::class.java) {
            runBlocking { RoadmapRepository(MemoryPrefs()) { "{" }.load() }
        }
    }
}
