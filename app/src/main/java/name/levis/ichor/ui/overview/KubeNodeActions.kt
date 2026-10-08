package name.levis.ichor.ui.overview

import android.content.Context
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.MaintenanceManager
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.ui.node.CordonDialog
import name.levis.ichor.ui.uiText

/**
 * The cordon or uncordon of a Kubernetes node (a cluster without Talos), from its screen: the
 * confirmation for [target] (null for none), the call, and how it went on [snackbar]; then
 * [onCordoned] reloads the nodes. [onDismiss] clears [target].
 */
@Composable
internal fun KubeCordonDialog(target: KubeNodeInfo?, snackbar: SnackbarHostState, onCordoned: () -> Unit, onDismiss: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    target?.let { node ->
        CordonDialog(
            hostname = node.name,
            cordoned = node.cordoned,
            onConfirm = { on ->
                onDismiss()
                scope.launch {
                    val message = cordonKubeNode(app.maintenanceManager, context, node.name, on)
                    onCordoned()
                    snackbar.showSnackbar(message, withDismissAction = true, duration = SnackbarDuration.Long)
                }
            },
            onDismiss = onDismiss,
        )
    }
}

/** Cordons ([on]) or uncordons the Kubernetes node [name]; returns the message to show. */
internal suspend fun cordonKubeNode(maintenances: MaintenanceManager, context: Context, name: String, on: Boolean): String =
    runCatching { maintenances.cordon(name, on) }.fold(
        onSuccess = { context.getString(if (on) R.string.cordon_done else R.string.uncordon_done, name) },
        onFailure = { context.getString(R.string.cordon_failed, name, it.uiText().resolve(context)) },
    )
