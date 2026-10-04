package name.levis.ichor.ui.argocd

import androidx.compose.animation.AnimatedVisibility
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
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
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
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The sync options, defaults from the app (server-side apply when its syncOptions say so) and
 * never pruning unless ticked; ticking prune lists what would be deleted. [resources] limits
 * the sync to those (selective sync); empty syncs everything.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoSyncSheet(app: ArgoApp, resources: List<ArgoResource>, onSync: (ArgoSyncOptions) -> Unit, onDismiss: () -> Unit) {
    var prune by remember { mutableStateOf(false) }
    var dryRun by remember { mutableStateOf(false) }
    var force by remember { mutableStateOf(false) }
    var outOfSyncOnly by remember { mutableStateOf(app.syncOptions.contains("ApplyOutOfSyncOnly=true")) }
    var serverSide by remember { mutableStateOf(app.serverSideApply) }
    var replace by remember { mutableStateOf(app.syncOptions.contains("Replace=true")) }
    val pruned = (if (resources.isEmpty()) app.resources else resources).filter { it.prune }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(stringResource(R.string.argo_sync_title, app.name), style = MaterialTheme.typography.titleLarge)
            Text(
                if (resources.isEmpty()) stringResource(R.string.argo_sync_all_resources, app.versionLabel)
                else pluralStringResource(R.plurals.argo_sync_some_resources, resources.size, resources.size),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(bottom = 8.dp),
            )
            Toggle(stringResource(R.string.argo_opt_prune), stringResource(R.string.argo_opt_prune_desc), prune) { prune = it }
            AnimatedVisibility(prune) { PruneList(pruned) }
            Toggle(stringResource(R.string.argo_opt_dry_run), stringResource(R.string.argo_opt_dry_run_desc), dryRun) { dryRun = it }
            Toggle(stringResource(R.string.argo_opt_force), stringResource(R.string.argo_opt_force_desc), force) { force = it }
            Toggle(stringResource(R.string.argo_opt_out_of_sync_only), stringResource(R.string.argo_opt_out_of_sync_only_desc), outOfSyncOnly) { outOfSyncOnly = it }
            Toggle(stringResource(R.string.argo_opt_server_side), stringResource(R.string.argo_opt_server_side_desc), serverSide) { serverSide = it }
            Toggle(stringResource(R.string.argo_opt_replace), stringResource(R.string.argo_opt_replace_desc), replace) { replace = it }
            Button(
                onClick = {
                    onSync(
                        ArgoSyncOptions(
                            prune = prune, dryRun = dryRun, force = force, applyOutOfSyncOnly = outOfSyncOnly,
                            serverSideApply = serverSide, replace = replace, resources = resources.map { it.ref },
                        ),
                    )
                },
                modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            ) {
                Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(
                    stringResource(if (dryRun) R.string.argo_sync_confirm_dry_run else R.string.argo_sync_confirm),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

@Composable
private fun Toggle(title: String, description: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
