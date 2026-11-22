package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.withTimeoutOrNull
import name.levis.ichor.model.KUBE_SIGN_IN_REQUIRED
import name.levis.ichor.model.gcpMergeRenewed
import name.levis.ichor.model.gcpNativeTokenExpiry
import name.levis.ichor.model.signInNeeded

/** How long a silent Google authorization may take: past it, the renewal failed. */
private const val AUTHORIZE_TIMEOUT_MS = 10_000L

/** A token with less than this left is renewed before a run rather than in the middle of it. */
private const val RENEW_AHEAD_S = 10 * 60L

/**
 * Renews the Google Cloud card's "Sign in with Google" (Play build) without asking, as
 * [GoogleTokenRenewer] does for GKE clusters: Google gives that sign-in an access token only, about
 * an hour long, and the Go core answers sign-in-required with the reason "google-native" once it ran
 * out. [authorize] asks Google silently; [signIn] turns its token into a credential
 * (Ichorgo.gcpSignInNative, without the project list), whose sign-in is merged into the stored one
 * so its project and zone stay, then [withRenewal]'s caller seals it. One attempt per failure: a
 * renewal Google refuses (consent withdrawn, account gone) ends with the core's own "it has ended"
 * message, which asks to sign in again from the card, never a loop. The open-source builds'
 * [authorize] always fails, and they never hold such a sign-in.
 */
class GCPTokenRenewal(
    private val authorize: suspend () -> GoogleAuthorization,
    private val signIn: suspend (secrets: Map<String, String>) -> String,
    private val nowMs: () -> Long = System::currentTimeMillis,
) {
    /**
     * Runs [call] with [stored]; when the core says the app-held token ran out, renews it, hands the
     * renewed credential to [save] and runs [call] once more with it.
     */
    suspend fun <T> withRenewal(stored: String, save: suspend (String) -> Unit, call: suspend (String) -> T): T {
        val failure = try {
            return call(stored)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            e
        }
        if (signInNeeded(failure.message)?.reason != GOOGLE_NATIVE_RENEW_REASON) throw failure
        val renewed = renew(stored) ?: throw ended(failure)
        save(renewed)
        return call(renewed)
    }

    /**
     * The credential a run (a create, an image) starts with: [stored], renewed first (and handed to
     * [save]) when its token runs out within minutes, so the run does not stop half way for it.
     */
    suspend fun fresh(stored: String, save: suspend (String) -> Unit): String {
        val expiry = gcpNativeTokenExpiry(stored) ?: return stored
        if (expiry - nowMs() / 1000 >= RENEW_AHEAD_S) return stored
        return renew(stored)?.also { save(it) } ?: stored
    }

    private suspend fun renew(stored: String): String? {
        val token = withTimeoutOrNull(AUTHORIZE_TIMEOUT_MS) { authorize() } as? GoogleAuthorization.Token ?: return null
        val credential = try {
            signIn(googleSignInSecrets(token))
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            return null
        }
        return gcpMergeRenewed(stored, credential)
    }

    /** The core's message without its sign-in code: "…it has ended: sign in with Google again from the Google Cloud card". */
    private fun ended(failure: Exception): Exception {
        val message = failure.message.orEmpty().substringBefore(": $KUBE_SIGN_IN_REQUIRED").ifBlank { return failure }
        return IllegalStateException(message, failure)
    }
}
