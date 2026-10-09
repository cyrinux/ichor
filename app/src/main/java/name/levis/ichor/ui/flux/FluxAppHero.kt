package name.levis.ichor.ui.flux

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Autorenew
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.Difference
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Upgrade
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxApp
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.model.shortFluxRevision
import name.levis.ichor.ui.argocd.ArgoBadge
import name.levis.ichor.ui.argocd.OwnerChip
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The app at a glance: icon and name, a badge for its Ready reason and for suspended, stalled
 * or reconciling, Flux's message, where it comes from (source and URL, path or chart@version),
 * the revision applied against the one tried when they differ, its interval, the Kustomization
 * applying it, what it depends on and a release's failures.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun FluxAppHero(app: FluxApp) {
    val colors = LocalStatusColors.current
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            FluxAppIcon(app, 64.dp)
            Column(Modifier.padding(start = 16.dp).weight(1f)) {
                Text(app.name, style = MaterialTheme.typography.headlineSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                MutedText(listOf(app.kind, app.namespace).filter { it.isNotEmpty() }.joinToString(" · "))
                app.owner?.let { OwnerChip(it.asArgoOwner(), Modifier.padding(top = 4.dp)) }
            }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ArgoBadge(app.state.icon, app.reason.ifEmpty { app.state.label() }, app.state.color())
            if (app.suspended) ArgoBadge(Icons.Outlined.PauseCircle, stringResource(R.string.flux_badge_suspended), colors.muted)
            if (app.stalled) ArgoBadge(Icons.Outlined.Block, stringResource(R.string.flux_badge_stalled), colors.bad)
            if (app.reconciling) ArgoBadge(Icons.Outlined.Autorenew, stringResource(R.string.flux_badge_reconciling), MaterialTheme.colorScheme.primary)
            if (app.pending && !app.reconciling) ArgoBadge(Icons.Outlined.Schedule, stringResource(R.string.flux_badge_pending), MaterialTheme.colorScheme.primary)
        }
        if (app.message.isNotEmpty()) {
            Text(app.message, style = MaterialTheme.typography.bodySmall, color = app.state.color())
        }
        if (app.suspended) MutedText(stringResource(R.string.flux_suspended_hint))
        Column {
            app.source?.let { InfoRow(stringResource(R.string.flux_source), "${it.kind}/${it.name}", mono = true) }
            if (app.sourceURL.isNotEmpty()) InfoRow(stringResource(R.string.argo_repo), app.sourceURL, mono = true)
            when {
                app.isHelmRelease && app.chart.isNotEmpty() ->
                    InfoRow(stringResource(R.string.argo_chart), listOf(app.chart, app.chartVersion).filter { it.isNotEmpty() }.joinToString("@"), mono = true)
                app.path.isNotEmpty() -> InfoRow(stringResource(R.string.argo_path), app.path, mono = true)
            }
            if (app.revision.isNotEmpty()) InfoRow(stringResource(R.string.argo_revision), shortFluxRevision(app.revision), mono = true)
            if (app.revisionDiffers) {
                InfoRow(stringResource(R.string.flux_attempted_revision), shortFluxRevision(app.attemptedRevision), mono = true, valueColor = app.state.color())
            }
            if (app.targetNamespace.isNotEmpty()) InfoRow(stringResource(R.string.argo_destination), app.targetNamespace, mono = true)
            if (app.interval.isNotEmpty()) InfoRow(stringResource(R.string.flux_interval), app.interval, mono = true)
            app.owner?.let { InfoRow(stringResource(R.string.flux_applied_by), "${it.kind}/${it.namespace}/${it.name}", mono = true) }
            if (app.dependsOn.isNotEmpty()) InfoRow(stringResource(R.string.flux_depends_on), app.dependsOn.joinToString("\n"), mono = true)
            if (app.failures > 0) InfoRow(stringResource(R.string.flux_failures), app.failures.toString(), valueColor = colors.bad)
        }
    }
}

/**
 * Reconcile and Reconcile with source; Suspend or Resume; for a HelmRelease, Force upgrade
 * and Reset failures. Each asks [onAction] (which confirms first); a suspended app only resumes.
 * For a Kustomization, [onDiff] (when given) shows what a reconcile would change.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun FluxActionButtons(app: FluxApp, busy: Boolean, onDiff: (() -> Unit)? = null, onAction: (FluxAction) -> Unit) {
    // Every action patches the object; the diff only reads.
    val denial = rememberKubeDenial(KubeAction.fluxReconcile(app.kind), app.namespace)
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        FluxActionRow(app, busy || denial != null, onDiff, onAction)
        KubeDenialNote(denial)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FluxActionRow(app: FluxApp, busy: Boolean, onDiff: (() -> Unit)?, onAction: (FluxAction) -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Button(onClick = { onAction(FluxAction.RECONCILE) }, enabled = !busy && app.canReconcile) {
            ButtonContent(Icons.Outlined.Refresh, FluxAction.RECONCILE)
        }
        OutlinedButton(onClick = { onAction(FluxAction.RECONCILE_WITH_SOURCE) }, enabled = !busy && app.canReconcile) {
            ButtonContent(Icons.Outlined.Sync, FluxAction.RECONCILE_WITH_SOURCE)
        }
        val toggle = if (app.suspended) FluxAction.RESUME else FluxAction.SUSPEND
        OutlinedButton(onClick = { onAction(toggle) }, enabled = !busy) {
            ButtonContent(if (app.suspended) Icons.Outlined.PlayArrow else Icons.Outlined.Pause, toggle)
        }
        if (app.isKustomization && onDiff != null) {
            OutlinedButton(onClick = onDiff) {
                Icon(Icons.Outlined.Difference, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.flux_show_diff), modifier = Modifier.padding(start = 6.dp), maxLines = 1)
            }
        }
        if (app.isHelmRelease) {
            OutlinedButton(onClick = { onAction(FluxAction.FORCE) }, enabled = !busy && app.canForce) {
                ButtonContent(Icons.Outlined.Upgrade, FluxAction.FORCE)
            }
            OutlinedButton(onClick = { onAction(FluxAction.RESET) }, enabled = !busy && app.canForce) {
                ButtonContent(Icons.Outlined.RestartAlt, FluxAction.RESET)
            }
        }
    }
}

@Composable
private fun ButtonContent(icon: ImageVector, action: FluxAction) {
    Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp))
    Text(stringResource(action.label), modifier = Modifier.padding(start = 6.dp), maxLines = 1)
}
