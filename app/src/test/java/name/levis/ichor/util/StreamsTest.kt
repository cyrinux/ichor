package name.levis.ichor.util

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.ByteArrayInputStream

class StreamsTest {

    @Test
    fun readsWithinLimit() {
        val data = ByteArray(20_000) { it.toByte() }
        assertArrayEquals(data, readBounded(ByteArrayInputStream(data), 20_000))
    }

    @Test
    fun rejectsOversized() {
        assertThrows(IllegalArgumentException::class.java) {
            readBounded(ByteArrayInputStream(ByteArray(20_001)), 20_000)
        }
    }

    @Test
    fun oversizedFailsWithTheCallersMessage() {
        val e = assertThrows(IllegalArgumentException::class.java) {
            readBounded(ByteArrayInputStream(ByteArray(11)), 10, "too big for an icon")
        }
        assertEquals("too big for an icon", e.message)
    }

    @Test
    fun readAtMostStopsAtTheLimit() {
        val data = ByteArray(40_000) { it.toByte() }
        assertArrayEquals(data.copyOf(33_000), ByteArrayInputStream(data).readAtMost(33_000))
        assertArrayEquals(data, ByteArrayInputStream(data).readAtMost(50_000))
        assertEquals(0, ByteArrayInputStream(data).readAtMost(0).size)
    }
}
