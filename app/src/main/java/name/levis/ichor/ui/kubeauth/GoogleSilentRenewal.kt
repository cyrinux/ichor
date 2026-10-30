package name.levis.ichor.ui.kubeauth

import name.levis.ichor.signInChanged
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import name.levis.ichor.TalosApp
import name.levis.ichor.model.SignInNeeded

/**
 * Renews the Google token of [context] without asking (Play build's "Sign in with Google"):
 * when a call failed because the token the app holds expired ([needed] with reason
 * "google-native"), one silent attempt per failure through the app's GoogleTokenRenewer (which
 * Kubernetes calls already use; this catches a failure they could not renew), then [onRenewed].
 * Google refusing (consent withdrawn, account gone) leaves the "Sign in again" banner as it is.
 */
@Composable
fun GoogleSilentRenewal(context: String, needed: SignInNeeded?, onRenewed: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    LaunchedEffect(context, needed) {
        if (!app.googleTokenRenewer.renewIfNeeded(context, needed)) return@LaunchedEffect
        app.signInChanged()
        onRenewed()
    }
}
