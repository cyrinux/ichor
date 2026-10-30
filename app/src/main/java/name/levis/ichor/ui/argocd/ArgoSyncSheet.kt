package name.levis.ichor.ui.argocd

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Sync
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoResource
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.model.forResources
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.diff.DiffCounts
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The sync options, defaults from the app (server-side apply when its syncOptions say so) and
 * never pruning unless ticked; ticking prune lists what would be deleted. [resources] limits
 * the sync to those (selective sync); empty syncs everything. [diff] is what Argo CD compared
 * last, loaded while the sheet is open: the options and Sync never wait for it.
 * [onFullDiff] opens the full diff screen.
 *
 * Dry run and Sync sit in a footer outside the scroll, so the default sync is one tap at any
 * sheet height; the rarer options fold under one row that names the ones turned on.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoSyncSheet(
    app: ArgoApp,
    resources: List<ArgoResource>,
    diff: UiState<FluxDiff>,
    onFullDiff: (() -> Unit)?,
    onSync: (ArgoSyncOptions) -> Unit,
    onDismiss: () -> Unit,
) {
    var prune by remember { mutableStateOf(false) }
    var options by remember { mutableStateOf(MoreOptions.of(app)) }
    val pruned = (if (resources.isEmpty()) app.resources else resources).filter { it.prune }
    val changes = (diff as? UiState.Loaded)?.data?.forResources(resources.map { it.ref })
    // What the sync creates or deletes, beside the button: visible without scrolling.
    val destructive = changes.orEmpty().filter { it.change == DiffChange.CREATED || it.change == DiffChange.DELETED }
    val sync = { dryRun: Boolean ->
        onSync(
            ArgoSyncOptions(
                prune = prune, dryRun = dryRun, force = options.force, applyOutOfSyncOnly = options.outOfSyncOnly,
                serverSideApply = options.serverSide, replace = options.replace, resources = resources.map { it.ref },
            ),
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(stringResource(R.string.argo_sync_title, app.name), style = MaterialTheme.typography.titleLarge)
            MutedText(
                if (resources.isEmpty()) stringResource(R.string.argo_sync_all_resources, app.versionLabel)
                else pluralStringResource(R.plurals.argo_sync_some_resources, resources.size, resources.size),
                modifier = Modifier.padding(bottom = 8.dp),
            )
            ArgoSyncChanges(diff, changes, onFullDiff)
            Toggle(stringResource(R.string.argo_opt_prune), stringResource(R.string.argo_opt_prune_desc), prune) { prune = it }
            AnimatedVisibility(prune) { PruneList(pruned) }
            MoreOptionsSection(options) { options = it }
        }
        SyncFooter(destructive, onDryRun = { sync(true) }, onSync = { sync(false) })
    }
}

/** The options most syncs leave alone, seeded from the app's own syncOptions. */
private data class MoreOptions(
    val force: Boolean = false,
    val outOfSyncOnly: Boolean = false,
    val serverSide: Boolean = false,
    val replace: Boolean = false,
) {
    companion object {
        fun of(app: ArgoApp) = MoreOptions(
            outOfSyncOnly = app.syncOptions.contains("ApplyOutOfSyncOnly=true"),
            serverSide = app.serverSideApply,
            replace = app.syncOptions.contains("Replace=true"),
        )
    }
}

/** One row naming the options turned on; tapping it unfolds their switches. */
@Composable
private fun MoreOptionsSection(options: MoreOptions, onChange: (MoreOptions) -> Unit) {
    var open by rememberSaveable { mutableStateOf(false) }
    val on = listOfNotNull(
        stringResource(R.string.argo_opt_force).takeIf { options.force },
        stringResource(R.string.argo_opt_out_of_sync_only).takeIf { options.outOfSyncOnly },
        stringResource(R.string.argo_opt_server_side).takeIf { options.serverSide },
        stringResource(R.string.argo_opt_replace).takeIf { options.replace },
    )
    HorizontalDivider(Modifier.padding(top = 4.dp))
    Row(
        Modifier.fillMaxWidth().clickable { open = !open }.padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.argo_sync_options), style = MaterialTheme.typography.bodyLarge)
            MutedText(on.joinToString(", ").ifEmpty { stringResource(R.string.argo_sync_options_none) })
        }
        Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
    }
    AnimatedVisibility(open) {
        Column {
            Toggle(stringResource(R.string.argo_opt_force), stringResource(R.string.argo_opt_force_desc), options.force) {
                onChange(options.copy(force = it))
            }
            Toggle(stringResource(R.string.argo_opt_out_of_sync_only), stringResource(R.string.argo_opt_out_of_sync_only_desc), options.outOfSyncOnly) {
                onChange(options.copy(outOfSyncOnly = it))
            }
            Toggle(stringResource(R.string.argo_opt_server_side), stringResource(R.string.argo_opt_server_side_desc), options.serverSide) {
                onChange(options.copy(serverSide = it))
            }
            Toggle(stringResource(R.string.argo_opt_replace), stringResource(R.string.argo_opt_replace_desc), options.replace) {
                onChange(options.copy(replace = it))
            }
        }
    }
}

/** Fixed under the scroll: what gets created or deleted, then Dry run beside Sync. */
@Composable
private fun SyncFooter(destructive: List<KubeDiffResource>, onDryRun: () -> Unit, onSync: () -> Unit) {
    Column(Modifier.padding(horizontal = 20.dp).padding(top = 8.dp, bottom = 16.dp).navigationBarsPadding()) {
        if (destructive.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().padding(bottom = 8.dp), horizontalArrangement = Arrangement.End) {
                DiffCounts(changeCounts(destructive))
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            IchorOutlinedButton(onClick = onDryRun, modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.argo_sync_confirm_dry_run))
            }
            IchorButton(onClick = onSync, modifier = Modifier.weight(1f)) {
                Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.argo_sync_confirm), modifier = Modifier.padding(start = 8.dp))
            }
        }
    }
}

@Composable
private fun Toggle(title: String, description: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            MutedText(description)
        }
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

/** What prune deletes, in amber; a calm line when nothing would go. */
@Composable
private fun PruneList(resources: List<ArgoResource>) {
    val warn = LocalStatusColors.current.warn
    Surface(color = warn.copy(alpha = 0.12f), contentColor = warn, shape = RoundedCornerShape(12.dp), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.DeleteSweep, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(
                    if (resources.isEmpty()) stringResource(R.string.argo_prune_none)
                    else pluralStringResource(R.plurals.argo_prune_list, resources.size, resources.size),
                    style = MaterialTheme.typography.labelLarge,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            resources.forEach { r ->
                Text(
                    "${r.kind} ${listOf(r.namespace, r.name).filter { it.isNotEmpty() }.joinToString("/")}",
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                )
            }
        }
    }
}
