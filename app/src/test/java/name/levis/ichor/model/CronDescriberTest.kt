package name.levis.ichor.model

import java.time.DayOfWeek
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CronDescriberTest {
    @Test fun daily() {
        assertEquals(CronShape.Daily(2, 30), describeCron("30 2 * * *"))
        assertEquals(CronShape.Daily(0, 0), describeCron("@daily"))
        assertEquals(CronShape.Daily(0, 0), describeCron(" @midnight "))
    }

    @Test fun weekly() {
        assertEquals(CronShape.Weekly(DayOfWeek.MONDAY, 9, 0), describeCron("0 9 * * 1"))
        assertEquals(CronShape.Weekly(DayOfWeek.SUNDAY, 9, 0), describeCron("0 9 * * 0"))
        assertEquals(CronShape.Weekly(DayOfWeek.SUNDAY, 9, 0), describeCron("0 9 * * 7"))
        assertEquals(CronShape.Weekly(DayOfWeek.SATURDAY, 23, 59), describeCron("59  23 * * SAT"))
        assertEquals(CronShape.Weekly(DayOfWeek.SUNDAY, 0, 0), describeCron("@weekly"))
    }

    @Test fun monthly() {
        assertEquals(CronShape.Monthly(1, 9, 0), describeCron("0 9 1 * *"))
        assertEquals(CronShape.Monthly(15, 4, 5), describeCron("5 4 15 * *"))
        assertEquals(CronShape.Monthly(1, 0, 0), describeCron("@monthly"))
    }

    @Test fun otherShapesAreLeftRaw() {
        listOf(
            "*/15 * * * *", "0 */6 * * *", "0 2 * * 1-5", "0 2 * * 1,3", "0 2 1 1 *", "0 2 1 * 1",
            "60 2 * * *", "0 24 * * *", "0 2 32 * *", "0 2 * * 8", "@hourly", "@yearly",
            "CRON_TZ=UTC 0 2 * * *", "0 2 * *", "", "nonsense",
        ).forEach { assertNull(it, describeCron(it)) }
    }
}
