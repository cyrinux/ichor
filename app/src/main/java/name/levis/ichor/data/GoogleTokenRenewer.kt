package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import name.levis.ichor.model.SignInNeeded
import name.levis.ichor.model.SignInRenewal
import name.levis.ichor.model.signInNeeded
import java.util.concurrent.ConcurrentHashMap

/** How long a silent Google authorization may take: past it, the renewal failed. */
private const val AUTHORIZE_TIMEOUT_MS = 10_000L

/** At most one renewal attempt per cluster within this: a refusal is not asked again at once. */
private const val ATTEMPT_INTERVAL_MS = 60_000L

/** A token with less than this left is renewed before the call rather than after it fails. */
private const val RENEW_AHEAD_S = 5 * 60L

/**
 * Renews the Google token of a cluster signed in with "Sign in with Google" (Play build) without
 * asking, wherever a call needs it: screens, background checks and widgets alike (see
 * [GoCall.kube]). Google gives that sign-in an access token only, about an hour long; the Go core
 * asks for a new one with the sign-in-required reason "google-native". A silent authorization is
 * not a sign-in: when Google wants the user (consent withdrawn, account gone), nothing happens and
 * the "Sign in again" banner stays. [authorize] asks Google silently; [store] hands the token to
 * the Go core (KubeAuthRepository.setCredentials). The open-source builds' [authorize] always
 * fails, so this is a no-op there.
 */
class GoogleTokenRenewer(
    private val authorize: suspend () -> GoogleAuthorization,
    private val store: suspend (context: String, secrets: Map<String, String>) -> Unit,
    private val nowMs: () -> Long = System::currentTimeMillis,
) {
    private data class Attempt(val atMs: Long, val renewed: Boolean)

    private val locks = ConcurrentHashMap<String, Mutex>()
    private val attempts = ConcurrentHashMap<String, Attempt>()

    /** When the token this renewer last got for a cluster expires, unix seconds. */
    private val expiries = ConcurrentHashMap<String, Long>()

    /**
     * Renews [context]'s token when [failure] says it expired; true when a new one is in the Go
     * core, so the failed call can run again. Two callers renew once: the second gets the first's
     * answer.
     */
    suspend fun renewIfNeeded(context: String, failure: Throwable?): Boolean = renewIfNeeded(context, signInNeeded(failure?.message))

    /** The same for a sign-in a screen already parsed out of its failure ([needed]). */
    suspend fun renewIfNeeded(context: String, needed: SignInNeeded?): Boolean =
        needed?.reason == GOOGLE_NATIVE_RENEW_REASON && renew(context)

    /** Renews [context]'s token before a call when the one this renewer got runs out within minutes. */
    suspend fun renewIfDue(context: String) {
        val expires = expiries[context] ?: return
        if (expires - nowMs() / 1000 < RENEW_AHEAD_S) renew(context)
    }

    /**
     * Renews the clusters [renewals] found out of token (KubeAuthRepository.renewAll, on foreground
     * and before a background check), so what comes next finds a fresh one.
     */
    suspend fun renewExpired(renewals: List<SignInRenewal>) {
        renewals.filter { it.kind == SignInRenewal.KIND_KUBE && it.signInRequired && !it.needsUser }.forEach { renew(it.context) }
    }

    /**
     * The sign-in of [context] changed hands (signed out, or signed in again, perhaps another way):
     * what this renewer knows of it is void, and [renewIfDue] must not bring a Google token back.
     */
    fun forget(context: String) {
        attempts.remove(context)
        expiries.remove(context)
    }

    private suspend fun renew(context: String): Boolean = locks.getOrPut(context) { Mutex() }.withLock {
        val last = attempts[context]
        if (last != null && nowMs() - last.atMs < ATTEMPT_INTERVAL_MS) return@withLock last.renewed
        val renewed = tryRenew(context)
        attempts[context] = Attempt(nowMs(), renewed)
        renewed
    }

    private suspend fun tryRenew(context: String): Boolean {
        val token = withTimeoutOrNull(AUTHORIZE_TIMEOUT_MS) { authorize() } as? GoogleAuthorization.Token ?: return false
        return try {
            store(context, googleSignInSecrets(token))
            expiries[context] = token.expiresAtUnix
            true
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            false
        }
    }
}
