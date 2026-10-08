package name.levis.ichor.security

import android.content.Context
import android.content.ContextWrapper
import android.os.Build
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_STRONG
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_WEAK
import androidx.biometric.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import kotlinx.coroutines.suspendCancellableCoroutine
import name.levis.ichor.TalosApp
import kotlin.coroutines.resume

sealed interface AuthResult {
    data object Success : AuthResult
    data class Failure(val message: String) : AuthResult
}

/** Fingerprint/face, falling back to the device PIN, pattern or password. */
private fun allowedAuthenticators(): Int =
    // BIOMETRIC_STRONG | DEVICE_CREDENTIAL is unsupported before Android 11.
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) BIOMETRIC_STRONG or DEVICE_CREDENTIAL
    else BIOMETRIC_WEAK or DEVICE_CREDENTIAL

/** True when the device has a fingerprint or a screen lock the prompt can use. */
fun canAuthenticate(context: Context): Boolean =
    BiometricManager.from(context).canAuthenticate(allowedAuthenticators()) == BiometricManager.BIOMETRIC_SUCCESS

/**
 * Verifies the user: with a security key enrolled (SecurityKeys.kt) the "tap your key" prompt,
 * which also offers the fingerprint unless the key is required; otherwise the fingerprint /
 * device-credential prompt. Every check in the app (unlock, reboot, export…) goes through here.
 */
suspend fun authenticate(activity: FragmentActivity, title: String, subtitle: String? = null): AuthResult {
    val app = activity.application as? TalosApp
    val enrolment = app?.appLock?.securityKeys?.value
    if (app == null || enrolment == null) return biometricPrompt(activity, title, subtitle)
    return app.keyPrompts.request(title, subtitle, allowBiometric = !enrolment.required && canAuthenticate(activity))
}

/** The system prompt: fingerprint/face, falling back to the device PIN, pattern or password. */
suspend fun biometricPrompt(activity: FragmentActivity, title: String, subtitle: String? = null): AuthResult =
    suspendCancellableCoroutine { cont ->
        val prompt = BiometricPrompt(
            activity,
            ContextCompat.getMainExecutor(activity),
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    if (cont.isActive) cont.resume(AuthResult.Success)
                }

                override fun onAuthenticationError(errorCode: Int, errString: CharSequence) {
                    if (cont.isActive) cont.resume(AuthResult.Failure(errString.toString()))
                }
                // onAuthenticationFailed (unrecognised finger) is retried inside the prompt.
            },
        )
        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle(title)
            .apply { subtitle?.let(::setSubtitle) }
            .setAllowedAuthenticators(allowedAuthenticators())
            .build()
        prompt.authenticate(info)
        cont.invokeOnCancellation { prompt.cancelAuthentication() }
    }

tailrec fun Context.findFragmentActivity(): FragmentActivity? = when (this) {
    is FragmentActivity -> this
    is ContextWrapper -> baseContext.findFragmentActivity()
    else -> null
}
