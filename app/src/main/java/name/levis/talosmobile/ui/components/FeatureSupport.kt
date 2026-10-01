package name.levis.talosmobile.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.talosmobile.R
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.OVERVIEW
import name.levis.talosmobile.data.featuresKey
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.FeatureSupport
import name.levis.talosmobile.model.NodeFeatures
import name.levis.talosmobile.model.TalosFeature
import name.levis.talosmobile.model.VersionNotice
import name.levis.talosmobile.model.clusterSupport
import name.levis.talosmobile.model.notice
import name.levis.talosmobile.model.support

/**
 * What [node]'s Talos version supports: the cached answer at once, then the node's. Null
 * while unknown (unreachable node, first load): everything then counts as supported.
 */
@Composable
fun rememberNodeFeatures(node: String): NodeFeatures? {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val invalidations by talos.invalidations.collectAsStateWithLifecycle()
    val changes by talos.featureChanges.collectAsStateWithLifecycle()
    val features by produceState(talos.cached<NodeFeatures>(featuresKey(node))?.value, node, invalidations, changes) {
        runCatching { talos.features(node) }.onSuccess { value = it }
    }
    return features
}

/**
 * Features of [nodes] (default: the reachable nodes of the last overview), for cluster-wide
 * screens. Nodes that do not answer are left out.
 */
@Composable
fun rememberClusterFeatures(nodes: List<String>? = null): List<NodeFeatures> {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val invalidations by talos.invalidations.collectAsStateWithLifecycle()
    val changes by talos.featureChanges.collectAsStateWithLifecycle()
    val targets = nodes ?: talos.cached<ClusterOverview>(OVERVIEW)?.value?.nodes.orEmpty().filter { it.reachable }.map { it.node }
    val features by produceState(
        targets.mapNotNull { talos.cached<NodeFeatures>(featuresKey(it))?.value },
        targets,
        invalidations,
        changes,
    ) {
        runCatching { talos.clusterFeatures(targets) }.onSuccess { value = it }
    }
    return features
}

@Composable
fun rememberClusterSupport(feature: TalosFeature, nodes: List<String>? = null): FeatureSupport =
    clusterSupport(rememberClusterFeatures(nodes), feature)

/** "Needs Talos v1.15 or newer", or the generic sentence when the version is unknown. */
@Composable
fun VersionNotice.text(): String =
    if (minVersion.isNotEmpty()) stringResource(R.string.common_needs_talos, minVersion) else stringResource(R.string.common_not_on_this_talos)

/** Full-screen information (not an error): e.g. a feature this node's Talos version lacks. */
@Composable
fun InfoBox(text: String, modifier: Modifier = Modifier, onRetry: (() -> Unit)? = null) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Outlined.Info,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text,
            textAlign = TextAlign.Center,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        onRetry?.let {
            Spacer(Modifier.height(16.dp))
            OutlinedButton(onClick = it) { Text(stringResource(R.string.common_retry)) }
        }
    }
}

/** Muted information line inside a card or a list. */
@Composable
fun InfoNotice(text: String, modifier: Modifier = Modifier) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Info, contentDescription = null, tint = muted, modifier = Modifier.size(16.dp))
        Text(text, color = muted, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(start = 8.dp))
    }
}

/** [content] when [support] says the node can do it, else the "Needs Talos…" information. */
@Composable
fun FeatureGate(support: FeatureSupport, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val notice = support.notice
    if (notice == null) content() else InfoBox(notice.text(), modifier)
}

/**
 * A menu entry for a version-dependent feature: never hidden, but disabled with
 * "Needs Talos vX or newer" under its label when the node's Talos lacks it.
 */
@Composable
fun FeatureMenuItem(
    label: String,
    icon: ImageVector,
    support: FeatureSupport,
    onClick: () -> Unit,
    enabled: Boolean = true,
) {
    val notice = support.notice
    DropdownMenuItem(
        text = {
            Column {
                Text(label)
                notice?.let { Text(it.text(), style = MaterialTheme.typography.labelSmall) }
            }
        },
        leadingIcon = { Icon(icon, contentDescription = null) },
        enabled = enabled && notice == null,
        onClick = onClick,
    )
}
