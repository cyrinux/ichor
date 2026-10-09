package name.levis.ichor.ui.overview

import android.content.Context
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Block
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
 * What a node of a cluster without Talos offers: a cordon or uncordon, and a drain (the
 * maintenance screen, drain only). The caller closes the sheet on a pick.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun KubeNodeMenuSheet(node: KubeNodeInfo, onCordon: () -> Unit, onDrain: () -> Unit, onDismiss: () -> Unit) {
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

/**
 * The sheets behind a tap on a Kubernetes node: its menu ([node], null for none), then the
 * cordon confirmation. The cordon runs here and tells [snackbar] how it went, then
 * [onCordoned] reloads the nodes; the drain leaves to [onDrain]. [onClose] clears [node].
 */
@Composable
internal fun KubeNodeActionSheets(
    node: KubeNodeInfo?,
    snackbar: SnackbarHostState,
    onClose: () -> Unit,
    onDrain: (name: String) -> Unit,
    onCordoned: () -> Unit,
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
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
            onDismiss = onClose,
        )
    }
    cordoning?.let { target ->
        CordonDialog(
            hostname = target.name,
            cordoned = target.cordoned,
            onConfirm = { on ->
                cordoning = null
                scope.launch {
                    val message = cordonKubeNode(app.maintenanceManager, context, target.name, on)
                    onCordoned()
                    snackbar.showSnackbar(message, withDismissAction = true, duration = SnackbarDuration.Long)
                }
            },
            onDismiss = { cordoning = null },
        )
    }
}

/** Cordons ([on]) or uncordons the Kubernetes node [name]; returns the message to show. */
internal suspend fun cordonKubeNode(maintenances: MaintenanceManager, context: Context, name: String, on: Boolean): String =
    runCatching { maintenances.cordon(name, on) }.fold(
        onSuccess = { context.getString(if (on) R.string.cordon_done else R.string.uncordon_done, name) },
        onFailure = { context.getString(R.string.cordon_failed, name, it.uiText().resolve(context)) },
    )
