package name.levis.ichor.ui.flux

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Difference
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FLUX_DRIFT_WARN
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.FluxDependencyTree
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.argocd.ArgoBadge
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * What [tree]'s app waits for, indented by depth with each link's health (a missing one
 * says so), then the apps that wait for it. A listed app opens through [onOpen].
 */
@Composable
fun FluxDependsOnTree(tree: FluxDependencyTree, onOpen: (FluxApp) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (tree.upstream.isNotEmpty()) {
            SectionTitle(stringResource(R.string.flux_depends_on))
            tree.upstream.forEach { node ->
                DependencyRow(node.key, node.app, indent = node.depth - 1, onOpen)
            }
        }
        if (tree.dependents.isNotEmpty()) {
            SectionTitle(stringResource(R.string.flux_needed_by, tree.dependents.size.toString()))
            tree.dependents.forEach { app -> DependencyRow("${app.namespace}/${app.name}", app, indent = 0, onOpen) }
        }
    }
}

@Composable
private fun DependencyRow(key: String, app: FluxApp?, indent: Int, onOpen: (FluxApp) -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .then(if (app != null) Modifier.clickable { onOpen(app) } else Modifier)
            .padding(start = (indent * 16).dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        HealthDot(app?.serviceHealth ?: ServiceHealth.UNKNOWN)
        Column(Modifier.weight(1f)) {
            Text(key, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            when {
                app == null -> MutedText(stringResource(R.string.flux_depends_on_missing))
                app.reason.isNotEmpty() -> MutedText(app.reason, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * A watched HelmRelease's drift: "Drift: none", or "Drift detected · 18 min ago" with what
 * changed (tap to read it whole). In warn mode Flux only reports it.
 */
@Composable
fun FluxDriftLine(app: FluxApp) {
    val colors = LocalStatusColors.current
    var expanded by rememberSaveable(app.key) { mutableStateOf(false) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (app.drifted) {
            ArgoBadge(Icons.Outlined.Difference, stringResource(R.string.flux_drift_detected, timeAgo(app.driftAt)), colors.warn)
            if (app.driftMessage.isNotEmpty()) {
                Text(
                    app.driftMessage,
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    maxLines = if (expanded) Int.MAX_VALUE else 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.clickable { expanded = !expanded },
                )
            }
        } else {
            ArgoBadge(Icons.Outlined.Difference, stringResource(R.string.flux_drift_none), colors.ok)
        }
        if (app.driftMode == FLUX_DRIFT_WARN) MutedText(stringResource(R.string.flux_drift_warn_hint))
    }
}
