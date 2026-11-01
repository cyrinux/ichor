package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class OutageRecoveryTest {

    private fun outage(cause: OutageCause, error: String = "authentication failed (certificate expired or wrong CA?)") =
        ClusterOutage(cause, nodes = 3, errors = listOf(error))

    private fun recovery(
        cause: OutageCause,
        omni: Boolean = false,
        demo: Boolean = false,
        masked: Boolean = false,
        error: String = "authentication failed (certificate expired or wrong CA?)",
    ) = outageRecovery(outage(cause, error), omni, demo, masked)

    private val hostnameMismatch = "x509: certificate is valid for lab-a, not lab-b"

    @Test
    fun aNetworkOutageOffersWhatItAlwaysDid() {
        val expected = OutageRecovery(RecoveryAction.VPN_SETTINGS, retrySecondary = true, scan = true, editEndpoints = true, explanation = null)
        assertEquals(expected, recovery(OutageCause.NETWORK))
        assertEquals(expected, recovery(OutageCause.NETWORK, omni = true))
        assertEquals(expected.copy(scan = false, editEndpoints = false), recovery(OutageCause.NETWORK, demo = true))
        assertEquals(expected.copy(editEndpoints = false), recovery(OutageCause.NETWORK, masked = true))
    }

    @Test
    fun aRefusedDirectTalosconfigOffersToImportTheRenewedOne() {
        assertEquals(
            OutageRecovery(RecoveryAction.REPLACE_CONFIG, retrySecondary = true, scan = false, editEndpoints = false, explanation = RecoveryExplanation.REPLACE_CONFIG),
            recovery(OutageCause.CREDENTIALS),
        )
    }

    @Test
    fun aRefusedOmniContextOffersToSignInAgain() {
        assertEquals(
            OutageRecovery(RecoveryAction.SIGN_IN, retrySecondary = true, scan = false, editEndpoints = false, explanation = RecoveryExplanation.OMNI_SESSION),
            recovery(OutageCause.CREDENTIALS, omni = true),
        )
    }

    @Test
    fun aCertificateForAnotherNameIsAnAddressProblemSoEndpointsStayEditable() {
        assertEquals(true, recovery(OutageCause.CREDENTIALS, error = hostnameMismatch).editEndpoints)
        assertEquals(true, recovery(OutageCause.CREDENTIALS, omni = true, error = hostnameMismatch).editEndpoints)
        assertEquals(false, recovery(OutageCause.CREDENTIALS, masked = true, error = hostnameMismatch).editEndpoints)
    }

    @Test
    fun theDemoHasNothingToSignInToOrReplace() {
        val expected = OutageRecovery(RecoveryAction.RETRY, retrySecondary = false, scan = false, editEndpoints = false, explanation = null)
        assertEquals(expected, recovery(OutageCause.CREDENTIALS, demo = true))
        assertEquals(expected, recovery(OutageCause.CREDENTIALS, demo = true, omni = true))
        assertEquals(expected, recovery(OutageCause.CREDENTIALS, demo = true, error = hostnameMismatch))
    }

    @Test
    fun anyOtherFailureKeepsTheGenericPath() {
        val expected = OutageRecovery(RecoveryAction.RETRY, retrySecondary = false, scan = false, editEndpoints = true, explanation = null)
        assertEquals(expected, recovery(OutageCause.OTHER))
        assertEquals(expected, recovery(OutageCause.OTHER, omni = true))
        assertEquals(expected.copy(editEndpoints = false), recovery(OutageCause.OTHER, demo = true))
        assertEquals(expected.copy(editEndpoints = false), recovery(OutageCause.OTHER, masked = true))
    }
}
