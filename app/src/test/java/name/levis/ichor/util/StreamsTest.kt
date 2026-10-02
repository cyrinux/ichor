package name.levis.ichor.util

import org.junit.Assert.assertArrayEquals
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
}
