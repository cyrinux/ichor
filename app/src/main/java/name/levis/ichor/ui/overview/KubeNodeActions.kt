package name.levis.ichor.ui.overview

import android.content.Context
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.MaintenanceManager
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubePermission
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeActionAccess
import name.levis.ichor.ui.node.CordonDialog
import name.levis.ichor.ui.uiText

/**
 * What a node of a cluster without Talos offers: a cordon or uncordon, a drain (the
 * maintenance screen, drain only), its taints and labels when [onMeta] is set, and a root shell
 * when [onDebug] is set (the setting is on). The caller closes the sheet on a pick.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun KubeNodeMenuSheet(
    node: KubeNodeInfo,
    onCordon: () -> Unit,
    onDrain: () -> Unit,
    onMeta: (() -> Unit)?,
    onDebug: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 16.dp).navigationBarsPadding()) {
            Text(node.name, style = MaterialTheme.typography.titleLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            // Nodes are cluster-scoped, and a drain evicts pods of every namespace.
            val access = rememberKubeActionAccess("")
            KubeNodeMenuRow(
                stringResource(if (node.cordoned) R.string.node_menu_uncordon else R.string.node_menu_cordon),
                Icons.Outlined.Block,
                access?.denial(KubeAction.CORDON_NODE),
                onCordon,
            )
            KubeNodeMenuRow(stringResource(R.string.node_menu_drain), Icons.AutoMirrored.Outlined.Logout, access?.denial(KubeAction.DRAIN_NODE), onDrain)
            if (onMeta != null) {
                KubeNodeMenuRow(stringResource(R.string.node_meta_menu), Icons.Outlined.Sell, access?.denial(KubeAction.EDIT_NODE_META), onMeta)
            }
            if (onDebug != null) {
                // The debug pod lives in "default": asked there, not cluster-wide.
                val podAccess = rememberKubeActionAccess(NODE_DEBUG_NAMESPACE)
                KubeNodeMenuRow(stringResource(R.string.node_menu_kube_debug_shell), Icons.Outlined.Terminal, podAccess?.denial(KubeAction.DEBUG_NODE), onDebug)
            }
        }
    }
}

@Composable
private fun KubeNodeMenuRow(label: String, icon: ImageVector, denial: KubePermission?, onClick: () -> Unit) {
    val enabled = denial == null
    val color = if (enabled) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurface.copy(alpha = DISABLED_ALPHA)
    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Icon(icon, contentDescription = null, tint = color)
        Column {
            Text(label, style = MaterialTheme.typography.bodyLarge, color = color)
            KubeDenialNote(denial)
        }
    }
}

// Material's disabled content alpha.
private const val DISABLED_ALPHA = 0.38f

/** Where the node debug pod is created. */
private const val NODE_DEBUG_NAMESPACE = "default"

/**
 * The sheets behind a tap on a Kubernetes node: its menu ([node], null for none), then the
 * cordon confirmation. The cordon runs here and tells [snackbar] how it went, then
 * [onCordoned] reloads the nodes; the drain leaves to [onDrain], the root shell to [onDebug], the
 * taints and labels editor to [onMeta] (none offered when null). [onClose] clears [node].
 */
@Composable
internal fun KubeNodeActionSheets(
    node: KubeNodeInfo?,
    snackbar: SnackbarHostState,
    onClose: () -> Unit,
    onDrain: (name: String) -> Unit,
    onCordoned: () -> Unit,
    onDebug: ((name: String) -> Unit)? = null,
    onMeta: ((name: String) -> Unit)? = null,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val debugShell by app.uiPreferences.nodeDebugShell.collectAsStateWithLifecycle()
    var cordoning by remember { mutableStateOf<KubeNodeInfo?>(null) }
    node?.let {
        KubeNodeMenuSheet(
            node = it,
            onCordon = {
                onClose()
                cordoning = it
            },
            onDrain = {
                onClose()
                onDrain(it.name)
            },
            onMeta = onMeta?.let { open ->
                {
                    onClose()
                    open(it.name)
                }
            },
            onDebug = if (debugShell && onDebug != null) {
                {
                    onClose()
                    onDebug(it.name)
                }
            } else null,
            onDismiss = onClose,
        )
    }
    KubeNodeCordonDialog(cordoning, snackbar, onDone = { cordoning = null }, onCordoned = onCordoned)
}

