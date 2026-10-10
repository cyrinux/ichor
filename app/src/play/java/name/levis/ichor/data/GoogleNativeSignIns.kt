package name.levis.ichor.data

import android.content.Context
import android.content.Intent
import com.google.android.gms.auth.api.identity.AuthorizationRequest
import com.google.android.gms.auth.api.identity.AuthorizationResult
import com.google.android.gms.auth.api.identity.Identity
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.android.gms.common.api.Scope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume

/** Google Identity Services' AuthorizationClient: the account picker and consent, then a token. */
fun createGoogleNativeSignIn(): GoogleNativeSignIn = PlayGoogleNativeSignIn

private object PlayGoogleNativeSignIn : GoogleNativeSignIn {
    private val request: AuthorizationRequest = AuthorizationRequest.builder()
        .setRequestedScopes(listOf(Scope("https://www.googleapis.com/auth/cloud-platform"), Scope("email")))
        .build()

    override fun available(context: Context): Boolean =
        GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(context) == ConnectionResult.SUCCESS

    override suspend fun authorize(context: Context, silent: Boolean): GoogleAuthorization = try {
        val result = suspendCancellableCoroutine { cont ->
            Identity.getAuthorizationClient(context).authorize(request)
                .addOnSuccessListener { cont.resume(Result.success(it)) }
                .addOnFailureListener { cont.resume(Result.failure(it)) }
        }.getOrThrow()
        val pending = result.pendingIntent
        when {
            !result.hasResolution() -> token(result)
            silent || pending == null -> GoogleAuthorization.Failed("")
            else -> GoogleAuthorization.NeedsUser(pending.intentSender)
        }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        GoogleAuthorization.Failed(e.message.orEmpty())
    }

    override fun fromResolution(context: Context, data: Intent?): GoogleAuthorization = try {
        token(Identity.getAuthorizationClient(context).getAuthorizationResultFromIntent(data))
    } catch (e: Exception) {
        GoogleAuthorization.Failed(e.message.orEmpty())
    }

    private fun token(result: AuthorizationResult): GoogleAuthorization {
        val accessToken = result.accessToken ?: return GoogleAuthorization.Failed("")
        return GoogleAuthorization.Token(accessToken, googleTokenExpiry(), result.toGoogleSignInAccount()?.email.orEmpty())
    }
}
