package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.VeleroRestoreOptions
import name.levis.ichor.model.VeleroRestorePreview
import name.levis.ichor.model.VeleroRestoreReason
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.timeAgo

/**
 * Restoring a backup: what it holds first, then the namespaces to restore (all by default, each
 * into another namespace if wanted), how existing objects are treated and whether volumes are
 * restored, confirmed by typing the backup's name. After the launch the dialog follows the
 * Restore's phase and item progress every 5 s until it ends.
 */
@Composable
fun VeleroRestoreDialog(namespace: String, backup: String, actions: VeleroActions, onDismiss: () -> Unit) {
    var preview by remember(namespace, backup) { mutableStateOf<VeleroRestorePreview?>(null) }
    var loadError by remember(namespace, backup) { mutableStateOf<UiText?>(null) }
    var selected by remember(namespace, backup) { mutableStateOf(emptySet<String>()) }
    var targets by remember(namespace, backup) { mutableStateOf(emptyMap<String, String>()) }
    var updateExisting by remember(namespace, backup) { mutableStateOf(false) }
    var restoreVolumes by remember(namespace, backup) { mutableStateOf(true) }
    var restoreName by remember(namespace, backup) { mutableStateOf<String?>(null) }
    var launchError by remember(namespace, backup) { mutableStateOf<UiText?>(null) }
    val denial = rememberKubeDenial(KubeAction.VELERO_RESTORE, namespace)
    val scope = rememberCoroutineScope()

    // Before the launch: the preview once. After it: the Restore's progress every 5 s until it
    // ends, then the lists reload.
    LaunchedEffect(namespace, backup, restoreName) {
        if (restoreName == null) {
            try {
                preview = actions.restorePreview(namespace, backup)
                selected = preview?.includedNamespaces?.filter { it != "*" }?.toSet() ?: emptySet()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                loadError = e.uiText()
            }

            return@LaunchedEffect
        }

        while (isActive) {
            try {
                preview = actions.restorePreview(namespace, backup)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // The last answer stays on show; the next tick tries again.
            }

            val restore = preview?.restores?.firstOrNull { it.name == restoreName }
            if (restore == null || restore.finished) break
            delay(5000)
        }

        actions.changed()
    }

    val p = preview
    if (restoreName != null || launchError != null) {
        VeleroRestoreProgress(backup, restoreName, p, launchError, onDismiss)
        return
    }

    val allNamespaces = p?.includedNamespaces?.let { it.isEmpty() || "*" in it } ?: false
    val options = VeleroRestoreOptions(
        includedNamespaces = if (allNamespaces) emptyList() else selected.toList(),
        namespaceMapping = targets.mapNotNull { (src, dst) ->
            if (src in selected && dst.isNotBlank() && dst != src) src to dst else null
        }.toMap(),
        existingResourcePolicy = if (updateExisting) "update" else "none",
        restorePVs = if (restoreVolumes) null else false,
    )

    HostnameConfirmDialog(
        title = stringResource(R.string.velero_restore_title, backup),
        hostname = backup,
        confirmLabel = stringResource(R.string.velero_restore),
        enabled = p?.canRestore == true && denial == null,
        onConfirm = {
            scope.launch {
                try {
                    restoreName = actions.restore(namespace, backup, options)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    launchError = e.uiText()
                }
            }
        },
        onDismiss = onDismiss,
    ) {
        Text(stringResource(R.string.velero_restore_risk))
        loadError?.let { Text(it.asString()) }
        if (p == null && loadError == null) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        p?.let {
            VeleroRestoreHolds(it)
            if (!it.canRestore) {
                it.reasons.forEach { reason -> Text(reasonText(reason), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
            } else {
                VeleroRestoreChoices(it, allNamespaces, selected, targets, updateExisting, restoreVolumes,
                    onSelect = { ns, on -> selected = if (on) selected + ns else selected - ns },
                    onTarget = { ns, target -> targets = targets + (ns to target) },
                    onUpdateExisting = { updateExisting = it },
                    onRestoreVolumes = { restoreVolumes = it },
                )
            }
            if (it.error.isNotEmpty()) InlineError(stringResource(R.string.data_services_unreadable, it.error))
        }
        denial?.let { KubeDenialNote(it) }
    }
}

/** What the backup holds: when, what namespaces, how many items and volumes, its warnings. */
@Composable
private fun VeleroRestoreHolds(p: VeleroRestorePreview) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        InfoRow(
            stringResource(R.string.cnpg_phase),
            listOfNotNull(
                p.phase,
                p.completedAt.takeIf { it > 0 }?.let { timeAgo(it) },
                countsText(p.errors, p.warnings),
            ).joinToString(" · "),
        )
        InfoRow(stringResource(R.string.velero_namespaces), p.includedNamespaces.filter { it != "*" }.ifEmpty { null }?.joinToString(", ") ?: stringResource(R.string.velero_all_namespaces))
        InfoRow(stringResource(R.string.velero_restore_items_label), pluralStringResource(R.plurals.velero_restore_items, p.totalItems, p.totalItems))
        InfoRow(stringResource(R.string.velero_restore_volumes_label), if (p.volumeSnapshots == 0) stringResource(R.string.velero_restore_no_volumes) else pluralStringResource(R.plurals.velero_restore_volumes, p.volumeSnapshots, p.volumeSnapshots))
        if (p.storageLocation.isNotEmpty()) InfoRow(stringResource(R.string.velero_storage_location), p.storageLocation)
    }
}