/**
 * The cordon or uncordon confirmation for [target] (none when null). On confirm the cordon runs
 * here and tells [snackbar] how it went, then [onCordoned] reloads; [onDone] clears [target].
 */
@Composable
internal fun KubeNodeCordonDialog(target: KubeNodeInfo?, snackbar: SnackbarHostState, onDone: () -> Unit, onCordoned: () -> Unit) {
    target ?: return
    val app = LocalContext.current.applicationContext as TalosApp
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    CordonDialog(
        hostname = target.name,
        cordoned = target.cordoned,
        onConfirm = { on ->
            onDone()
            scope.launch {
                val message = cordonKubeNode(app.maintenanceManager, context, target.name, on)
                onCordoned()
                snackbar.showSnackbar(message, withDismissAction = true, duration = SnackbarDuration.Long)
            }
        },
        onDismiss = onDone,
    )
}

/**
 * The node menu as a row of chips, for a node shown beside the list: the same actions, icons and
 * permission gating as [KubeNodeMenuSheet]; a refused one is disabled with its reason below.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun KubeNodeActionRow(
    node: KubeNodeInfo,
    onCordon: () -> Unit,
    onDrain: () -> Unit,
    onMeta: () -> Unit,
    onDebug: (() -> Unit)?,
    modifier: Modifier = Modifier,
) {
    val access = rememberKubeActionAccess("")
    val podAccess = if (onDebug != null) rememberKubeActionAccess(NODE_DEBUG_NAMESPACE) else null
    val cordonDenial = access?.denial(KubeAction.CORDON_NODE)
    val drainDenial = access?.denial(KubeAction.DRAIN_NODE)
    val metaDenial = access?.denial(KubeAction.EDIT_NODE_META)
    val debugDenial = podAccess?.denial(KubeAction.DEBUG_NODE)
    Column(modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            KubeNodeActionChip(stringResource(if (node.cordoned) R.string.node_menu_uncordon else R.string.node_menu_cordon), Icons.Outlined.Block, cordonDenial, onCordon)
            KubeNodeActionChip(stringResource(R.string.node_menu_drain), Icons.AutoMirrored.Outlined.Logout, drainDenial, onDrain)
            KubeNodeActionChip(stringResource(R.string.node_meta_menu), Icons.Outlined.Sell, metaDenial, onMeta)
            if (onDebug != null) KubeNodeActionChip(stringResource(R.string.node_menu_kube_debug_shell), Icons.Outlined.Terminal, debugDenial, onDebug)
        }
        // Each refusal once: chips refused for the same reason share its note.
        listOfNotNull(cordonDenial, drainDenial, metaDenial, debugDenial).distinct().forEach { KubeDenialNote(it) }
    }
}

@Composable
private fun KubeNodeActionChip(label: String, icon: ImageVector, denial: KubePermission?, onClick: () -> Unit) {
    AssistChip(
        onClick = onClick,
        enabled = denial == null,
        label = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null, modifier = Modifier.size(AssistChipDefaults.IconSize)) },
    )
}

/** Cordons ([on]) or uncordons the Kubernetes node [name]; returns the message to show. */
internal suspend fun cordonKubeNode(maintenances: MaintenanceManager, context: Context, name: String, on: Boolean): String =
    runCatching { maintenances.cordon(name, on) }.fold(
        onSuccess = { context.getString(if (on) R.string.cordon_done else R.string.uncordon_done, name) },
        onFailure = { context.getString(R.string.cordon_failed, name, it.uiText().resolve(context)) },
    )
