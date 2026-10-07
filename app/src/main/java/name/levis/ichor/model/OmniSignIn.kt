package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/omni_auth.go (omniSignInInfo, OmniSignInRequired): a talosconfig context
// reached through Omni signs every request with a service account key or a browser sign-in.

/** ContextSummary.auth of a context reached through Omni (config.go authOmni). */
const val AUTH_OMNI = "omni"

/** Reached through Omni: no client certificate, so no roles nor expiry; Omni decides what is allowed. */
val ContextSummary.isOmni: Boolean get() = !isKube && auth == AUTH_OMNI

/** How a stored Omni context signs in (OmniSignInInfo). */
@Serializable
data class OmniSignInInfo(
    val signedIn: Boolean = false,
    /** [METHOD_SERVICE_ACCOUNT] or [METHOD_BROWSER] when signed in. */
    val method: String = "",
    /** The service account, or the identity a browser key signs as. */
    val user: String = "",
    /** The context's own email; a browser sign-in needs one. */
    val identity: String = "",
    /** The Omni instance URL. */
    val instance: String = "",
    /** When a browser key expires, unix seconds; 0 for a service account. */
    val sessionExpires: Long = 0,
) {
    companion object {
        const val METHOD_SERVICE_ACCOUNT = "omni-service-account"
        const val METHOD_BROWSER = "omni-browser"
    }
}

/** The code a Go core error starts with when an Omni cluster needs a key (Ichorgo.OmniSignInRequired). */
const val OMNI_SIGN_IN_REQUIRED = "omni-sign-in-required"

private const val OMNI_SIGN_IN_PREFIX = "$OMNI_SIGN_IN_REQUIRED: sign in to Omni"

/**
 * Why a failed call asks for an Omni sign-in, from its error [message] ("" when the core gave
 * no reason), null when it failed for another reason. The code may follow what wrapped it.
 */
fun omniSignInNeeded(message: String?): String? {
    val at = message?.indexOf(OMNI_SIGN_IN_REQUIRED) ?: -1
    if (at < 0) return null
    val text = message!!.substring(at).trim()
    return text.removePrefix(OMNI_SIGN_IN_PREFIX).removePrefix(OMNI_SIGN_IN_REQUIRED).trimStart(':', ' ')
}
