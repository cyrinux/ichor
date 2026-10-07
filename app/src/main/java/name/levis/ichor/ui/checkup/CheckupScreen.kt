package name.levis.ichor.ui.checkup

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.CheckupSection
import name.levis.ichor.model.CheckupSectionId
import name.levis.ichor.model.CheckupSeverity
import name.levis.ichor.model.CheckupStatus
import name.levis.ichor.model.count
import name.levis.ichor.model.level
import name.levis.ichor.model.shownSections
import name.levis.ichor.model.state
import name.levis.ichor.model.verdict
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/** A checkup lists the cluster's pods and asks every kubelet: loaded on demand, never polled. */
class CheckupViewModel(private val talos: TalosRepository) : LoadingViewModel<CheckupReport>() {
    override suspend fun fetch() = talos.checkup()
}

/**
 * The cluster checkup (os:admin): what no other screen shows, one card per kind of trouble.
 * A card opens on its findings, each worded with what to do; the capacity, storage, nodes and
 * Helm cards also list what they measured.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CheckupScreen(
    onBack: () -> Unit,
    vm: CheckupViewModel = viewModel(factory = factory { CheckupViewModel(app.talosRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.checkup_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.padding(padding)) { report -> CheckupList(report) }
    }
}

@Composable
private fun CheckupList(report: CheckupReport) {
    // Fixed while the report is on screen: the ages must not drift between recompositions.
    val now = remember(report) { System.currentTimeMillis() }
    val sections = report.shownSections
    // The sections in trouble start open; the user's choices then hold across rotations.
    var open by rememberSaveable(report) {
        mutableStateOf(sections.filter { it.state == CheckupStatus.CRITICAL || it.state == CheckupStatus.WARNING }.map { it.id })
    }

    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        item(key = "verdict") { CheckupVerdict(report, sections.size) }
        items(sections, key = { it.id }) { section ->
            val expanded = section.id in open
            SectionCard(section, expanded, onToggle = { open = if (expanded) open - section.id else open + section.id }) {
                SectionBody(section, report, now)
            }
        }
    }
}

@Composable
fun CheckupStatus.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        CheckupStatus.CRITICAL -> colors.bad
        CheckupStatus.WARNING -> colors.warn
        CheckupStatus.OK -> colors.ok
        CheckupStatus.UNKNOWN, CheckupStatus.ABSENT -> colors.muted
    }
}

/** The verdict, how many findings of each weight, and what the checkup is. */
@Composable
private fun CheckupVerdict(report: CheckupReport, sections: Int) {
    val verdict = report.verdict
    val critical = report.count(CheckupSeverity.CRITICAL)
    val warnings = report.count(CheckupSeverity.WARNING)
    Column(Modifier.padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            StatusPill(
                stringResource(
                    when (verdict) {
                        CheckupStatus.CRITICAL -> R.string.checkup_verdict_critical
                        CheckupStatus.WARNING -> R.string.checkup_verdict_warning
                        else -> R.string.checkup_verdict_ok
                    },
                ),
                verdict.color(),
            )
            if (report.kubeVersion.isNotEmpty()) MutedText("Kubernetes ${report.kubeVersion}")
        }
        Text(
            if (critical + warnings == 0) {
                stringResource(R.string.checkup_all_clear, sections.toString())
            } else {
                stringResource(R.string.checkup_counts, critical.toString(), warnings.toString())
            },
            style = MaterialTheme.typography.bodyMedium,
        )
        MutedText(stringResource(R.string.checkup_intro))
    }
}

/** A section's header (icon, name, what it looks for, its count) and, opened, its [content]. */
@Composable
private fun SectionCard(section: CheckupSection, expanded: Boolean, onToggle: () -> Unit, content: @Composable () -> Unit) {
    val look = sectionLook(section.id)
    val status = section.state
    OutlinedCard(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().expandable(expanded, onToggle = onToggle).padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (look != null) Icon(look.icon, contentDescription = null, tint = status.color(), modifier = Modifier.size(24.dp))
            Column(Modifier.weight(1f)) {
                Text(look?.let { stringResource(it.title) } ?: section.id, style = MaterialTheme.typography.titleSmall)
                if (look != null) MutedText(stringResource(look.hint), maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            SectionBadges(section)
            Icon(if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
        }
        AnimatedVisibility(expanded) {
            Column(Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                content()
            }
        }
    }
}

/** How many findings of each weight a section has: a tick when none, a question mark when unread. */
@Composable
private fun SectionBadges(section: CheckupSection) {
    val colors = LocalStatusColors.current
    val critical = section.findings.count { it.level == CheckupSeverity.CRITICAL }
    val warnings = section.findings.count { it.level == CheckupSeverity.WARNING }
    val notes = section.findings.size - critical - warnings
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        if (critical > 0) TagBadge(critical.toString(), colors.bad)
        if (warnings > 0) TagBadge(warnings.toString(), colors.warn)
        if (notes > 0) TagBadge(notes.toString(), colors.muted)
        when {
            critical + warnings + notes > 0 -> Unit
            section.state == CheckupStatus.UNKNOWN -> TagBadge("?", colors.muted)
            else -> Icon(Icons.Outlined.CheckCircle, contentDescription = stringResource(R.string.checkup_nothing_found), tint = colors.ok, modifier = Modifier.size(20.dp))
        }
    }
}

/** A section's findings, then what it measured: node requests, volume levels, taints, releases. */
@Composable
private fun SectionBody(section: CheckupSection, report: CheckupReport, now: Long) {
    if (section.error.isNotEmpty()) InlineError(stringResource(R.string.checkup_section_error, section.error))
    if (section.findings.isEmpty() && section.error.isEmpty()) {
        MutedText(stringResource(R.string.checkup_nothing_in, section.checked.toString()))
    }
    section.findings.forEach { FindingCard(it, now) }
    if (section.truncated > 0) MutedText(stringResource(R.string.checkup_truncated, section.truncated.toString()))
    when (section.id) {
        CheckupSectionId.CAPACITY -> NodeRequests(report.nodes)
        CheckupSectionId.NODES -> NodeTaints(report.nodes)
        CheckupSectionId.STORAGE -> VolumeLevels(report.volumes)
        CheckupSectionId.HELM -> HelmReleases(report.releases, now)
    }
}
