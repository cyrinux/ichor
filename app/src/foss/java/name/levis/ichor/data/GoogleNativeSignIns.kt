package name.levis.ichor.data

import android.content.Context
import android.content.Intent

/** The open-source builds carry no Play services: the other GKE sign-ins remain. */
fun createGoogleNativeSignIn(): GoogleNativeSignIn = NoGoogleNativeSignIn

private object NoGoogleNativeSignIn : GoogleNativeSignIn {
    override fun available(context: Context) = false

    override suspend fun authorize(context: Context, silent: Boolean): GoogleAuthorization = GoogleAuthorization.Failed("")

    override fun fromResolution(context: Context, data: Intent?): GoogleAuthorization = GoogleAuthorization.Failed("")
}
