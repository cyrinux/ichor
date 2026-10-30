package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class UiStyleTest {
    @Test
    fun `parse reads a known style`() {
        assertEquals(UiStyle.MATERIAL3, UiStyle.parse("MATERIAL3"))
    }

    @Test
    fun `parse falls back to Material 3 on a missing or unknown style`() {
        assertEquals(UiStyle.MATERIAL3, UiStyle.parse(null))
        assertEquals(UiStyle.MATERIAL3, UiStyle.parse("NOT_A_STYLE"))
    }
}
