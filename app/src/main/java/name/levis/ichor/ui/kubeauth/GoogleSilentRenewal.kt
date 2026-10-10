package name.levis.ichor.ui.kubeauth

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.CancellationException
import name.levis.ichor.TalosApp
import name.levis.ichor.data.GOOGLE_NATIVE_RENEW_REASON
import name.levis.ichor.data.GoogleAuthorization
import name.levis.ichor.data.googleSignInSecrets
import name.levis.ichor.model.SignInNeeded

/**
 * Renews the Google token of [context] without asking (Play build's "Sign in with Google"):
 * when a call failed because the token the app holds expired ([needed] with reason
 * "google-native"), one silent attempt per failure, then [onRenewed]. Google refusing (consent
 * withdrawn, account gone) leaves the "Sign in again" banner as it is.
 */
@Composable
fun GoogleSilentRenewal(context: String, needed: SignInNeeded?, onRenewed: () -> Unit) {
    val ui = LocalContext.current
    val app = ui.applicationContext as TalosApp
    LaunchedEffect(context, needed) {
        if (needed?.reason != GOOGLE_NATIVE_RENEW_REASON) return@LaunchedEffect
        val token = app.googleNativeSignIn.authorize(ui, silent = true) as? GoogleAuthorization.Token ?: return@LaunchedEffect
        try {
            app.kubeAuthRepository.setCredentials(context, googleSignInSecrets(token))
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            return@LaunchedEffect
        }
        app.signInChanged()
        onRenewed()
    }
}
