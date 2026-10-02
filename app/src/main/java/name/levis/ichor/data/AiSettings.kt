package name.levis.ichor.data

import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable

/** A model provider the AI diagnosis can use, as listed by the Go core. */
@Serializable
data class AiProvider(val id: String, val name: String = "", val defaultModel: String = "", val keyUrl: String = "")

@Serializable
data class AiModel(val id: String, val name: String = "")

/**
 * The AI diagnosis is optional and off until the user turns it on. The model and the server
 * URL are kept per provider, so switching provider and back loses nothing.
 */
data class AiSettings(
    val enabled: Boolean = false,
    val provider: String = DEFAULT_PROVIDER,
    val models: Map<String, String> = emptyMap(),
    val baseUrls: Map<String, String> = emptyMap(),
    /** Replace node names, addresses and domains with placeholders in what is sent. */
    val anonymize: Boolean = true,
) {
    /** Empty: the provider's default model. */
    val model: String get() = models[provider].orEmpty()

    /** Empty: the provider's own API. */
    val baseUrl: String get() = baseUrls[provider].orEmpty()

    fun withModel(model: String): AiSettings = copy(models = models + (provider to model.trim()))

    fun withBaseUrl(url: String): AiSettings = copy(baseUrls = baseUrls + (provider to url.trim()))

    companion object {
        const val DEFAULT_PROVIDER = "anthropic"
    }
}

/** The answer can be asked in the app with a key, or with a server that needs none. */
fun canAsk(apiKey: String, baseUrl: String): Boolean = apiKey.isNotBlank() || baseUrl.isNotBlank()

/** What the model is told when the diagnosis is opened from a failed health check (sent as is, so in English). */
fun healthCheckNote(error: String): String = "The cluster health check failed: ${error.trim()}"

/** Settings in preferences, API keys encrypted with a Keystore key, one [SecureStore] per provider. */
class AiPreferences(
    private val prefs: SharedPreferences,
    private val providerIds: List<String>,
    private val keyStore: (provider: String) -> SecureStore,
) {
    private val _settings = MutableStateFlow(read())
    val settings: StateFlow<AiSettings> = _settings.asStateFlow()

    private val keys = HashMap<String, String>()

    fun update(settings: AiSettings) {
        prefs.edit().apply {
            putBoolean(KEY_ENABLED, settings.enabled)
            putString(KEY_PROVIDER, settings.provider)
            putBoolean(KEY_ANONYMIZE, settings.anonymize)
            providerIds.forEach { id ->
                putString(KEY_MODEL + id, settings.models[id].orEmpty())
                putString(KEY_BASE_URL + id, settings.baseUrls[id].orEmpty())
            }
        }.apply()
        _settings.value = settings
    }

    /** The provider's API key, "" when none is stored (or it can no longer be decrypted). */
    @Synchronized
    fun apiKey(provider: String): String = keys.getOrPut(provider) {
        runCatching { keyStore(provider).read()?.decodeToString() }.getOrNull().orEmpty()
    }

    /** Stores the key encrypted; an empty key removes it and its Keystore key. */
    @Synchronized
    fun setApiKey(provider: String, key: String) {
        val trimmed = key.trim()
        if (trimmed.isEmpty()) keyStore(provider).clear() else keyStore(provider).write(trimmed.encodeToByteArray())
        keys[provider] = trimmed
    }

    private fun read() = AiSettings(
        enabled = prefs.getBoolean(KEY_ENABLED, false),
        provider = prefs.getString(KEY_PROVIDER, null)?.takeIf { it in providerIds } ?: AiSettings.DEFAULT_PROVIDER,
        models = providerIds.associateWith { prefs.getString(KEY_MODEL + it, "").orEmpty() }.filterValues { it.isNotEmpty() },
        baseUrls = providerIds.associateWith { prefs.getString(KEY_BASE_URL + it, "").orEmpty() }.filterValues { it.isNotEmpty() },
        anonymize = prefs.getBoolean(KEY_ANONYMIZE, true),
    )

    companion object {
        const val FILE = "talosdev-mobile-ai"
        private const val KEY_ENABLED = "enabled"
        private const val KEY_PROVIDER = "provider"
        private const val KEY_ANONYMIZE = "anonymize"
        private const val KEY_MODEL = "model_"
        private const val KEY_BASE_URL = "base_url_"
    }
}
