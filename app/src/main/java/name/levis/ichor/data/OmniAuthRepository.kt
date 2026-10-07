package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import name.levis.ichor.model.OmniSignInInfo
import name.levis.ichor.model.SignInPrompt
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.SignInListener

/**
 * Signs the stored Omni clusters (talosconfig contexts reached through Omni) in, through the
 * Go core: a service account key, or a key approved in the browser. What it keeps goes to the
 * [KubeAuthStore] the app registered, under the context's fingerprint. As for kubeconfig
 * clusters, only screens the user opened call this.
 */
class OmniAuthRepository(private val configs: ConfigRepository) {

    /** How the stored Omni context [context] is signed in. */
    suspend fun info(context: String): OmniSignInInfo = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(OmniSignInInfo.serializer(), Ichorgo.omniSignInInfo(talosYaml(), context))
    }

    /** Stores the service account [key] (base64, as Omni shows it) for [context]; throws when it is not one. */
    suspend fun setServiceAccount(context: String, key: String) = withContext(Dispatchers.IO) {
        Ichorgo.omniSetServiceAccount(talosYaml(), context, key)
    }

    /** Signs [context] in in the browser; cancelling the collector stops it. */
    fun signIn(context: String): Flow<SignInEvent> = callbackFlow {
        val run = Ichorgo.startOmniSignIn(
            talosYaml(),
            context,
            object : SignInListener {
                override fun onPrompt(json: String) {
                    runCatching { TalosJson.decodeFromString(SignInPrompt.serializer(), json) }
                        .onSuccess { trySend(SignInEvent.Prompt(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(SignInEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }

    /** Forgets [context]'s key. */
    suspend fun signOut(context: String) = withContext(Dispatchers.IO) { Ichorgo.omniSignOut(talosYaml(), context) }

    private fun talosYaml(): String = configs.config.value?.talosYaml?.takeIf { it.isNotBlank() } ?: throw NoConfigException()
}
