package name.levis.ichor.ui.apihealth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.AuditReport
import name.levis.ichor.model.formatMegabytes
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.factory

/** The windows offered, in minutes. */
private val AUDIT_WINDOWS = listOf(5, 15, 60)

/** Reading the audit logs moves tens of MB: only when asked, never polled. */
class AuditViewModel(private val talos: TalosRepository) : LoadingViewModel<AuditReport>() {
    var minutes = 15
    override suspend fun fetch() = talos.auditAnalysis(minutes)
}

/**
 * Who loads the Kubernetes API server and what they do wrong, from the control planes' audit
 * logs read through the Talos API (os:admin): each problem with the client at fault, what it
 * does and what to change, then the busiest clients and what was read.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AuditScreen(
    onBack: () -> Unit,
    vm: AuditViewModel = viewModel(factory = factory { AuditViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    // Not saveable: after the process died the result is gone, Analyze must be offered again.
    var started by remember { mutableStateOf(false) }
    var minutes by rememberSaveable { mutableIntStateOf(vm.minutes) }
    val run = {
        vm.minutes = minutes
        started = true
        vm.refresh(reset = true)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.audit_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (started) TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = run)
                },
            )
        },
    ) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxSize()) {
            item(key = "intro") { AuditIntro(minutes, onMinutes = { minutes = it }, onRun = run, running = started && state !is UiState.Loaded && state !is UiState.Failed) }
            if (started) auditResult(state, vm::refresh)
        }
    }
}

@Composable
private fun AuditIntro(minutes: Int, onMinutes: (Int) -> Unit, onRun: () -> Unit, running: Boolean) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        MutedText(stringResource(R.string.audit_intro))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            AUDIT_WINDOWS.forEach { m ->
                FilterChip(selected = minutes == m, onClick = { onMinutes(m) }, label = { Text(stringResource(R.string.audit_window, m)) })
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Button(onClick = onRun, enabled = !running) { Text(stringResource(R.string.audit_run)) }
            if (running) {
                CircularProgressIndicator(Modifier.padding(4.dp))
                MutedText(stringResource(R.string.audit_reading))
            }
        }
        HorizontalDivider()
    }
}

private fun LazyListScope.auditResult(state: UiState<AuditReport>, retry: () -> Unit) {
    when (state) {
        UiState.Loading -> Unit
        is UiState.Failed -> item(key = "error") { ErrorBox(state.message, retry) }
        is UiState.Loaded -> auditReport(state.data)
    }
}

private fun LazyListScope.auditReport(r: AuditReport) {
    item(key = "summary") {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
            Text(
                stringResource(
                    R.string.audit_summary,
                    r.requests,
                    localizedDuration(r.seconds.toLong()),
                    r.nodes.size,
                    formatMegabytes(r.nodes.sumOf { it.bytes }),
                ),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
    if (r.requests == 0) {
        item(key = "empty") { EmptyText(stringResource(R.string.audit_empty)) }
        return
    }
    item(key = "findings-title") { SectionTitle(stringResource(R.string.audit_findings), Modifier.padding(horizontal = 16.dp)) }
    if (r.findings.isEmpty()) {
        item(key = "no-finding") { EmptyText(stringResource(R.string.audit_no_finding)) }
    }
    items(r.findings.withIndex().toList(), key = { "finding-${it.index}" }) { FindingCard(it.value) }
    if (r.actors.isNotEmpty()) {
        item(key = "actors-title") { SectionTitle(stringResource(R.string.audit_actors), Modifier.padding(horizontal = 16.dp)) }
        items(r.actors, key = { "actor-${it.actor.user}/${it.actor.agent}" }) { ActorRow(it) }
    }
    item(key = "nodes") {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
            SectionTitle(stringResource(R.string.audit_nodes))
            r.nodes.forEach { NodeReadRow(it) }
        }
    }
}
