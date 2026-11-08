package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.seedOf

/**
 * The line under a screen's title that names what it acts on: [subtitle] (a node's hostname)
 * and, with several clusters on the phone, the active cluster's label after it ("node-a ·
 * lab"). Null when there is nothing to say: one cluster and no subtitle.
 */
fun clusterTitleLine(contexts: List<ContextSummary>, active: ContextSummary?, labels: ClusterLabels, subtitle: String? = null): String? {
    val cluster = active?.takeIf { contexts.size > 1 }?.let(labels::of)
    return listOfNotNull(subtitle?.ifBlank { null }, cluster).joinToString(" · ").ifEmpty { null }
}

/**
 * The title of a screen that changes the cluster (an upgrade, a maintenance, a sync): [title],
 * then [clusterTitleLine] with the cluster's colour dot, so a screen three levels deep (or one
 * a link opened after switching the cluster) still says which cluster it acts on.
 */
@Composable
fun ClusterScreenTitle(title: String, subtitle: String? = null) {
    Column {
        Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
        ClusterTitleSubline(subtitle)
    }
}

/** [ClusterScreenTitle]'s second line alone, for a title that shows more under it. */
@Composable
fun ClusterTitleSubline(subtitle: String? = null) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val colors by app.clusterColors.colors.collectAsStateWithLifecycle()
    val contexts = config?.summary?.contexts.orEmpty()
    val active = config?.activeSummary
    val line = clusterTitleLine(contexts, active, rememberClusterLabels(), subtitle) ?: return
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        if (active != null && contexts.size > 1) ClusterLogo(active, Color(colors.seedOf(active)), selected = false, size = 12.dp)
        Text(line, style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
