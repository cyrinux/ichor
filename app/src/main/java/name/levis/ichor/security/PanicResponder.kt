package name.levis.ichor.security

/**
 * The panic trigger protocol (PanicKit, the one Ripple speaks), without the library: its only
 * release is from 2021 and built on the deprecated android.preference. Same action strings and
 * handshake, see https://guardianproject.info/code/panickit/ and its Panic.java.
 */
object PanicActions {
    const val TRIGGER = "info.guardianproject.panic.action.TRIGGER"
    const val CONNECT = "info.guardianproject.panic.action.CONNECT"
    const val DISCONNECT = "info.guardianproject.panic.action.DISCONNECT"
}

enum class PanicResponse { LOCK, DELETE_CONFIG }

sealed interface PanicRequest {
    data class Trigger(val response: PanicResponse) : PanicRequest
    data class Connect(val callingPackage: String) : PanicRequest
    data object Disconnect : PanicRequest
    data object Ignore : PanicRequest
}

/**
 * What an incoming intent asks for. [callingPackage] is Activity.getCallingActivity()'s package:
 * the only proof of the sender, absent when it used plain startActivity. Whatever cannot be
 * proven to come from the connected [trustedPackage] can only lock, never delete the config.
 */
fun panicRequest(action: String?, callingPackage: String?, trustedPackage: String?, configured: PanicResponse): PanicRequest {
    val caller = callingPackage?.takeIf { it.isNotBlank() }
    val fromTrusted = caller != null && caller == trustedPackage
    return when (action) {
        PanicActions.TRIGGER -> PanicRequest.Trigger(if (fromTrusted) configured else PanicResponse.LOCK)
        PanicActions.CONNECT -> if (caller != null) PanicRequest.Connect(caller) else PanicRequest.Ignore
        PanicActions.DISCONNECT -> if (fromTrusted) PanicRequest.Disconnect else PanicRequest.Ignore
        else -> PanicRequest.Ignore
    }
}
