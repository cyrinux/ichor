package name.levis.ichor.ui.argocd

import android.content.Context
import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoProject
import name.levis.ichor.model.ArgoResource
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoWindow
import name.levis.ichor.model.DEFAULT_FREEZE_MINUTES
import name.levis.ichor.model.FREEZE_DURATIONS
import name.levis.ichor.model.FreezeScope
import name.levis.ichor.model.availableFor
import name.levis.ichor.model.drifted
import name.levis.ichor.model.freezeOptions
import name.levis.ichor.model.freezeTargets
import name.levis.ichor.model.minutesUntil
import name.levis.ichor.model.projectOf
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.util.formatTime
import java.text.DateFormat
import java.time.ZoneId

// The freeze sheet, the banner of a frozen app and the dialogs ending a freeze or removing a
// window.

/** The hour "until …" freezes to: the start of a working day. */
private const val MORNING_HOUR = 9
private const val MAX_NAMED = 6
private const val MAX_REASON = 200

/**
 * Freezing Argo CD around [app]: the scope (the app, its namespace, its project), how long,
 * why, and whether manual syncs still run, with the apps it stops named before confirming.
 * [onPauseAutoSync] offers pausing auto-sync instead, on an app nothing rewrites.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ArgoFreezeSheet(
    app: ArgoApp,
    status: ArgoStatus,
    initialScope: FreezeScope = FreezeScope.APP,
    onFreeze: (ArgoProject, ArgoFreezeOptions) -> Unit,
    onPauseAutoSync: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    val project = status.projectOf(app)
    var scope by rememberSaveable { mutableStateOf(initialScope.takeIf { it.availableFor(app) } ?: FreezeScope.APP) }
    var minutes by rememberSaveable { mutableIntStateOf(DEFAULT_FREEZE_MINUTES) }
    var untilMorning by rememberSaveable { mutableStateOf(false) }
    var reason by rememberSaveable { mutableStateOf("") }
    var manualSync by rememberSaveable { mutableStateOf(true) }
    val now = remember { System.currentTimeMillis() }
    val morning = remember(now) { minutesUntil(MORNING_HOUR, now, ZoneId.systemDefault()) }
    val chosen = if (untilMorning) morning else minutes
    val targets = remember(status, app, scope) { status.freezeTargets(app, scope) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.argo_freeze_title), style = MaterialTheme.typography.titleLarge)
            if (project == null) {
                MutedText(stringResource(R.string.argo_freeze_no_project, app.project))
                return@Column
            }
            Label(stringResource(R.string.argo_freeze_scope))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FreezeScope.entries.filter { it.availableFor(app) }.forEach { s ->
                    FilterChip(selected = scope == s, onClick = { scope = s }, label = { Text(scopeLabel(s, app)) })
                }
            }
            Label(stringResource(R.string.argo_freeze_for))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FREEZE_DURATIONS.forEach { m ->
                    FilterChip(selected = !untilMorning && minutes == m, onClick = { minutes = m; untilMorning = false }, label = { Text(durationLabel(m)) })
                }
                FilterChip(
                    selected = untilMorning,
                    onClick = { untilMorning = true },
                    label = { Text(stringResource(R.string.argo_freeze_until, freezeClock(now + morning * 60_000L))) },
                )
            }
            OutlinedTextField(
                value = reason,
                onValueChange = { reason = it.take(MAX_REASON) },
                label = { Text(stringResource(R.string.argo_freeze_reason)) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f).padding(end = 12.dp)) {
                    Text(stringResource(R.string.argo_freeze_manual_sync), style = MaterialTheme.typography.bodyLarge)
                    MutedText(stringResource(R.string.argo_freeze_manual_sync_desc))
                }
                Switch(checked = manualSync, onCheckedChange = { manualSync = it })
            }
            val names = targets.take(MAX_NAMED).joinToString(", ") { it.name } + if (targets.size > MAX_NAMED) ", …" else ""
            Text(
                pluralStringResource(R.plurals.argo_freeze_preview, targets.size, targets.size, names, freezeClock(now + chosen * 60_000L)),
                style = MaterialTheme.typography.bodyMedium,
            )
            ProjectNote(project)
            Button(
                onClick = { onFreeze(project, freezeOptions(app, scope, chosen, manualSync, reason)) },
                enabled = targets.isNotEmpty(),
                modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
            ) {
                Icon(Icons.Outlined.AcUnit, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.argo_freeze_confirm), modifier = Modifier.padding(start = 8.dp))
            }
            onPauseAutoSync?.let { TextButton(onClick = it, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.argo_freeze_pause_instead)) } }
        }
    }
}

@Composable
private fun Label(text: String) {
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 4.dp))
}

@Composable
private fun scopeLabel(scope: FreezeScope, app: ArgoApp): String = when (scope) {
    FreezeScope.APP -> stringResource(R.string.argo_freeze_scope_app)
    FreezeScope.NAMESPACE -> stringResource(R.string.argo_freeze_scope_namespace, app.destination.namespace)
    FreezeScope.PROJECT -> stringResource(R.string.argo_freeze_scope_project, app.project)
}

@Composable
private fun durationLabel(minutes: Int): String =
    if (minutes % 60 == 0) stringResource(R.string.argo_freeze_hours, minutes / 60) else stringResource(R.string.argo_freeze_minutes, minutes)

/** Where the project comes from: applied from Git (server-side, where a freeze may not hold). */
@Composable
private fun ProjectNote(project: ArgoProject) {
    when {
        project.managedServerSide -> WarningNote(stringResource(R.string.argo_freeze_server_side, project.name, project.managedBy))
        project.managedBy.isNotEmpty() -> MutedText(stringResource(R.string.argo_freeze_managed, project.name, project.managedBy))
    }
}

