package name.levis.ichor.ui.components

import org.junit.Assert.assertEquals
import org.junit.Test

class DataFreshnessTest {

    @Test
    fun ageUnits() {
        assertEquals(Age(AgeUnit.JUST_NOW, 0), age(0))
        assertEquals(Age(AgeUnit.JUST_NOW, 0), age(59_999))
        assertEquals(Age(AgeUnit.MINUTES, 1), age(60_000))
        assertEquals(Age(AgeUnit.MINUTES, 59), age(3_599_000))
        assertEquals(Age(AgeUnit.HOURS, 2), age(2 * 3_600_000L))
        assertEquals(Age(AgeUnit.DAYS, 3), age(3 * 86_400_000L))
        assertEquals(Age(AgeUnit.JUST_NOW, 0), age(-5_000)) // clock skew never shows a negative age
    }
}
