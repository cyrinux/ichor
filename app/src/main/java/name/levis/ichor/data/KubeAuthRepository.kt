package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInPrompt
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.SignInListener
import name.levis.ichor.ui.goErrorText

/** What an interactive sign-in reports: a prompt to show, then its end ([error] null on success or cancel). */
sealed interface SignInEvent {
    data class Prompt(val prompt: SignInPrompt) : SignInEvent
    data class Done(val error: String?) : SignInEvent
}

/**
 * Signs the stored kubeconfig clusters in (OIDC, EKS, GKE, AKS, DigitalOcean, Rancher) and
 * finds clusters in a cloud account, through the Go core. What a sign-in keeps goes to the
 * [KubeAuthStore] the app registered. Only screens the user opened call this: background
 * checks and widgets never start a sign-in, their calls just fail with "sign-in required".
 */
class KubeAuthRepository(private val configs: ConfigRepository) {

    /** How the stored kubeconfig cluster [context] signs in; null for static credentials. */
    suspend fun info(context: String): KubeSignInInfo? = withContext(Dispatchers.IO) {
        val json = Ichorgo.kubeSignInInfo(kubeYaml(), context)
        json.takeIf { it.isNotBlank() }?.let { TalosJson.decodeFromString(KubeSignInInfo.serializer(), it) }
    }

    /**
     * Signs [context] in with the [secrets] the user entered (checked by getting a token).
     * Throws with the core's message, which starts with the sign-in-required code when the
     * method goes on in the browser (EKS with IAM Identity Center).
     */
    suspend fun setCredentials(context: String, secrets: Map<String, String>) = withContext(Dispatchers.IO) {
        Ichorgo.kubeSetCredentials(kubeYaml(), context, encodeSecrets(secrets))
    }

    /** Signs [context] in in the browser or with a device code; cancelling the collector stops it. */
    fun signIn(context: String): Flow<SignInEvent> = callbackFlow {
        val run = Ichorgo.startKubeSignIn(
            kubeYaml(),
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

    /** Forgets [context]'s sign-in (its tokens and the secrets entered). */
    suspend fun signOut(context: String) = withContext(Dispatchers.IO) { Ichorgo.kubeSignOut(kubeYaml(), context) }

    /** The fields each cloud's discovery asks for, by provider id. */
    suspend fun discoveryFields(): Map<String, List<String>> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(MapSerializer(String.serializer(), ListSerializer(String.serializer())), Ichorgo.kubeDiscoverFields())
    }

    /** A kubeconfig of the clusters the [provider] account reaches with [secrets], for the import preview. */
    suspend fun discover(provider: String, secrets: Map<String, String>): String = withContext(Dispatchers.IO) {
        Ichorgo.discoverClusters(provider, encodeSecrets(secrets))
    }

    /**
     * Signs the contexts [names] in with the [secrets] that found them (discovery), when they
     * sign in with credentials. Best effort: one that cannot (an AKS user kubeconfig that
     * needs a device sign-in, keys refused) shows "Sign in" on its home instead.
     */
    suspend fun signInDiscovered(names: List<String>, secrets: Map<String, String>) {
        val stored = configs.config.value ?: return
        names.filter { name -> stored.summary.contexts.any { it.name == name && it.signIn.isNotEmpty() } }.forEach { name ->
            runCatching { setCredentials(name, secrets) }
        }
    }

    private fun kubeYaml(): String = configs.config.value?.kubeYaml?.takeIf { it.isNotBlank() } ?: throw NoConfigException()

    private fun encodeSecrets(secrets: Map<String, String>): String =
        TalosJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), secrets)
}
