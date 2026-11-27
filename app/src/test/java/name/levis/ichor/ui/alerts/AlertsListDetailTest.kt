package name.levis.ichor.ui.alerts

import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.AmAlerts
import name.levis.ichor.model.AmGroup
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AlertsListDetailTest {
    private val crashing = AmAlert(
        fingerprint = "a1b2c3",
        alertname = "KubePodCrashLooping",
        labels = mapOf("namespace" to "shop", "pod" to "web-1"),
    )
    private val disk = AmAlert(fingerprint = "d4e5f6", alertname = "NodeDiskFull", labels = mapOf("node" to "worker-1"))

    private fun alerts(vararg alerts: AmAlert) =
        AmAlerts(groups = alerts.groupBy { it.alertname }.map { (name, list) -> AmGroup(alertname = name, alerts = list) })

    @Test
    fun theFingerprintIsTheId() {
        assertEquals("a1b2c3", crashing.paneId)
    }

    @Test
    fun withoutAFingerprintTheNameAndLabelsAreTheId() {
        val a = AmAlert(alertname = "Watchdog", labels = mapOf("b" to "2", "a" to "1"))
        val b = AmAlert(alertname = "Watchdog", labels = mapOf("a" to "1", "b" to "2"))
        assertEquals(a.paneId, b.paneId)
        assertNotEquals(a.paneId, AmAlert(alertname = "Watchdog", labels = mapOf("a" to "1")).paneId)
    }

    @Test
    fun theIdPicksTheAlertInAnyGroup() {
        assertEquals(disk, pickedAlert(alerts(crashing, disk), "d4e5f6"))
    }

    @Test
    fun thePaneShowsTheAlertAsLastRead() {
        val silenced = crashing.copy(silencedBy = listOf("s1"))
        assertEquals(silenced, pickedAlert(alerts(silenced, disk), crashing.paneId))
    }

    @Test
    fun anAlertThatStoppedFiringShowsThePlaceholder() {
        assertNull(pickedAlert(alerts(disk), crashing.paneId))
    }

    @Test
    fun nothingPickedOrNotReadShowsThePlaceholder() {
        assertNull(pickedAlert(alerts(crashing), null))
        assertNull(pickedAlert(null, crashing.paneId))
    }
}
