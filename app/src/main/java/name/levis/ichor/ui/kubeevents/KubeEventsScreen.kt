package name.levis.ichor.ui.kubeevents

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.kubeEventsText
import name.levis.ichor.model.matching
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.workloads.KubeFilters
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.util.formatDateTime
import java.text.DateFormat

/**
 * The cluster's Kubernetes events, live: newest first, coalesced by object, reason and type,
 * in the namespace the Kubernetes screens list (or every one), Warnings only on demand. A
 * search narrows what is shown; pause holds the list still while new rows are counted; a
 * tap opens the object an event is about ([onObject]) when its kind is known. Streams only
 * while visible.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KubeEventsScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: KubeEventsViewModel = viewModel(factory = factory { KubeEventsViewModel(app.kubeRepository) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val context = LocalContext.current
    val state by vm.state.collectAsStateWithLifecycle()
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.kubeRepository) })
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val control = rememberKubeScope(app, namespaces, mask.enabled)
    var warningsOnly by rememberSaveable { mutableStateOf(false) }
    var query by rememberSaveable { mutableStateOf("") }
    // Bumped by Retry: the same stream again once it ended.
    var attempt by rememberSaveable { mutableIntStateOf(0) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val namespace = control.scope.namespace
    // Stream only while visible: leaving the screen or backgrounding the app cancels it. The
    // privacy mask is applied by the core as rows come: a change starts over, masked or not.
    LaunchedEffect(lifecycle, control.ready, namespace, warningsOnly, mask.enabled, attempt) {
        if (control.ready) lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.stream(namespace, warningsOnly, mask.enabled) }
    }
    // Re-render every 15 s so relative times stay true.
    val now by produceState(System.currentTimeMillis()) {
        while (true) {
            delay(15_000)
            value = System.currentTimeMillis()
        }
    }
    val rows = state.feed.rows
    val shown = remember(rows, query) { rows.matching(query) }
    val listed = remember(rows) { rows.map { it.regarding.namespace }.filter { it.isNotEmpty() }.distinct().sorted() }
    val shareTitle = stringResource(R.string.kube_events_share)

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.kube_events_stream_title))
                        Text(namespace ?: stringResource(R.string.workloads_all_namespaces), style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (state.feed.paused) {
                        TooltipIconButton(Icons.Outlined.PlayArrow, stringResource(R.string.kube_events_resume), onClick = vm::resume)
                    } else {
                        TooltipIconButton(Icons.Outlined.Pause, stringResource(R.string.kube_events_pause), onClick = vm::pause)
                    }
                    TooltipIconButton(
                        Icons.Outlined.Share,
                        shareTitle,
                        onClick = { shareText(context, kubeEventsText(shown) { formatDateTime(it, DateFormat.SHORT, DateFormat.MEDIUM) }, shareTitle) },
                        enabled = shown.isNotEmpty(),
                    )
                },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            KubeFilters(control, listed, query, { query = it }, R.string.kube_events_search)
            Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(
                    selected = warningsOnly,
                    onClick = { warningsOnly = !warningsOnly },
                    label = { Text(stringResource(R.string.kube_events_warnings_only)) },
                )
            }
            KubeEventsStatusLine(state, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp))
            if (state.feed.paused) PausedBar(state.feed.pendingCount, onResume = vm::resume)
            if (!state.streaming && state.error != null) {
                TextButton(onClick = { attempt++ }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.common_retry)) }
            }
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            when {
                !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
                !state.received && state.streaming -> LoadingBox(rest)
                shown.isEmpty() -> EmptyText(emptyOrNoMatch(query, R.string.kube_events_stream_empty, R.string.kube_events_no_match), rest)
                else -> LazyColumn(rest.fillMaxSize()) {
                    items(shown, key = { it.key }) { e ->
                        KubeEventItem(e, now, showNamespace = namespace == null, onClick = e.regarding.ref?.let { ref -> { onObject(ref) } })
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

/** Paused: how many rows came or changed since, and the way back to live. */
@Composable
private fun PausedBar(pending: Int, onResume: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        MutedText(
            if (pending > 0) pluralStringResource(R.plurals.kube_events_pending, pending, pending) else stringResource(R.string.kube_events_paused),
            modifier = Modifier.weight(1f),
        )
        TextButton(onClick = onResume) { Text(stringResource(R.string.kube_events_resume)) }
    }
}
