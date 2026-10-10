package name.levis.ichor.ui.overview

import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.share.ShareLinkMenuItem
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.SystemUpdateAlt
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.seedOf
import name.levis.ichor.ui.components.ClusterLogo

/**
 * The overview title's menu, as on iOS: the clusters behind a "Switch cluster" submenu, so
 * "Manage clusters…" stays in reach without a scroll however many clusters there are.
 * Compose menus do not nest, so the submenu takes the menu's place, with a way back.
 */
@Composable
fun ClusterMenu(
    expanded: Boolean,
    config: StoredConfig,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onSelect: (String) -> Unit,
    onManage: () -> Unit,
    onDismiss: () -> Unit,
    /** "Upgrade cluster…": null when the role cannot upgrade (or the cluster has no Talos). */
    onUpgradeCluster: (() -> Unit)? = null,
    /** "Upgrade Kubernetes…": null when the role cannot upgrade (or the cluster has no Talos). */
    onUpgradeKubernetes: (() -> Unit)? = null,
) {
    var switching by remember(expanded) { mutableStateOf(false) }
    val contexts = config.summary.contexts
    DropdownMenu(expanded = expanded, onDismissRequest = onDismiss) {
        if (switching) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.clusters_switch)) },
                leadingIcon = { Icon(Icons.AutoMirrored.Outlined.ArrowBack, stringResource(R.string.common_back)) },
                onClick = { switching = false },
            )
            HorizontalDivider()
            contexts.forEach { context ->
                val active = context.name == config.activeContext
                DropdownMenuItem(
                    text = { Text(labels.of(context), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    leadingIcon = { ClusterLogo(context, Color(colors.seedOf(context)), selected = false, size = 24.dp) },
                    trailingIcon = if (active) {
                        { Icon(Icons.Outlined.Check, contentDescription = null) }
                    } else {
                        null
                    },
                    onClick = {
                        onDismiss()
                        if (!active) onSelect(context.name)
                    },
                )
            }
        } else {
            if (contexts.size > 1) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.clusters_switch)) },
                    leadingIcon = { Icon(Icons.Outlined.SwapHoriz, contentDescription = null) },
                    trailingIcon = { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null) },
                    onClick = { switching = true },
                )
            }
            DropdownMenuItem(
                text = { Text(stringResource(R.string.clusters_manage)) },
                leadingIcon = { Icon(Icons.Outlined.Layers, contentDescription = null) },
                onClick = {
                    onDismiss()
                    onManage()
                },
            )
            onUpgradeCluster?.let { upgrade ->
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.cluster_upgrade_menu)) },
                    leadingIcon = { Icon(Icons.Outlined.SystemUpdateAlt, contentDescription = null) },
                    onClick = {
                        onDismiss()
                        upgrade()
                    },
                )
            }
            onUpgradeKubernetes?.let { upgrade ->
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.k8s_upgrade_menu)) },
                    leadingIcon = { Icon(Icons.Outlined.Upgrade, contentDescription = null) },
                    onClick = {
                        onDismiss()
                        upgrade()
                    },
                )
            }
            ShareLinkMenuItem(ShareTarget.screen(ShareTarget.CLUSTER), onClick = onDismiss)
        }
    }
}
