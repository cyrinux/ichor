package name.levis.ichor.ui.plugins

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.OLLAMA_CLUSTER_PROVIDER
import name.levis.ichor.model.OllamaServerRef
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/**
 * "Use for AI diagnosis" under an Ollama server row: opens a port-forward to a ready pod of
 * [namespace]/[server] and makes it the AI provider (Settings, AI diagnosis shows it). The demo
 * cluster refuses it with the Go core's message.
 */
@Composable
internal fun OllamaAiButton(namespace: String, server: String) {
    val app = LocalContext.current.applicationContext as TalosApp
    val settings by app.aiPreferences.settings.collectAsStateWithLifecycle()
    val ref = remember(namespace, server) { OllamaServerRef(namespace, server) }
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<Pair<String, Boolean>?>(null) }
    val inUse = settings.enabled && settings.provider == OLLAMA_CLUSTER_PROVIDER && settings.baseUrl == ref.text

    Column {
        IchorOutlinedButton(
            enabled = !busy && !inUse,
            onClick = {
                busy = true
                message = null
                scope.launch {
                    message = runCatching { app.aiForward.use(ref) }.fold(
                        onSuccess = { app.getString(R.string.ai_cluster_server_done, ref.text) to true },
                        onFailure = { it.userMessage() to false },
                    )
                    busy = false
                }
            },
        ) { Text(stringResource(R.string.ai_cluster_server_use)) }
        message?.let { (text, ok) ->
            val colors = LocalStatusColors.current
            Text(text, color = if (ok) colors.ok else colors.bad, style = MaterialTheme.typography.bodySmall)
        }
    }
}
