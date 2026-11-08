package name.levis.ichor.ui.logsearch

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.sourceFor
import name.levis.ichor.model.LOG_SEARCH_HOURS
import name.levis.ichor.model.LOG_SEARCH_MAX_HOURS
import name.levis.ichor.model.LOGS_KIND_VICTORIALOGS
import name.levis.ichor.model.LogsLine
import name.levis.ichor.model.LogsResult
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.asText
import name.levis.ichor.model.isDemo
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorAssistChip
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.logs.rememberLogPalette
import name.levis.ichor.ui.metrics.SourceDialog
import name.levis.ichor.ui.workloads.deleteSharedLogs
import name.levis.ichor.ui.workloads.shareLog

/**
 * Searches the cluster's log backend (Loki, VictoriaLogs; found in the cluster through the service
 * proxy, or at a URL set by the user): a query with a label picker, a time range, saved queries.
 * [initialQuery], from a pod's log sheet, is searched at once over the last 24 hours.
 */
@Composable
fun LogSearchScreen(onBack: () -> Unit, initialQuery: String = "") {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val invalidations by app.talosRepository.invalidations.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.fingerprint ?: return
    val key = "log-search-$fingerprint-$invalidations-$initialQuery"
    val vm: LogSearchViewModel = viewModel(
        key = key,
        factory = factory { LogSearchViewModel(app.logsBackendRepository, app.logsBackendStore, fingerprint, initialQuery, config?.activeSummary?.isDemo == true) },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    var sourceOpen by remember { mutableStateOf(false) }
    var labelsOpen by remember { mutableStateOf(false) }
    LaunchedEffect(key) { vm.load() }
    val context = LocalContext.current
    DisposableEffect(Unit) { onDispose { deleteSharedLogs(context) } }
    val title = stringResource(R.string.log_search_title)
    val found = (state.result as? UiState.Loaded)?.data

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(title) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.pod_logs_share), enabled = !found?.lines.isNullOrEmpty(), onClick = {
                        shareLog(context, "log-search", found?.asText(::formatLineTime).orEmpty())
                    })
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::search)
                    TooltipIconButton(Icons.Outlined.Tune, stringResource(R.string.log_search_source), onClick = { sourceOpen = true })
                },
            )
        },
    ) { padding ->
        val modifier = Modifier.pageContent(padding)
        when {
            !state.ready -> LoadingBox(modifier)
            state.source == null -> NoSource(state, modifier, onSetUp = { sourceOpen = true }, onSearch = { vm.discover(useFirst = true) })
            else -> SearchContent(state, vm, modifier, onLabels = {
                labelsOpen = true
                vm.pickLabels()
            })
        }
    }

    if (labelsOpen) {
        LogSearchLabelsSheet(
            label = state.pickedLabel,
            picker = state.picker,
            onLabel = vm::pickLabel,
            onValue = {
                vm.pickValue(it)
                labelsOpen = false
            },
            onBack = vm::pickLabels,
            onDismiss = { labelsOpen = false },
        )
    }
    if (sourceOpen) {
        SourceDialog(
            current = state.source,
            discovered = state.discovered,
            discovering = state.discovering,
            discoveryError = state.discoveryError,
            onDiscover = { vm.discover() },
            onTest = vm::test,
            onSave = vm::setSource,
            onDismiss = { sourceOpen = false },
            title = stringResource(R.string.log_search_source),
            noneFound = stringResource(R.string.log_search_none_found),
            reachable = stringResource(R.string.log_search_reachable),
        )
    }
}

@Composable
private fun SearchContent(state: LogSearchState, vm: LogSearchViewModel, modifier: Modifier, onLabels: () -> Unit) {
    val source = state.source ?: return
    val proxied = source.takeIf { it.mode == PromSource.MODE_PROXY && it.service.isNotEmpty() }
    val denial = rememberKubeCanDenial("get", "", "services/proxy", proxied?.namespace.orEmpty(), proxied?.service.orEmpty()).takeIf { proxied != null }
    Column(modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        MutedText(stringResource(R.string.log_search_source_line, source.label), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        KubeDenialNote(denial, Modifier.padding(horizontal = 16.dp))
        OutlinedTextField(
            state.query, vm::setQuery,
            label = { Text(stringResource(R.string.log_search_query)) },
            placeholder = { Text(if (source.kind == LOGS_KIND_VICTORIALOGS) "namespace:\"demo\" AND timeout" else "{namespace=\"demo\"} |= \"timeout\"", fontFamily = FontFamily.Monospace) },
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            maxLines = 4,
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
        )
        Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            IchorButton(onClick = vm::search, enabled = state.query.isNotBlank()) { Text(stringResource(R.string.log_search_run)) }
            IchorTextButton(onClick = onLabels) { Text(stringResource(R.string.log_search_labels)) }
            val isSaved = state.query.trim() in state.saved
            TooltipIconButton(
                if (isSaved) Icons.Outlined.Bookmark else Icons.Outlined.BookmarkBorder,
                stringResource(if (isSaved) R.string.log_search_unsave else R.string.log_search_save),
                enabled = state.query.isNotBlank(),
                onClick = vm::toggleSaved,
            )
        }
        RangeChips(state.hours, vm::setHours)
        if (state.saved.isNotEmpty()) {
            LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(state.saved, key = { it }) { query ->
                    IchorAssistChip(onClick = { vm.openSaved(query) }, label = {
                        Text(query, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(end = 2.dp))
                    })
                }
            }
        }
        Box(Modifier.weight(1f)) {
            when (val r = state.result) {
                null -> EmptyText(stringResource(R.string.log_search_idle))
                UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                is UiState.Failed -> ErrorBox(r.message, vm::search)
                is UiState.Loaded -> ResultLines(r.data)
            }
        }
    }
}

