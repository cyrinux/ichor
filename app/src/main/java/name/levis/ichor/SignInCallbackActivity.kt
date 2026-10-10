package name.levis.ichor

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Where a browser sign-in comes back to the app (ichor://signin?code=…&state=…, forwarded by
 * the redirect page of a Web OAuth client): the URL goes to the sign-in in progress, which
 * checks it against its own state, and the app comes back over the browser tab. Shows nothing.
 */
class SignInCallbackActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val uri = intent?.data
        if (intent?.action == Intent.ACTION_VIEW && uri?.scheme == "ichor" && uri.host == "signin") {
            (application as TalosApp).kubeAuthRepository.completeSignIn(uri.toString())
        }
        // Back to the app, closing the browser tab above it.
        startActivity(Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP))
        finish()
    }
}
