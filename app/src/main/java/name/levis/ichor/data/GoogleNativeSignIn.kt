package name.levis.ichor.data

import android.content.Context
import android.content.Intent
import android.content.IntentSender
import name.levis.ichor.model.GCP_GOOGLE_SIGN_IN

/**
 * "Sign in with Google" for GKE through Google's own SDK (Google Identity Services): Play
 * build only (src/play); the open-source builds have none (src/foss), so the proprietary Play
 * services never reach their APKs. The app holds the access token Google gives; the Go core
 * keeps it in the cluster's session (never in a backup) and asks for a new one when it expires
 * (sign-in-required reason "google-native"). See createGoogleNativeSignIn.
 */
interface GoogleNativeSignIn {
    /** Whether Google's sign-in can run here (the Play build, with Play services). */
    fun available(context: Context): Boolean

    /**
     * Asks Google for an access token to the clusters: at once when the user already allowed
     * it, else [GoogleAuthorization.NeedsUser] to show Google's picker and consent ([silent]:
     * never, a failure instead).
     */
    suspend fun authorize(context: Context, silent: Boolean): GoogleAuthorization

    /** The answer of Google's picker and consent ([data], the activity result). */
    fun fromResolution(context: Context, data: Intent?): GoogleAuthorization
}

/** What asking Google for a token gave. */
sealed interface GoogleAuthorization {
    /** An access token, valid until [expiresAtUnix]; [email] when Google said it. */
    data class Token(val accessToken: String, val expiresAtUnix: Long, val email: String) : GoogleAuthorization

    /** Google's picker and consent must be shown first ([intentSender]). */
    data class NeedsUser(val intentSender: IntentSender) : GoogleAuthorization

    /** No token: cancelled, refused, or no Play services ([message] is Google's, may be empty). */
    data class Failed(val message: String) : GoogleAuthorization
}

/** The sign-in-required reason the Go core gives when the app-held token expired. */
const val GOOGLE_NATIVE_RENEW_REASON = "google-native"

/** What the Go core takes for [token] (KubeSetCredentials). */
fun googleSignInSecrets(token: GoogleAuthorization.Token): Map<String, String> = mapOf(
    GCP_GOOGLE_SIGN_IN to "android",
    "gcpAccessToken" to token.accessToken,
    "gcpAccessTokenExpiry" to token.expiresAtUnix.toString(),
    "gcpAccount" to token.email,
)

/**
 * Google does not say when its access token expires: an hour, minus a minute for clock drift
 * (the Go core renews a minute before as well).
 */
fun googleTokenExpiry(nowUnix: Long = System.currentTimeMillis() / 1000): Long = nowUnix + 3600 - 60
