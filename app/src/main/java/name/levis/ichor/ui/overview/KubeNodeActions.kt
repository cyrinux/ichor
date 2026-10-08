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
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.MaintenanceManager
import name.levis.ichor.model.KubeNodeInfo
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
            KubeNodeMenuRow(stringResource(if (node.cordoned) R.string.node_menu_uncordon else R.string.node_menu_cordon), Icons.Outlined.Block, onCordon)
            KubeNodeMenuRow(stringResource(R.string.node_menu_drain), Icons.AutoMirrored.Outlined.Logout, onDrain)
        }
    }
}

@Composable
private fun KubeNodeMenuRow(label: String, icon: ImageVector, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .clickable(role = Role.Button, onClick = onClick)
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Icon(icon, contentDescription = null)
        Text(label, style = MaterialTheme.typography.bodyLarge)
    }
}

/** Cordons ([on]) or uncordons the Kubernetes node [name]; returns the message to show. */
internal suspend fun cordonKubeNode(maintenances: MaintenanceManager, context: Context, name: String, on: Boolean): String =
    runCatching { maintenances.cordon(name, on) }.fold(
        onSuccess = { context.getString(if (on) R.string.cordon_done else R.string.uncordon_done, name) },
        onFailure = { context.getString(R.string.cordon_failed, name, it.uiText().resolve(context)) },
    )
