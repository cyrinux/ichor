package name.levis.ichor.ui.settings

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AiProvider
import name.levis.ichor.data.AiSettings
import name.levis.ichor.model.OllamaServerRef
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/**
 * The cluster's Ollama as the AI server: which server, whether its port-forward runs (and to
 * which pod), a reconnect when it does not, and the model picker through the forward. The
 * server is picked on its row in Data services, Ollama.
 */
@Composable
internal fun ClusterServerSettings(app: TalosApp, settings: AiSettings, provider: AiProvider) {
    val ref = OllamaServerRef.parse(settings.baseUrl)
    val forward by app.aiForward.forward.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var connecting by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // It may have ended while the app was in the background.
    LaunchedEffect(Unit) { app.aiForward.refresh() }

    if (ref == null) {
        MutedText(stringResource(R.string.ai_cluster_server_pick))
        return
    }
    Text(stringResource(R.string.ai_cluster_server, ref.text), style = MaterialTheme.typography.bodyMedium)
    val active = forward?.takeIf { it.namespace == ref.namespace && it.server == ref.server }
    if (active != null) {
        StatusPill(stringResource(R.string.ai_cluster_server_active, active.target), LocalStatusColors.current.ok)
    } else {
        IchorTextButton(
            enabled = !connecting,
            onClick = {
                connecting = true
                error = null
                scope.launch {
                    runCatching { app.aiForward.connect(ref) }.onFailure { error = it.userMessage() }
                    connecting = false
                }
            },
        ) { Text(stringResource(R.string.ai_cluster_server_lost)) }
    }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
    ModelField(app, settings, provider, apiKey = "")
    MutedText(stringResource(R.string.ai_cluster_server_pick))
}
