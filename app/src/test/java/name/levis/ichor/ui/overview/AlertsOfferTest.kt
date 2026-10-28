package name.levis.ichor.ui.overview

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertsOfferTest {
    @Test
    fun offeredOnceAfterTheFirstRealCluster() {
        assertTrue(shouldOfferAlerts(pending = true, alertsEnabled = false, isDemo = false, tourActive = false))
    }

    @Test
    fun notOnceDismissedOrTurnedOn() {
        assertFalse(shouldOfferAlerts(pending = false, alertsEnabled = false, isDemo = false, tourActive = false))
        assertFalse(shouldOfferAlerts(pending = true, alertsEnabled = true, isDemo = false, tourActive = false))
    }

    @Test
    fun neverOnTheDemoNorOverItsTour() {
        assertFalse(shouldOfferAlerts(pending = true, alertsEnabled = false, isDemo = true, tourActive = false))
        assertFalse(shouldOfferAlerts(pending = true, alertsEnabled = false, isDemo = false, tourActive = true))
    }
}
