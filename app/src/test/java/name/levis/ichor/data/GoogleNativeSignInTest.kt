package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class GoogleNativeSignInTest {
    @Test
    fun theTokenGoesToGoAsTheAndroidOption() {
        val secrets = googleSignInSecrets(GoogleAuthorization.Token("ya29.app-held", 1_700_003_540, "dev@example.com"))
        assertEquals(
            mapOf(
                "gcpGoogleSignIn" to "android",
                "gcpAccessToken" to "ya29.app-held",
                "gcpAccessTokenExpiry" to "1700003540",
                "gcpAccount" to "dev@example.com",
            ),
            secrets,
        )
    }

    @Test
    fun googleSaysNoExpiryAnHourMinusAMinuteIsAssumed() {
        assertEquals(1_700_003_540L, googleTokenExpiry(1_700_000_000))
    }
}
