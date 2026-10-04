package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class FundingHistoryTest {

    @Test
    fun startsEmpty() {
        assertEquals(emptySet<String>(), FundingHistory(MemoryPrefs()).backed.value)
    }

    @Test
    fun recordsAndPersistsProducts() {
        val prefs = MemoryPrefs()
        val history = FundingHistory(prefs)
        history.record(listOf("fund_a_2"))
        history.record(listOf("fund_b_5", "fund_a_2"))
        assertEquals(setOf("fund_a_2", "fund_b_5"), history.backed.value)
        assertEquals(setOf("fund_a_2", "fund_b_5"), FundingHistory(prefs).backed.value)
    }
}
