package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * The Go core's AI provider that asks the cluster's Ollama through a port-forward
 * (KubeOllamaForward). Its stored "base URL" is the server it points at, [OllamaServerRef.text].
 */
const val OLLAMA_CLUSTER_PROVIDER = "ollama-cluster"

/** The provider's name in the settings and the AI screens (a brand: not translated). */
const val OLLAMA_BRAND = "Ollama"

/** An Ollama server of the open cluster: the Deployment or StatefulSet running it. */
data class OllamaServerRef(val namespace: String, val server: String) {
    /** As stored in the AI settings and shown: "namespace/server". */
    val text: String get() = "$namespace/$server"

    companion object {
        /** The stored form back; null for anything else (an empty setting, a URL). */
        fun parse(stored: String): OllamaServerRef? {
            val parts = stored.trim().split('/')
            if (parts.size != 2 || parts.any { it.isEmpty() || ':' in it }) return null
            return OllamaServerRef(parts[0], parts[1])
        }
    }
}

/** A running forward, as KubeOllamaForward and OllamaForwardState give it. */
@Serializable
data class OllamaForward(
    val baseUrl: String = "",
    val namespace: String = "",
    val server: String = "",
    val pod: String = "",
    val port: Int = 0,
) {
    /** "namespace/pod", what the settings say the forward goes to. */
    val target: String get() = "$namespace/$pod"
}
