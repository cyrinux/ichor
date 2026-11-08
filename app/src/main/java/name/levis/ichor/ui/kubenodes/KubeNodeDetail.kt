package name.levis.ichor.ui.kubenodes

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.ui.kubebrowser.KubeObjectScreen
import name.levis.ichor.ui.overview.KubeNodeActionRow
import name.levis.ichor.ui.overview.KubeNodeCordonDialog

/**
 * A Kubernetes node beside the node list: its object screen (summary with conditions,
 * capacity, taints and labels, events, then YAML), with the node menu's actions as a row at
 * the top. Cordon confirms as from the menu and [onCordoned] reloads the list; the drain, the
 * taints and labels editor and the debug shell open full-screen. [onBack] clears the pick.
 */
@Composable
fun KubeNodeDetail(
    node: KubeNodeInfo,
    onBack: () -> Unit,
    onDrain: (name: String) -> Unit,
    onDebug: (name: String) -> Unit,
    onMeta: (name: String) -> Unit,
    onCordoned: () -> Unit,
    onOwner: (KubeObjectRef) -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val debugShell by app.uiPreferences.nodeDebugShell.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    var cordoning by remember { mutableStateOf<KubeNodeInfo?>(null) }
    KubeNodeCordonDialog(cordoning, snackbar, onDone = { cordoning = null }, onCordoned = onCordoned)
    Box(Modifier.fillMaxSize()) {
        KubeObjectScreen(
            ref = KubeObjectRef.node(node.name),
            onBack = onBack,
            onPortForward = null,
            onOwner = onOwner,
            onHelmRelease = { _, _ -> },
            onDeleted = onBack,
            header = {
                KubeNodeActionRow(
                    node,
                    onCordon = { cordoning = node },
                    onDrain = { onDrain(node.name) },
                    onMeta = { onMeta(node.name) },
                    onDebug = if (debugShell) { { onDebug(node.name) } } else null,
                )
            },
        )
        SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter))
    }
}
