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
import name.levis.ichor.model.isKube
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
 * Signs the stored kubeconfig clusters in (OIDC, EKS, GKE, AKS, DigitalOcean, Rancher), and the
 * talosconfig clusters managed by Sidero Omni (browser or service account key), and
 * finds clusters in a cloud account, through the Go core. What a sign-in keeps goes to the
 * [KubeAuthStore] the app registered. Only screens the user opened call this: background
 * checks and widgets never start a sign-in, their calls just fail with "sign-in required".
 */
class KubeAuthRepository(private val configs: ConfigRepository) {

    /** How the stored cluster [context] signs in; null for static credentials (or a Talos certificate). */
    suspend fun info(context: String): KubeSignInInfo? = withContext(Dispatchers.IO) {
        val json = if (isTalos(context)) Ichorgo.talosSignInInfo(talosYaml(), context) else Ichorgo.kubeSignInInfo(kubeYaml(), context)
        json.takeIf { it.isNotBlank() }?.let { TalosJson.decodeFromString(KubeSignInInfo.serializer(), it) }
    }

    /**
     * Signs [context] in with the [secrets] the user entered (checked by getting a token).
     * Throws with the core's message, which starts with the sign-in-required code when the
     * method goes on in the browser (EKS with IAM Identity Center).
     */
    suspend fun setCredentials(context: String, secrets: Map<String, String>) = withContext(Dispatchers.IO) {
        if (isTalos(context)) {
            Ichorgo.talosSetCredentials(talosYaml(), context, encodeSecrets(secrets))
        } else {
            Ichorgo.kubeSetCredentials(kubeYaml(), context, encodeSecrets(secrets))
        }
    }

    /** Signs [context] in in the browser or with a device code; cancelling the collector stops it. */
    fun signIn(context: String): Flow<SignInEvent> = callbackFlow {
        val listener = object : SignInListener {
            override fun onPrompt(json: String) {
                runCatching { TalosJson.decodeFromString(SignInPrompt.serializer(), json) }
                    .onSuccess { trySend(SignInEvent.Prompt(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(SignInEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        }
        val run = if (isTalos(context)) {
            Ichorgo.startTalosSignIn(talosYaml(), context, listener)
        } else {
            Ichorgo.startKubeSignIn(kubeYaml(), context, listener)
        }
        awaitClose { run.cancel() }
    }

    /** Forgets [context]'s sign-in (its tokens and the secrets entered). */
    suspend fun signOut(context: String) = withContext(Dispatchers.IO) {
        if (isTalos(context)) Ichorgo.talosSignOut(talosYaml(), context) else Ichorgo.kubeSignOut(kubeYaml(), context)
    }

    /** Signs the account [email] in to the Omni instance at [endpoint] in the browser; cancelling stops it. */
    fun omniSignIn(endpoint: String, email: String): Flow<SignInEvent> = callbackFlow {
        val run = Ichorgo.startOmniAccountSignIn(
            endpoint,
            email,
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

    /** Checks and stores an Omni service account [key]; returns the identity it signs as. */
    suspend fun omniServiceAccount(endpoint: String, key: String): String = withContext(Dispatchers.IO) {
        Ichorgo.setOmniServiceAccount(endpoint, key)
    }

    /** A talosconfig of the clusters [identity] sees on the Omni instance at [endpoint], for the preview. */
    suspend fun discoverOmni(endpoint: String, identity: String): String = withContext(Dispatchers.IO) {
        Ichorgo.discoverOmniClusters(endpoint, identity)
    }

    /** The fields each cloud's discovery asks for, by provider id. */
    suspend fun discoveryFields(): Map<String, List<String>> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(MapSerializer(String.serializer(), ListSerializer(String.serializer())), Ichorgo.kubeDiscoverFields())
    }

    /** The providers whose discovery takes one of several credentials, each a field set, by provider id. */
    suspend fun discoveryOptions(): Map<String, List<List<String>>> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(
            MapSerializer(String.serializer(), ListSerializer(ListSerializer(String.serializer()))),
            Ichorgo.kubeDiscoverOptions(),
        )
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

    /** Whether [context] is a talosconfig context (Omni signs those in); names are unique across both configs. */
    private fun isTalos(context: String): Boolean =
        configs.config.value?.summary?.contexts?.any { it.name == context && !it.isKube } == true

    private fun talosYaml(): String = configs.config.value?.talosYaml?.takeIf { it.isNotBlank() } ?: throw NoConfigException()

    private fun kubeYaml(): String = configs.config.value?.kubeYaml?.takeIf { it.isNotBlank() } ?: throw NoConfigException()

    private fun encodeSecrets(secrets: Map<String, String>): String =
        TalosJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), secrets)
}
