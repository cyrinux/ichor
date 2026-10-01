package name.levis.talosmobile.ui.components

import org.junit.Assert.assertEquals
import org.junit.Test

class DataFreshnessTest {

    @Test
    fun agoWording() {
        assertEquals("just now", ago(0))
        assertEquals("just now", ago(59_999))
        assertEquals("1 min ago", ago(60_000))
        assertEquals("59 min ago", ago(3_599_000))
        assertEquals("2 h ago", ago(2 * 3_600_000L))
        assertEquals("3 d ago", ago(3 * 86_400_000L))
        assertEquals("just now", ago(-5_000)) // clock skew never shows a negative age
    }
}
