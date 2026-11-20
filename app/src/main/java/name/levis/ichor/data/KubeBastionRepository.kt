package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.BastionFields
import name.levis.ichor.model.KubeBastionHostKey
import name.levis.ichor.model.KubeBastionInfo
import name.levis.ichorgo.Ichorgo

/**
 * A cluster's SSH bastion (a kubeconfig or a Talos cluster), through the Go core: what is set, the host key a bastion
 * presents, set (checked for real first) and remove. The core keeps the setting, key included,
 * in the sealed auth store under the context's bastionKey; the app only ever sees the
 * fingerprint.
 */
class KubeBastionRepository(private val configs: ConfigRepository) {
    companion object {
        private val _revision = MutableStateFlow(0)

        /** Moves on each set or removal (any instance): the cluster list reads the bastions again. */
        val revision: StateFlow<Int> = _revision.asStateFlow()
    }

    /** The bastion of the stored cluster [context], or why it cannot have one. */
    suspend fun info(context: String): KubeBastionInfo = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(KubeBastionInfo.serializer(), Ichorgo.kubeBastionInfo(yamlFor(context), context))
    }

    /** The host key the bastion at [host] (host or host:port) presents, without signing in. */
    suspend fun hostKey(host: String): KubeBastionHostKey = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(KubeBastionHostKey.serializer(), Ichorgo.kubeBastionHostKey(host.trim()))
    }

    /**
     * Sets [context]'s bastion from [fields] after the core checked the pin, the sign-in and a
     * connection to the API server. Throws with the core's message (a kube-bastion-* code).
     */
    suspend fun set(context: String, fields: BastionFields): KubeBastionInfo = withContext(Dispatchers.IO) {
        val json = TalosJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), fields.toMap())
        TalosJson.decodeFromString(KubeBastionInfo.serializer(), Ichorgo.kubeSetBastion(yamlFor(context), context, json))
            .also { _revision.update { rev -> rev + 1 } }
    }

    /** Forgets [context]'s bastion: it connects directly again. */
    suspend fun remove(context: String) = withContext(Dispatchers.IO) {
        Ichorgo.kubeRemoveBastion(yamlFor(context), context)
        _revision.update { it + 1 }
    }

    /** The stored config holding [context]: its kubeconfig or its talosconfig. */
    private fun yamlFor(context: String): String =
        configs.config.value?.yamlFor(context)?.takeIf { it.isNotBlank() } ?: throw NoConfigException()
}
