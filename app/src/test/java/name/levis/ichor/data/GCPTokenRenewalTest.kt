package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import name.levis.ichor.model.gcpNativeTokenExpiry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class GCPTokenRenewalTest {
    private val now = 1_700_000_000L

    /** A stored "Sign in with Google" credential: its project and zone, and the app-held token until [expiry]. */
    private fun stored(token: String, expiry: Long) = """
        {"project":"sample-project","zone":"europe-west1-b","account":"someone@example.com",
         "googleSignIn":{"method":"gke","user":"someone@example.com",
           "secrets":{"gcpGoogleSignIn":"android"},
           "session":{"bearer":"$token","bearerExpiry":"$expiry"}}}
    """.trimIndent()

    private val expired = Exception(
        "Google sign-in: it has ended: sign in with Google again from the Google Cloud card: " +
            "kube-sign-in-required: sign in to this cluster (gke): google-native",
    )

    private class Fakes(var answer: GoogleAuthorization) {
        var authorizations = 0
        val handed = mutableListOf<Map<String, String>>()
        val saved = mutableListOf<String>()

        /** What Ichorgo.gcpSignInNative gives for the handed token: a credential with no project yet. */
        fun renewal(now: Long) = GCPTokenRenewal(
            authorize = {
                authorizations++
                answer
            },
            signIn = { secrets ->
                handed += secrets
                """{"account":"someone@example.com","googleSignIn":{"method":"gke","user":"someone@example.com",""" +
                    """"secrets":{"gcpGoogleSignIn":"android"},"session":{"bearer":"${secrets["gcpAccessToken"]}",""" +
                    """"bearerExpiry":"${secrets["gcpAccessTokenExpiry"]}"}}}"""
            },
            nowMs = { now * 1000 },
        )
    }

    private fun bearer(credential: String): String =
        Json.parseToJsonElement(credential).jsonObject["googleSignIn"]!!.jsonObject["session"]!!.jsonObject["bearer"]!!.jsonPrimitive.content

    private fun project(credential: String): String = Json.parseToJsonElement(credential).jsonObject["project"]!!.jsonPrimitive.content

    @Test
    fun anExpiredTokenIsRenewedResealedUnderItsProjectAndTheCallRetried() = runBlocking {
        val fakes = Fakes(GoogleAuthorization.Token("ya29.renewed", now + 3540, "someone@example.com"))
        val calls = mutableListOf<String>()

        val result = fakes.renewal(now).withRenewal(stored("ya29.old", now - 60), save = { fakes.saved += it }) { credential ->
            calls += bearer(credential)
            if (calls.size == 1) throw expired
            "options"
        }

        assertEquals("options", result)
        assertEquals(listOf("ya29.old", "ya29.renewed"), calls)
        assertEquals("android", fakes.handed.single()["gcpGoogleSignIn"])
        val sealed = fakes.saved.single()
        assertEquals("ya29.renewed", bearer(sealed))
        assertEquals("sample-project", project(sealed))
        assertEquals("europe-west1-b", Json.parseToJsonElement(sealed).jsonObject["zone"]!!.jsonPrimitive.content)
    }

    @Test
    fun aRefusedRenewalAsksToSignInAgainWithoutTheCodeAndWithoutALoop() = runBlocking {
        val fakes = Fakes(GoogleAuthorization.Failed("consent withdrawn"))
        var calls = 0
        try {
            fakes.renewal(now).withRenewal(stored("ya29.old", now - 60), save = { fakes.saved += it }) {
                calls++
                throw expired
            }
            fail("no failure")
        } catch (e: IllegalStateException) {
            assertEquals("Google sign-in: it has ended: sign in with Google again from the Google Cloud card", e.message)
        }
        assertEquals(1, calls)
        assertEquals(1, fakes.authorizations)
        assertTrue(fakes.saved.isEmpty())
    }

    @Test
    fun anotherFailureIsLeftAsItIs() = runBlocking {
        val fakes = Fakes(GoogleAuthorization.Token("ya29.renewed", now + 3540, "someone@example.com"))
        val denied = Exception("Google sign-in: may not bill calls to project sample-project")
        try {
            fakes.renewal(now).withRenewal(stored("ya29.old", now + 3000), save = { fakes.saved += it }) { throw denied }
            fail("no failure")
        } catch (e: Exception) {
            assertEquals(denied, e)
        }
        assertEquals(0, fakes.authorizations)
    }

    @Test
    fun aRunStartsWithATokenThatLastsItsLength() = runBlocking {
        val fakes = Fakes(GoogleAuthorization.Token("ya29.renewed", now + 3540, "someone@example.com"))
        val renewal = fakes.renewal(now)

        val lasting = stored("ya29.fresh", now + 3000)
        assertEquals(lasting, renewal.fresh(lasting) { fakes.saved += it })
        assertEquals(0, fakes.authorizations)

        val ending = renewal.fresh(stored("ya29.old", now + 120)) { fakes.saved += it }
        assertEquals("ya29.renewed", bearer(ending))
        assertEquals("sample-project", project(fakes.saved.single()))

        // A key, or a browser sign-in, is never touched.
        val key = """{"serviceAccountJson":"{}","project":"sample-project"}"""
        assertEquals(key, renewal.fresh(key) { fakes.saved += it })
        assertEquals(1, fakes.authorizations)
    }

    @Test
    fun onlyTheAndroidSignInHasATokenTheAppRenews() {
        assertEquals(now + 60, gcpNativeTokenExpiry(stored("ya29.x", now + 60)))
        assertNull(gcpNativeTokenExpiry("""{"googleSignIn":{"secrets":{"gcpGcloudSignIn":"1"}}}"""))
        assertNull(gcpNativeTokenExpiry("""{"serviceAccountJson":"{}"}"""))
        assertNull(gcpNativeTokenExpiry("not json"))
        assertFalse(gcpNativeTokenExpiry("""{"googleSignIn":{"secrets":{"gcpGoogleSignIn":"android"}}}""") == null)
    }
}
