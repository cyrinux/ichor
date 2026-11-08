package name.levis.ichor.ui.alerts

import name.levis.ichor.model.AmSilenceState
import name.levis.ichor.model.OnCallStatus
import name.levis.ichor.model.OnCallVerb
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertRowActionsTest {
    @Test
    fun aTriggeredIncidentCanBeAcknowledgedOrResolved() {
        assertEquals(listOf(OnCallVerb.ACKNOWLEDGE, OnCallVerb.RESOLVE), onCallRowVerbs(OnCallStatus.TRIGGERED))
    }

    @Test
    fun anAcknowledgedIncidentCanOnlyBeResolved() {
        assertEquals(listOf(OnCallVerb.RESOLVE), onCallRowVerbs(OnCallStatus.ACKNOWLEDGED))
    }

    @Test
    fun onlyAnExpiredSilenceHasNothingToExpire() {
        assertTrue(silenceExpirable(AmSilenceState.ACTIVE))
        assertTrue(silenceExpirable(AmSilenceState.PENDING))
        assertFalse(silenceExpirable(AmSilenceState.EXPIRED))
    }
}