@Composable
private fun RangeChips(hours: Int, onHours: (Int) -> Unit) {
    var custom by remember { mutableStateOf(false) }
    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        items(LOG_SEARCH_HOURS, key = { it }) { h ->
            IchorFilterChip(selected = hours == h, onClick = { onHours(h) }, label = {
                Text(if (h % HOURS_PER_DAY == 0 && h > HOURS_PER_DAY) stringResource(R.string.log_search_range_days, h / HOURS_PER_DAY) else stringResource(R.string.log_search_range_hours, h))
            })
        }
        item(key = "custom") {
            IchorFilterChip(selected = hours !in LOG_SEARCH_HOURS, onClick = { custom = true }, label = { Text(stringResource(R.string.log_search_range_custom)) })
        }
    }
    if (custom) CustomRangeDialog(hours, onHours = { custom = false; onHours(it) }, onDismiss = { custom = false })
}

@Composable
private fun CustomRangeDialog(hours: Int, onHours: (Int) -> Unit, onDismiss: () -> Unit) {
    var text by remember { mutableStateOf(hours.toString()) }
    val value = text.toIntOrNull()?.takeIf { it in 1..LOG_SEARCH_MAX_HOURS }
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.log_search_range_custom)) },
        text = {
            OutlinedTextField(
                text, { text = it.filter(Char::isDigit).take(3) },
                label = { Text(stringResource(R.string.log_search_custom_hours, LOG_SEARCH_MAX_HOURS)) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                singleLine = true,
            )
        },
        confirmButton = { IchorTextButton(enabled = value != null, onClick = { value?.let(onHours) }) { Text(stringResource(R.string.log_search_run)) } },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun ResultLines(result: LogsResult) {
    if (result.lines.isEmpty()) {
        EmptyText(stringResource(R.string.pod_logs_empty))
        return
    }
    Column(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.weight(1f).fillMaxWidth(), contentPadding = PaddingValues(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            items(result.lines) { LogLine(it) }
        }
        val count = stringResource(R.string.log_search_count, result.stats.returned)
        MutedText(if (result.stats.truncated) "$count · ${stringResource(R.string.workload_logs_truncated)}" else count, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
    }
}

@Composable
private fun LogLine(line: LogsLine) {
    val palette = rememberLogPalette()
    val pod = line.labels["pod"] ?: line.labels["kubernetes_pod_name"]
    Column {
        MutedText(listOfNotNull(formatLineTime(line.time), pod).joinToString("  ·  "))
        Text(line.text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = palette.text)
    }
}

@Composable
private fun NoSource(state: LogSearchState, modifier: Modifier, onSetUp: () -> Unit, onSearch: () -> Unit) {
    Column(modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (state.discovering) {
            Text(stringResource(R.string.log_search_searching))
            LinearProgressIndicator(Modifier.fillMaxWidth())
            return@Column
        }
        Text(stringResource(R.string.log_search_none_found), style = MaterialTheme.typography.titleMedium)
        MutedText(state.discoveryError ?: stringResource(R.string.log_search_none_found_hint))
        IchorButton(onClick = onSetUp) { Text(stringResource(R.string.metrics_set_up)) }
        IchorTextButton(onClick = onSearch) { Text(stringResource(R.string.metrics_search_again)) }
    }
}

private const val HOURS_PER_DAY = 24
private val LineTimeFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss").withZone(ZoneId.systemDefault())

private fun formatLineTime(millis: Long): String = LineTimeFormat.format(Instant.ofEpochMilli(millis))

/** The active cluster's log backend, null until known or without one: what "Open in" offers. */
@Composable
internal fun rememberLogsBackend(): PromSource? {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val fingerprint = config?.activeSummary?.fingerprint
    val source by produceState<PromSource?>(null, fingerprint) {
        value = fingerprint?.let { app.logsBackendRepository.sourceFor(app.logsBackendStore, it) }
    }
    return source
}

/** "Open in Loki" or "Open in VictoriaLogs", by the backend's kind. */
@Composable
internal fun openInLogsBackendLabel(source: PromSource): String =
    stringResource(if (source.kind == LOGS_KIND_VICTORIALOGS) R.string.log_search_open_victorialogs else R.string.log_search_open_loki)
