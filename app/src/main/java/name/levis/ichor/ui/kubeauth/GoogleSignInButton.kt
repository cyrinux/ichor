package name.levis.ichor.ui.kubeauth

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.IntentSenderRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.GoogleAuthorization
import name.levis.ichor.data.GoogleNativeSignIn

/**
 * Starts Google's sign-in (Play build): a token at once when the user already allowed the
 * app, else Google's account picker and consent, whose answer [onAnswer] gets too.
 */
@Composable
internal fun rememberGoogleSignIn(google: GoogleNativeSignIn, onAnswer: (GoogleAuthorization, String) -> Unit): () -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val failedText = stringResource(R.string.kube_signin_google_unavailable)
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.StartIntentSenderForResult()) { result ->
        onAnswer(google.fromResolution(context, result.data), failedText)
    }
    return {
        scope.launch {
            when (val answer = google.authorize(context, silent = false)) {
                is GoogleAuthorization.NeedsUser -> picker.launch(IntentSenderRequest.Builder(answer.intentSender).build())
                else -> onAnswer(answer, failedText)
            }
        }
    }
}
