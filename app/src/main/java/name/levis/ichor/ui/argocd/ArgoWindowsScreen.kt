package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.EventAvailable
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoWindow
import name.levis.ichor.model.FREEZE_EXTEND_MINUTES
import name.levis.ichor.model.ProjectWindow
import name.levis.ichor.model.WindowSection
import name.levis.ichor.model.windowSections
import name.levis.ichor.monitor.freezeReminderHook
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo
import name.levis.ichor.ui.components.pageContent

/**
 * Every sync window of every project: the active ones (Ichor's freezes with "+1 h" and "End"),
 * the upcoming ones, and Ichor's ended freezes. A window Ichor did not create can be removed
 * after a warning: Git may put it back.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoWindowsScreen(onBack: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val vm: ArgoViewModel = viewModel(factory = factory { ArgoViewModel(app.talosRepository, freezeReminderHook(app)) })
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val generation by app.configRepository.generation.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation, invalidations) { vm.load(Triple(config?.activeContext, generation, invalidations)) }
    ArgoPolling(vm)
    val snackbar = remember { SnackbarHostState() }
    ArgoFreezeMessages(vm.freezeResults) { snackbar.showSnackbar(it) }
    var ending by remember { mutableStateOf<ProjectWindow?>(null) }
    var removing by remember { mutableStateOf<ProjectWindow?>(null) }

    ending?.let { pw ->
        EndFreezeDialog(
            drifted = emptyList(),
            covers = pw.window.apps,
            onEnd = { ending = null; vm.freeze(pw.project, ArgoFreezeAction.UNFREEZE, listOf(ArgoFreezeOptions(window = pw.window.id))) },
            onEndAndSync = null,
            onDismiss = { ending = null },
        )
    }
    removing?.let { pw ->
        RemoveWindowDialog(
            pw.project,
            onRemove = { removing = null; vm.freeze(pw.project, ArgoFreezeAction.UNFREEZE, listOf(ArgoFreezeOptions(window = pw.window.id, fromGit = true))) },
            onDismiss = { removing = null },
        )
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.argo_windows_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        Loaded(state, vm::refresh, modifier, freshness = true) { data ->
            Windows(
                data,
                busy = { vm.freezeBusy(busy, it.project) },
                onExtend = { vm.freeze(it.project, ArgoFreezeAction.EXTEND, listOf(ArgoFreezeOptions(window = it.window.id, minutes = FREEZE_EXTEND_MINUTES))) },
                onEnd = { ending = it },
                onRemove = { removing = it },
                onClear = { pws ->
                    pws.map { it.project }.distinctBy { it.key }.forEach { vm.freeze(it, ArgoFreezeAction.CLEAR_EXPIRED, listOf(ArgoFreezeOptions())) }
                },
            )
        }
    }
}

@Composable
private fun Windows(
    status: ArgoStatus,
    busy: (ProjectWindow) -> Boolean,
    onExtend: (ProjectWindow) -> Unit,
    onEnd: (ProjectWindow) -> Unit,
    onRemove: (ProjectWindow) -> Unit,
    onClear: (List<ProjectWindow>) -> Unit,
) {
    val sections = status.windowSections()
    if (sections.values.all { it.isEmpty() }) {
        EmptyText(stringResource(R.string.argo_windows_none))
        return
    }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        WindowSection.entries.forEach { section ->
            val list = sections.getValue(section)
            if (list.isEmpty()) return@forEach
            item(key = "title-$section") {
                Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    SectionTitle(stringResource(section.title), Modifier.weight(1f))
                    if (section == WindowSection.EXPIRED) TextButton(onClick = { onClear(list) }) { Text(stringResource(R.string.argo_windows_clear)) }
                }
            }
            // Two identical windows (possible from Git) share an id: the position tells them apart.
            itemsIndexed(list, key = { i, pw -> "${pw.key}#$i" }) { _, pw ->
                WindowRow(pw, section, busy(pw), onExtend = { onExtend(pw) }, onEnd = { onEnd(pw) }, onRemove = { onRemove(pw) })
                HorizontalDivider()
            }
        }
    }
}

private val WindowSection.title: Int
    get() = when (this) {
        WindowSection.ACTIVE -> R.string.argo_windows_active
        WindowSection.UPCOMING -> R.string.argo_windows_upcoming
        WindowSection.EXPIRED -> R.string.argo_windows_expired
    }

@Composable
private fun WindowRow(pw: ProjectWindow, section: WindowSection, busy: Boolean, onExtend: () -> Unit, onEnd: () -> Unit, onRemove: () -> Unit) {
    val w = pw.window
    val colors = LocalStatusColors.current
    val ichor = w.ichor
    val (icon, tint) = when {
        !w.isDeny -> Icons.Outlined.EventAvailable to colors.ok
        ichor != null -> Icons.Outlined.AcUnit to MaterialTheme.colorScheme.primary
        else -> Icons.Outlined.Block to colors.warn
    }
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.Top) {
        Icon(icon, contentDescription = stringResource(if (w.isDeny) R.string.argo_window_deny else R.string.argo_window_allow), tint = tint, modifier = Modifier.size(22.dp))
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("${pw.project.name} · ${scopeText(w)}", style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
            MutedText(
                listOf(
                    if (ichor != null) stringResource(R.string.argo_window_ichor) else stringResource(R.string.argo_window_not_ichor),
                    ichor?.reason.orEmpty(),
                    if (ichor == null) listOf(w.schedule, w.duration, w.timeZone).filter { it.isNotEmpty() }.joinToString(" ") else "",
                ).filter { it.isNotEmpty() }.joinToString(" · "),
            )
            WhenLine(w, section)
            MutedText(
                listOfNotNull(
                    pluralStringResource(R.plurals.argo_window_apps_count, w.apps, w.apps),
                    if (w.isDeny) stringResource(if (w.manualSync) R.string.argo_frozen_manual_allowed else R.string.argo_frozen_manual_blocked) else null,
                ).joinToString(" · "),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(top = 4.dp)) {
                when {
                    ichor != null && section == WindowSection.ACTIVE -> {
                        OutlinedButton(onClick = onExtend, enabled = !busy) { Text(stringResource(R.string.argo_freeze_extend_hour)) }
                        FilledTonalButton(onClick = onEnd, enabled = !busy) { Text(stringResource(R.string.argo_unfreeze)) }
                    }
                    ichor == null -> OutlinedButton(onClick = onRemove, enabled = !busy) { Text(stringResource(R.string.argo_window_remove)) }
                }
            }
        }
    }
}

/** "38 min left" over a bar while active, "Starts 22:00" before, "Ended 3 h ago" after. */
@Composable
private fun WhenLine(w: ArgoWindow, section: WindowSection) {
    val now = System.currentTimeMillis()
    when {
        w.error.isNotEmpty() -> Text(stringResource(R.string.argo_window_unreadable, w.error), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
        section == WindowSection.EXPIRED -> MutedText(stringResource(R.string.argo_window_ended, timeAgo(w.endsAt, now)))
        section == WindowSection.ACTIVE -> {
            val total = (w.endsAt - w.start).coerceAtLeast(1)
            Text(stringResource(R.string.argo_window_left, localizedDuration((w.endsAt - now).coerceAtLeast(0) / 1000)), style = MaterialTheme.typography.bodySmall)
            LinearProgressIndicator(
                progress = { ((now - w.start).toFloat() / total).coerceIn(0f, 1f) },
                strokeCap = StrokeCap.Round,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        w.start > 0 -> MutedText(stringResource(R.string.argo_window_starts, freezeClock(w.start)))
    }
}

/** "namespace demo", "web-api, worker", "all apps". */
@Composable
private fun scopeText(w: ArgoWindow): String {
    val parts = buildList {
        if (w.applications.isNotEmpty()) add(if (w.applications == listOf("*")) stringResource(R.string.argo_window_all_apps) else w.applications.joinToString(", "))
        if (w.namespaces.isNotEmpty()) add(stringResource(R.string.argo_window_namespaces, w.namespaces.joinToString(", ")))
        if (w.clusters.isNotEmpty()) add(stringResource(R.string.argo_window_clusters, w.clusters.joinToString(", ")))
    }
    // A window without selectors matches no app.
    return parts.joinToString(" · ").ifEmpty { "—" }
}