@Composable
private fun WarningNote(text: String) {
    val warn = LocalStatusColors.current.warn
    Surface(color = warn.copy(alpha = 0.12f), contentColor = warn, shape = RoundedCornerShape(12.dp), modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.padding(12.dp), verticalAlignment = Alignment.Top) {
            Icon(Icons.Outlined.WarningAmber, contentDescription = null, modifier = Modifier.size(18.dp))
            Text(text, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(start = 8.dp))
        }
    }
}

/** "15:42" today, else with the date. */
fun freezeClock(millis: Long): String =
    if (DateUtils.isToday(millis)) formatTime(millis, DateFormat.SHORT) else formatDateTime(millis, DateFormat.SHORT, DateFormat.SHORT)

/**
 * A frozen app: until when, by whom, whether manual syncs run, what was changed by hand; "+1 h"
 * and "Unfreeze" on Ichor's freezes, the sync windows otherwise. [onFreeze] when not frozen.
 */
@Composable
fun ArgoFreezeCard(app: ArgoApp, windows: List<ArgoWindow>, busy: Boolean, onFreeze: () -> Unit, onExtend: () -> Unit, onUnfreeze: () -> Unit, onWindows: (() -> Unit)?) {
    val freeze = app.freeze
    if (freeze == null) {
        OutlinedButton(onClick = onFreeze, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
            Icon(Icons.Outlined.AcUnit, contentDescription = null, modifier = Modifier.size(18.dp))
            Text(stringResource(R.string.argo_freeze_button), modifier = Modifier.padding(start = 8.dp))
        }
        return
    }
    val tint = MaterialTheme.colorScheme.primary
    val reason = windows.mapNotNull { it.ichor?.reason?.takeIf(String::isNotBlank) }.distinct().joinToString(" · ")
    Surface(color = tint.copy(alpha = 0.10f), shape = RoundedCornerShape(16.dp), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.AcUnit, contentDescription = null, tint = tint)
                Text(
                    stringResource(R.string.argo_frozen_until, freezeClock(freeze.until)),
                    style = MaterialTheme.typography.titleSmall,
                    color = tint,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            MutedText(
                listOf(
                    if (freeze.byIchor) stringResource(R.string.argo_frozen_by_ichor) else stringResource(R.string.argo_frozen_by_window, freeze.project),
                    reason,
                    stringResource(if (freeze.manualSync) R.string.argo_frozen_manual_allowed else R.string.argo_frozen_manual_blocked),
                ).filter { it.isNotEmpty() }.joinToString(" · "),
            )
            val drifted = app.drifted
            if (drifted.isNotEmpty()) Text(pluralStringResource(R.plurals.argo_frozen_drifted, drifted.size, drifted.size), style = MaterialTheme.typography.bodySmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (freeze.byIchor) {
                    OutlinedButton(onClick = onExtend, enabled = !busy) { Text(stringResource(R.string.argo_freeze_extend_hour)) }
                    FilledTonalButton(onClick = onUnfreeze, enabled = !busy) { Text(stringResource(R.string.argo_unfreeze)) }
                } else {
                    onWindows?.let { OutlinedButton(onClick = it) { Text(stringResource(R.string.argo_windows_title)) } }
                }
            }
        }
    }
}