/** The choices: which namespaces, into which targets, existing objects, volumes. */
@Composable
private fun VeleroRestoreChoices(
    p: VeleroRestorePreview,
    allNamespaces: Boolean,
    selected: Set<String>,
    targets: Map<String, String>,
    updateExisting: Boolean,
    restoreVolumes: Boolean,
    onSelect: (String, Boolean) -> Unit,
    onTarget: (String, String) -> Unit,
    onUpdateExisting: (Boolean) -> Unit,
    onRestoreVolumes: (Boolean) -> Unit,
) {
    if (allNamespaces) {
        Text(stringResource(R.string.velero_restore_every_namespace), style = MaterialTheme.typography.bodySmall)
    } else {
        Text(stringResource(R.string.velero_restore_choose_namespaces), style = MaterialTheme.typography.labelLarge)
        p.includedNamespaces.filter { it != "*" }.forEach { ns ->
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = ns in selected, onCheckedChange = { onSelect(ns, it) })
                Column(Modifier.weight(1f)) {
                    Text(ns, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (ns in selected) {
                        OutlinedTextField(
                            value = targets[ns].orEmpty(),
                            onValueChange = { onTarget(ns, it.trim()) },
                            placeholder = { Text(stringResource(R.string.velero_restore_target_same), style = MaterialTheme.typography.labelSmall) },
                            singleLine = true,
                            textStyle = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }
        }
    }
    Text(stringResource(R.string.velero_restore_existing), style = MaterialTheme.typography.labelLarge)
    ChoiceRow(stringResource(R.string.velero_restore_existing_leave), !updateExisting) { onUpdateExisting(false) }
    ChoiceRow(stringResource(R.string.velero_restore_existing_update), updateExisting) { onUpdateExisting(true) }
    Text(stringResource(R.string.velero_restore_volumes), style = MaterialTheme.typography.labelLarge)
    ChoiceRow(stringResource(R.string.velero_restore_volumes_yes), restoreVolumes) { onRestoreVolumes(true) }
    ChoiceRow(stringResource(R.string.velero_restore_volumes_no), !restoreVolumes) { onRestoreVolumes(false) }
}

@Composable
private fun ChoiceRow(label: String, chosen: Boolean, onChoose: () -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected = chosen, onClick = onChoose)
        Text(label, style = MaterialTheme.typography.bodySmall)
    }
}

/** The Restore's phase and item progress until it ends, then its errors and warnings. */
@Composable
private fun VeleroRestoreProgress(backup: String, restoreName: String?, p: VeleroRestorePreview?, launchError: UiText?, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val restore = p?.restores?.firstOrNull { it.name == restoreName }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.velero_restore_title, backup)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                launchError?.let { Text(it.asString(), color = colors.bad) }
                when {
                    launchError != null -> Unit
                    restore == null -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                        Text(stringResource(R.string.velero_restore_started, restoreName.orEmpty()))
                    }
                    else -> {
                        Text(restore.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOfNotNull(
                                restore.phase,
                                restore.totalItems.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_restore_progress, restore.itemsRestored, restore.itemsRestored, restore.totalItems) },
                                countsText(restore.errors, restore.warnings),
                            ).joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall,
                        )
                        restore.validationErrors.forEach { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.bad) }
                        restore.failureReason.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.bad) }
                        if (restore.running) {
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                                Text(stringResource(R.string.velero_restore_running), style = MaterialTheme.typography.labelSmall)
                            }
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.velero_restore_close)) } },
    )
}

@Composable
private fun reasonText(reason: String): String = when (VeleroRestoreReason.from(reason)) {
    VeleroRestoreReason.PARTIALLY_FAILED -> stringResource(R.string.velero_restore_reason_partially_failed)
    VeleroRestoreReason.LOCATION_UNAVAILABLE -> stringResource(R.string.velero_restore_reason_location)
    VeleroRestoreReason.EXPIRED -> stringResource(R.string.velero_restore_reason_expired)
    VeleroRestoreReason.RESTORE_RUNNING -> stringResource(R.string.velero_restore_reason_running)
    VeleroRestoreReason.ALL_NAMESPACES -> stringResource(R.string.velero_restore_reason_all_namespaces)
    null -> reason
}

/** "2 errors, 3 warnings", null when both are 0. */
@Composable
private fun countsText(errors: Int, warnings: Int): String? = listOfNotNull(
    errors.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_errors, it, it) },
    warnings.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_warnings, it, it) },
).joinToString(", ").ifEmpty { null }
