package name.levis.ichor.monitor

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.SignInRenewal
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SignInAlertsTest {
    private val oidc = ContextSummary(name = "sso-lab", fingerprint = "fpoidc", kind = KIND_KUBE)
    private val omni = ContextSummary(name = "omni-lab", fingerprint = "fpomni")
    private val contexts = listOf(oidc, omni)

    private fun renewal(context: String, status: String, kind: String = SignInRenewal.KIND_KUBE) =
        SignInRenewal(context = context, kind = kind, status = status)

    private fun plan(renewals: List<SignInRenewal>, notified: Set<String> = emptySet()) =
        signInAlertPlan(renewals, contexts, notified) { "label of ${it.name}" }

    @Test
    fun anEndedSessionAlertsOnceUntilSignedInAgain() {
        val ended = listOf(renewal("sso-lab", SignInRenewal.STATUS_SIGN_IN_REQUIRED))

        val first = plan(ended)
        assertEquals(1, first.alerts.size)
        val alert = first.alerts.single().alerts.single()
        assertEquals(AlertKind.SIGN_IN_REQUIRED, alert.kind)
        assertEquals("label of sso-lab", alert.subject)
        assertEquals(AlertChannel.CERTS, alert.channel)
        assertEquals(ShareTarget.screen(ShareTarget.CLUSTER), alert.shareTarget())
        assertEquals(setOf("fpoidc"), first.notified)

        // The next run: still ended, already told.
        val second = plan(ended, first.notified)
        assertTrue(second.alerts.isEmpty())
        assertEquals(setOf("fpoidc"), second.notified)

        // Signed in again: forgotten, so a later end alerts again.
        val renewed = plan(listOf(renewal("sso-lab", SignInRenewal.STATUS_RENEWED)), second.notified)
        assertTrue(renewed.notified.isEmpty())
        assertEquals(1, plan(ended, renewed.notified).alerts.size)
    }

    @Test
    fun offlineTellsNothingAndKeepsWhatWasTold() {
        val offline = plan(listOf(renewal("sso-lab", SignInRenewal.STATUS_ERROR)), setOf("fpoidc"))
        assertTrue(offline.alerts.isEmpty())
        assertEquals(setOf("fpoidc"), offline.notified)

        assertTrue(plan(listOf(renewal("sso-lab", SignInRenewal.STATUS_ERROR))).alerts.isEmpty())
    }

    @Test
    fun matchesTheContextOfItsKind() {
        // A kube renewal named like a Talos context is not that context.
        assertTrue(plan(listOf(renewal("omni-lab", SignInRenewal.STATUS_SIGN_IN_REQUIRED))).alerts.isEmpty())

        val talos = plan(listOf(renewal("omni-lab", SignInRenewal.STATUS_SIGN_IN_REQUIRED, SignInRenewal.KIND_TALOS)))
        assertEquals(omni, talos.alerts.single().context)
    }

    @Test
    fun signInWithGoogleIsRenewedByTheAppNotTold() {
        val google = SignInRenewal(context = "sso-lab", kind = SignInRenewal.KIND_KUBE, status = SignInRenewal.STATUS_SIGN_IN_REQUIRED,
            reason = SignInRenewal.REASON_GOOGLE_NATIVE)
        assertTrue(plan(listOf(google)).alerts.isEmpty())
    }

    @Test
    fun anUnwatchedClusterIsNotTold() {
        val unwatched = signInAlertPlan(listOf(renewal("other", SignInRenewal.STATUS_SIGN_IN_REQUIRED)), contexts, emptySet()) { it.name }
        assertTrue(unwatched.alerts.isEmpty())
        assertTrue(unwatched.notified.isEmpty())
    }
}
