package name.levis.ichor.data

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import name.levis.ichor.model.SignInRenewal
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

class GoogleTokenRenewerTest {
    private val expired = Exception("kube nodes: kube-sign-in-required: sign in to this cluster (gke): google-native")
    private val otherReason = Exception("kube nodes: kube-sign-in-required: sign in to this cluster (gke): invalid_grant")

    private class Fakes(var answer: GoogleAuthorization = GoogleAuthorization.Token("ya29.renewed", 1_700_003_540, "someone@example.com")) {
        val authorizations = AtomicInteger()
        val stored = mutableListOf<Pair<String, Map<String, String>>>()
        val clock = AtomicLong(1_700_000_000_000)
        var gate: CompletableDeferred<Unit>? = null

        fun renewer() = GoogleTokenRenewer(
            authorize = {
                authorizations.incrementAndGet()
                gate?.await()
                answer
            },
            store = { context, secrets -> synchronized(stored) { stored += context to secrets } },
            nowMs = clock::get,
        )
    }

    @Test
    fun anExpiredGoogleTokenIsRenewedOnceAndStoredForTheCluster() = runBlocking {
        val fakes = Fakes()
        assertTrue(fakes.renewer().renewIfNeeded("gke-prod", expired))
        assertEquals(1, fakes.authorizations.get())
        assertEquals("gke-prod", fakes.stored.single().first)
        assertEquals("ya29.renewed", fakes.stored.single().second["gcpAccessToken"])
    }

    @Test
    fun anotherSignInReasonIsLeftToTheUser() = runBlocking {
        val fakes = Fakes()
        val renewer = fakes.renewer()
        assertFalse(renewer.renewIfNeeded("gke-prod", otherReason))
        assertFalse(renewer.renewIfNeeded("gke-prod", Exception("connection refused")))
        assertEquals(0, fakes.authorizations.get())
        assertTrue(fakes.stored.isEmpty())
    }

    @Test
    fun twoCallersAtOnceRenewOnce() = runBlocking {
        val fakes = Fakes().apply { gate = CompletableDeferred() }
        val renewer = fakes.renewer()
        val first = async { renewer.renewIfNeeded("gke-prod", expired) }
        val second = async { renewer.renewIfNeeded("gke-prod", expired) }
        fakes.gate!!.complete(Unit)
        assertTrue(first.await())
        assertTrue(second.await())
        assertEquals(1, fakes.authorizations.get())
        assertEquals(1, fakes.stored.size)
    }

    @Test
    fun googleRefusingIsNotAskedAgainWithinAMinute() = runBlocking {
        val fakes = Fakes(answer = GoogleAuthorization.Failed(""))
        val renewer = fakes.renewer()
        assertFalse(renewer.renewIfNeeded("gke-prod", expired))
        assertFalse(renewer.renewIfNeeded("gke-prod", expired))
        assertEquals(1, fakes.authorizations.get())
        fakes.clock.addAndGet(61_000)
        assertFalse(renewer.renewIfNeeded("gke-prod", expired))
        assertEquals(2, fakes.authorizations.get())
        assertTrue(fakes.stored.isEmpty())
    }

    @Test
    fun aTokenAboutToRunOutIsRenewedBeforeTheCall() = runBlocking {
        val fakes = Fakes()
        val renewer = fakes.renewer()
        renewer.renewIfDue("gke-prod")
        assertEquals("nothing known yet: nothing to renew ahead", 0, fakes.authorizations.get())

        assertTrue(renewer.renewIfNeeded("gke-prod", expired))
        renewer.renewIfDue("gke-prod")
        assertEquals("an hour left: not yet", 1, fakes.authorizations.get())

        fakes.clock.set(1_700_003_540_000 - 4 * 60_000)
        renewer.renewIfDue("gke-prod")
        assertEquals("four minutes left: renewed", 2, fakes.authorizations.get())
    }

    @Test
    fun aClusterSignedOutGetsNoGoogleTokenBack() = runBlocking {
        val fakes = Fakes()
        val renewer = fakes.renewer()
        assertTrue(renewer.renewIfNeeded("gke-prod", expired))
        renewer.forget("gke-prod")
        fakes.clock.set(1_700_003_540_000 - 4 * 60_000)
        renewer.renewIfDue("gke-prod")
        assertEquals(1, fakes.authorizations.get())
        assertEquals(1, fakes.stored.size)
    }

    @Test
    fun aSignInAgainForgetsARefusal() = runBlocking {
        val fakes = Fakes(answer = GoogleAuthorization.Failed(""))
        val renewer = fakes.renewer()
        assertFalse(renewer.renewIfNeeded("gke-prod", expired))
        renewer.forget("gke-prod")
        fakes.answer = GoogleAuthorization.Token("ya29.again", 1_700_003_540, "someone@example.com")
        assertTrue(renewer.renewIfNeeded("gke-prod", expired))
    }

    @Test
    fun theRenewalsOfABackgroundCheckRenewOnlyGoogleTokens() = runBlocking {
        val fakes = Fakes()
        val renewals = listOf(
            renewal("gke-prod", SignInRenewal.STATUS_SIGN_IN_REQUIRED, SignInRenewal.REASON_GOOGLE_NATIVE),
            renewal("oidc-ended", SignInRenewal.STATUS_SIGN_IN_REQUIRED, "invalid_grant"),
            renewal("gke-fresh", SignInRenewal.STATUS_VALID),
            renewal("offline", SignInRenewal.STATUS_ERROR, "no route to host"),
        )
        fakes.renewer().renewExpired(renewals)
        assertEquals(listOf("gke-prod"), fakes.stored.map { it.first })
    }

    private fun renewal(context: String, status: String, reason: String = "") =
        SignInRenewal(context = context, kind = SignInRenewal.KIND_KUBE, method = "gke", status = status, reason = reason)
}