/**
 * Ending a freeze: what Argo CD will put back as Git has it ([drifted]), asking whether the fix
 * is committed; [onEndAndSync] syncs right after, when there is an app to sync. [covers]: the
 * apps the freeze holds, all of them resuming (a namespace or project freeze).
 */
@Composable
fun EndFreezeDialog(drifted: List<ArgoResource>, covers: Int, onEnd: () -> Unit, onEndAndSync: (() -> Unit)?, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.AcUnit, contentDescription = null) },
        title = { Text(stringResource(R.string.argo_unfreeze_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (drifted.isEmpty()) {
                    Text(stringResource(R.string.argo_unfreeze_clean))
                } else {
                    Text(stringResource(R.string.argo_unfreeze_drift))
                    drifted.take(MAX_NAMED).forEach { r ->
                        Text(
                            "${r.kind} ${listOf(r.namespace, r.name).filter { it.isNotEmpty() }.joinToString("/")}",
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                        )
                    }
                    if (drifted.size > MAX_NAMED) Text("…", style = MaterialTheme.typography.bodySmall)
                    Text(stringResource(R.string.argo_unfreeze_committed))
                }
                if (covers > 1) Text(pluralStringResource(R.plurals.argo_unfreeze_covers, covers, covers), style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            Row {
                TextButton(onClick = onEnd) { Text(stringResource(R.string.argo_unfreeze_only)) }
                onEndAndSync?.let { TextButton(onClick = it) { Text(stringResource(R.string.argo_unfreeze_and_sync)) } }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** Removing a window Ichor did not create: Argo CD may put it back when Git still has it. */
@Composable
fun RemoveWindowDialog(project: ArgoProject, onRemove: () -> Unit, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.WarningAmber, contentDescription = null, tint = colors.warn) },
        title = { Text(stringResource(R.string.argo_window_remove_title)) },
        text = {
            Text(
                if (project.managedBy.isNotEmpty()) stringResource(R.string.argo_window_remove_text, project.managedBy)
                else stringResource(R.string.argo_window_remove_text_unmanaged, project.name),
            )
        },
        confirmButton = { TextButton(onClick = onRemove) { Text(stringResource(R.string.argo_window_remove), color = colors.bad) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The outcome of each change to the sync windows, through [show]. */
@Composable
fun ArgoFreezeMessages(results: Flow<ArgoFreezeResult>, show: suspend (String) -> Unit) {
    val context = LocalContext.current
    LaunchedEffect(results) {
        results.collect { r -> show(freezeMessage(context, r)) }
    }
}

private fun freezeMessage(context: Context, r: ArgoFreezeResult): String = when (val error = r.error) {
    null -> context.getString(
        when (r.action) {
            ArgoFreezeAction.FREEZE -> R.string.argo_done_freeze
            ArgoFreezeAction.EXTEND -> R.string.argo_done_extend
            ArgoFreezeAction.UNFREEZE -> R.string.argo_done_unfreeze
            ArgoFreezeAction.CLEAR_EXPIRED -> R.string.argo_done_clear_expired
        },
    )
    else -> context.getString(R.string.argo_action_failed, error.resolve(context))
}
