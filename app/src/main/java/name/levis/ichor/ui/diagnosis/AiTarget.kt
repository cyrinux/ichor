package name.levis.ichor.ui.diagnosis

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AiSettings
import name.levis.ichor.data.canAsk
import name.levis.ichor.localmodel.LlamaNative
import name.levis.ichor.model.OLLAMA_BRAND
import name.levis.ichor.model.OLLAMA_CLUSTER_PROVIDER
import name.levis.ichor.model.OllamaServerRef

/**
 * Who answers, as the AI screens name it ("Ask %1$s", "Waiting for %1$s…"), whether a
 * question can be asked now, and what to say when it cannot.
 */
data class AiTarget(val name: String, val askable: Boolean, val onPhone: Boolean, val inCluster: Boolean = false) {
    /** Why the Ask button is missing: no API key, no model on the phone, or no server of the cluster picked. */
    val notReadyText: Int get() = when {
        onPhone -> R.string.ai_local_not_ready
        inCluster -> R.string.ai_cluster_server_pick
        else -> R.string.ai_no_key
    }
}

/** Read on every composition: a key or a model may have been added in Settings a moment ago. */
@Composable
fun aiTarget(app: TalosApp, settings: AiSettings): AiTarget {
    val provider = app.diagnosisRepository.providers.firstOrNull { it.id == settings.provider }
    if (settings.provider == OLLAMA_CLUSTER_PROVIDER) {
        // No key: a server picked is enough, its forward opens when asked.
        return AiTarget(OLLAMA_BRAND, OllamaServerRef.parse(settings.baseUrl) != null, onPhone = false, inCluster = true)
    }
    if (settings.provider != AiSettings.DEVICE_PROVIDER) {
        return AiTarget(
            name = provider?.name ?: settings.provider,
            askable = canAsk(app.aiPreferences.apiKey(settings.provider), settings.baseUrl),
            onPhone = false,
        )
    }
    val changes by app.localModels.changes.collectAsStateWithLifecycle()
    val model = settings.model.ifEmpty { provider?.defaultModel.orEmpty() }
    val ready = remember(changes, model) { model.isNotEmpty() && LlamaNative.available && app.localModels.isReady(model) }
    return AiTarget(stringResource(R.string.ai_provider_device_answer), ready, onPhone = true)
}
