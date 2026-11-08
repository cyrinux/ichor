package name.levis.ichor.ui.node

import android.widget.Toast
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ContextNodeRemoval
import name.levis.ichor.removeClusterNodes
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.userMessage
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Takes [node] out of the active cluster, each step reporting its own outcome. Under the app's
 * scope: leaving the screen, which a reset does, must not cut the wait for the kubelet.
 * [deleteKubeNode]: its Node goes too, once its kubelet has stopped.
 */
internal fun TalosApp.removeNodeForGood(node: String, hostname: String, deleteKubeNode: Boolean) {
    val contextName = configRepository.config.value?.activeContext ?: return
    ProcessLifecycleOwner.get().lifecycleScope.launch {
        val removed = runCatching { removeClusterNodes(contextName, listOf(node), listOf(hostname)) }
        val message = removed.fold(
            onSuccess = { getString(R.string.node_removed, hostname) },
            onFailure = { getString(R.string.node_remove_failed, hostname, it.userMessage()) },
        )
        Toast.makeText(this@removeNodeForGood, message, Toast.LENGTH_LONG).show()
        if (removed.isFailure || !deleteKubeNode) return@launch
        val deleted = runCatching { nodeLifecycleRepository.deleteDepartedNode(hostname, node) }.fold(
            onSuccess = { getString(R.string.node_kube_deleted, hostname) },
            onFailure = { getString(R.string.node_kube_delete_failed, hostname, it.userMessage()) },
        )
        Toast.makeText(this@removeNodeForGood, deleted, Toast.LENGTH_LONG).show()
    }
}

/** After a reset that wipes the system disk: also take the node out of the cluster? */
@Composable
internal fun RemoveAfterResetDialog(
    hostname: String,
    removal: ContextNodeRemoval,
    onRemove: () -> Unit,
    onKeep: () -> Unit,
) {
    IchorAlertDialog(
        onDismissRequest = onKeep,
        title = { Text(stringResource(R.string.node_remove_title, hostname)) },
        text = { RemovalBody(stringResource(R.string.node_remove_body), removal) },
        confirmButton = { IchorTextButton(onClick = onRemove) { Text(stringResource(R.string.node_remove_confirm)) } },
        dismissButton = { IchorTextButton(onClick = onKeep) { Text(stringResource(R.string.node_remove_keep)) } },
    )
}

/**
 * "Forget node" for a node that does not answer and that the cluster no longer lists; nothing
 * for any other node. Checked again when [down] changes: a read that fails offers nothing.
 */
@Composable
internal fun ForgetNodeCard(app: TalosApp, node: String, hostname: String, down: Boolean, onForgotten: () -> Unit) {
    var removal by remember { mutableStateOf<ContextNodeRemoval?>(null) }
    var confirming by remember { mutableStateOf(false) }
    LaunchedEffect(node, down) {
        val lifecycle = app.nodeLifecycleRepository
        removal = runCatching { if (down && lifecycle.forgettable(node)) lifecycle.removalPlan(node) else null }
            .getOrNull()?.takeIf { it.allowed }
    }
    val offered = removal ?: return
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            MutedText(stringResource(R.string.node_forget_hint))
            IchorTextButton(onClick = { confirming = true }) { Text(stringResource(R.string.node_forget_action)) }
        }
    }
    if (confirming) {
        IchorAlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text(stringResource(R.string.node_forget_title, hostname)) },
            text = { RemovalBody(stringResource(R.string.node_forget_body), offered) },
            confirmButton = {
                IchorTextButton(onClick = {
                    confirming = false
                    app.removeNodeForGood(node, hostname, deleteKubeNode = false)
                    onForgotten()
                }) { Text(stringResource(R.string.node_forget_action)) }
            },
            dismissButton = {
                IchorTextButton(onClick = { confirming = false }) { Text(stringResource(R.string.common_cancel)) }
            },
        )
    }
}

@Composable
private fun RemovalBody(body: String, removal: ContextNodeRemoval) {
    Column {
        Text(body, style = MaterialTheme.typography.bodyMedium)
        if (removal.endpoint) {
            Text(
                stringResource(R.string.node_remove_endpoint),
                color = LocalStatusColors.current.warn,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}
