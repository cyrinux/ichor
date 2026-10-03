package name.levis.ichor.ui.apps

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.InventoryImage
import name.levis.ichor.model.InventoryPod
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.PodState
import name.levis.ichor.model.driftVersions
import name.levis.ichor.model.driftingRepos
import name.levis.ichor.model.memory
import name.levis.ichor.model.shortDigest
import name.levis.ichor.model.state
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * One app: what it is, which versions run, the URLs it is served at ([routes]) and its
 * workloads to restart ([restart]), both null when the role cannot reach the Kubernetes API,
 * the Argo CD Applications deploying it ([argo], null without Argo CD), its images and its
 * pods. [nodes] names the node addresses; tapping a pod opens its node's pods ([onPodNode]
 * with the node address).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppDetailSheet(
    app: InventoryApp,
    nodes: Map<String, NodeOverview>,
    routes: UiState<List<KubeRoute>>?,
    restart: AppRestartUi?,
    argo: AppArgoUi?,
    onPodNode: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        LazyColumn(
            contentPadding = PaddingValues(start = 20.dp, end = 20.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item { Header(app) }
            item { Badges(app) }
            item { Stats(app) }
            routes?.let { appRoutesSection(it) }
            restart?.let { appWorkloadsSection(it) }
            argo?.let { appArgoSection(it) }
            if (app.images.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.apps_detail_images)) }
                val drifting = app.driftingRepos
                items(app.images) { ImageRow(it, it.repo in drifting) }
            }
            if (app.pods.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.apps_detail_pods)) }
                items(app.pods, key = { "${it.namespace}/${it.pod}/${it.node}" }) { pod ->
                    PodRow(pod, nodes[pod.node]?.hostname ?: pod.node) { onPodNode(pod.node) }
                }
            }
        }
    }
}

@Composable
private fun Header(app: InventoryApp) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        AppIconTile(app, size = 64.dp)
        Column(Modifier.padding(start = 16.dp)) {
            Text(app.name, style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(
                (listOf(categoryLabel(app.category)) + app.namespaces).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Badges(app: InventoryApp) {
    val warn = LocalStatusColors.current.warn
    if (app.version.isEmpty() && !app.drift && !app.unpinned) return
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (app.version.isNotEmpty()) {
            Surface(shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
                Text(
                    app.version,
                    style = MaterialTheme.typography.labelMedium,
                    fontFamily = FontFamily.Monospace,
                    modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
                )
            }
        }
        if (app.drift) StatusPill(pluralStringResource(R.plurals.apps_versions_running, app.driftVersions, app.driftVersions), warn)
        if (app.unpinned) StatusPill(stringResource(R.string.apps_unpinned), warn)
    }
}

@Composable
private fun Stats(app: InventoryApp) {
    val containers = if (app.running < app.containers) "${app.running}/${app.containers}" else app.containers.toString()
    Row(Modifier.padding(vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        StatTile(containers, stringResource(R.string.apps_stat_containers), Modifier.weight(1f))
        StatTile(app.nodes.size.toString(), stringResource(R.string.apps_stat_nodes), Modifier.weight(1f))
        StatTile(if (app.memory > 0) formatBytes(app.memory) else "—", stringResource(R.string.apps_stat_memory), Modifier.weight(1f))
    }
}

@Composable
private fun StatTile(value: String, label: String, modifier: Modifier) {
    Surface(modifier, shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Column(Modifier.padding(horizontal = 12.dp, vertical = 10.dp)) {
            Text(value, style = MaterialTheme.typography.titleMedium, maxLines = 1)
            Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
    }
}

/** Repository and tag (×containers); a digest-only image shows its short digest. */
@Composable
private fun ImageRow(image: InventoryImage, drifting: Boolean) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            image.repo.ifEmpty { shortDigest(image.digest) },
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (image.tag.isNotEmpty()) {
            Text(
                image.tag,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = if (drifting) LocalStatusColors.current.warn else MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        if (image.containers > 1) {
            Text("×${image.containers}", style = MaterialTheme.typography.bodySmall, color = muted, modifier = Modifier.padding(start = 6.dp))
        }
    }
}

/** Status dot, pod name, then "node · containers · memory". */
@Composable
private fun PodRow(pod: InventoryPod, host: String, onClick: () -> Unit) {
    val colors = LocalStatusColors.current
    val dot = when (pod.state) {
        PodState.RUNNING -> colors.ok
        PodState.STARTING -> colors.warn
        PodState.STOPPED -> colors.bad
    }
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).background(dot, CircleShape))
        Column(Modifier.padding(start = 12.dp)) {
            Text(pod.pod, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(
                    host,
                    pod.containers.joinToString(", ") { it.name }.takeIf { it.isNotEmpty() },
                    pod.memory.takeIf { it > 0 }?.let(::formatBytes),
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
