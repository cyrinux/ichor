package name.levis.ichor.monitor

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FreezeReminderPlanTest {
    private val now = 1_000_000L
    private val lead = 600L

    @Test
    fun schedulesEachRunningFreezeBeforeItsEnd() {
        val plan = planFreezeReminders(null, mapOf("demo/a" to now + 1_000, "demo/b" to now + 5_000), now, lead)!!
        assertEquals(mapOf("demo/a" to 400L, "demo/b" to 4_400L), plan.schedule)
        assertEquals(emptySet<String>(), plan.cancel)
        assertEquals(setOf("demo/a@${now + 1_000}", "demo/b@${now + 5_000}"), plan.signature)
    }

    @Test
    fun nothingToDoWhenUnchanged() {
        val freezes = mapOf("demo/a" to now + 1_000)
        val first = planFreezeReminders(null, freezes, now, lead)!!
        assertNull(planFreezeReminders(first.signature, freezes, now + 10, lead))
    }

    @Test
    fun endedFreezesAreIgnoredAndGoneOnesCancelled() {
        val previous = setOf("demo/a@${now + 1_000}", "demo/gone@${now + 2_000}")
        val plan = planFreezeReminders(previous, mapOf("demo/a" to now + 1_000, "demo/ended" to now - 1), now, lead)!!
        assertEquals(setOf("demo/gone"), plan.cancel)
        assertEquals(setOf("demo/a"), plan.schedule.keys)
        assertEquals(setOf("demo/a@${now + 1_000}"), plan.signature)
    }

    @Test
    fun movedEndIsScheduledAgainNotCancelled() {
        val plan = planFreezeReminders(setOf("demo/a@${now + 1_000}"), mapOf("demo/a" to now + 9_000), now, lead)!!
        assertEquals(emptySet<String>(), plan.cancel)
        assertEquals(mapOf("demo/a" to 8_400L), plan.schedule)
    }

    @Test
    fun insideTheLeadTimeKeepsThePendingReminder() {
        val plan = planFreezeReminders(null, mapOf("demo/a" to now + lead), now, lead)!!
        assertEquals(emptyMap<String, Long>(), plan.schedule)
        assertEquals(emptySet<String>(), plan.cancel)
        assertEquals(setOf("demo/a@${now + lead}"), plan.signature)
    }

    @Test
    fun firstSyncWithNoFreezeStillRecordsIt() {
        assertEquals(ReminderPlan(emptySet(), emptySet(), emptyMap()), planFreezeReminders(null, emptyMap(), now, lead))
        assertNull(planFreezeReminders(emptySet(), emptyMap(), now, lead))
    }
}
