package name.levis.ichor.ui.upgrade

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ClusterUpgradeNode
import name.levis.ichor.model.ClusterUpgradePlan
import name.levis.ichor.model.TalosRelease
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors

/** What the plan screen asks the roll: the version, a drain per node, the warnings understood. */
data class ClusterUpgradeChoice(val version: String, val drain: Boolean, val acknowledged: Boolean)

/**
 * The rolling upgrade before it starts: the version, then every node in the order the roll
 * follows with its checks, and one Start (Continue after an interrupted roll).
 */
@Composable
fun ClusterUpgradePlanView(
    releases: UiState<List<TalosRelease>>,
    version: String,
    onVersion: (String) -> Unit,
    plan: UiState<ClusterUpgradePlan>?,
    onRetry: () -> Unit,
    demo: Boolean,
    busyWith: String?,
    onStart: (ClusterUpgradeChoice) -> Unit,
) {
    val colors = LocalStatusColors.current
    var drain by rememberSaveable { mutableStateOf(true) }
    var understood by rememberSaveable(version) { mutableStateOf(false) }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (demo) InfoNotice(stringResource(R.string.cluster_upgrade_demo))
        MutedText(stringResource(R.string.cluster_upgrade_intro))

        SectionTitle(stringResource(R.string.cluster_upgrade_version))
        VersionPicker(releases, version, onVersion)

        when (plan) {
            null -> Unit
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(plan.message, onRetry)
            is UiState.Loaded -> {
                val p = plan.data
                val warnings = p.warnings + p.nodes.flatMap { n -> n.warnings.map { "${n.name}: $it" } }
                if (!p.drain) {
                    ToggleRow(
                        title = stringResource(R.string.cluster_upgrade_drain),
                        description = stringResource(R.string.cluster_upgrade_drain_desc),
                        checked = drain,
                        onChange = { drain = it },
                    )
                }
                SectionTitle(stringResource(R.string.cluster_upgrade_order, p.done, p.nodes.size))
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(vertical = 8.dp)) { p.nodes.forEachIndexed { i, n -> PlanNodeRow(i + 1, n) } }
                }
                Checks(p.allBlockers(), warnings, understood) { understood = it }
                if (p.upToDate) Text(stringResource(R.string.cluster_upgrade_up_to_date, p.version), color = colors.ok)
                if (busyWith != null) Text(busyWith, color = colors.warn, style = MaterialTheme.typography.bodySmall)
                MutedText(stringResource(R.string.cluster_upgrade_keep_open))
                Button(
                    onClick = { onStart(ClusterUpgradeChoice(p.version, drain && !p.drain, warnings.isEmpty() || understood)) },
                    enabled = p.canStart && busyWith == null && (warnings.isEmpty() || understood),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(if (p.continues) R.string.cluster_upgrade_continue else R.string.cluster_upgrade_start))
                }
            }
        }
    }
}

@Composable
private fun VersionPicker(releases: UiState<List<TalosRelease>>, version: String, onVersion: (String) -> Unit) {
    when (releases) {
        UiState.Loading -> LoadingBox()
        // Offline: the version the overview offered is still there to pick.
        is UiState.Failed -> MutedText(releases.message.asString())
        is UiState.Loaded -> Unit
    }
    val offered = ((releases as? UiState.Loaded)?.data.orEmpty().map { it.version } + version).filter { it.isNotEmpty() }.distinct().take(MAX_VERSIONS)
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        offered.forEach { v ->
            FilterChip(selected = v == version, onClick = { onVersion(v) }, label = { Text(v, fontFamily = FontFamily.Monospace) })
        }
    }
}

private const val MAX_VERSIONS = 6

@Composable
private fun PlanNodeRow(position: Int, node: ClusterUpgradeNode) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("$position.", style = MaterialTheme.typography.labelLarge)
            Text(node.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            when {
                node.state == ClusterUpgradeNode.DONE -> StatusPill(stringResource(R.string.cluster_upgrade_node_done), colors.ok)
                node.blockers.isNotEmpty() -> StatusPill(stringResource(R.string.cluster_upgrade_node_blocked), colors.bad)
                node.warnings.isNotEmpty() -> StatusPill(stringResource(R.string.cluster_upgrade_node_warning), colors.warn)
            }
        }
        val role = stringResource(if (node.controlPlane) R.string.upgrade_control_plane else R.string.cluster_upgrade_worker)
        val leader = if (node.leader) " · " + stringResource(R.string.cluster_upgrade_leader) else ""
        MutedText("$role$leader · ${node.from.ifEmpty { "?" }}")
    }
}

@Composable
private fun Checks(blockers: List<String>, warnings: List<String>, understood: Boolean, onUnderstood: (Boolean) -> Unit) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SectionTitle(stringResource(R.string.upgrade_checks))
            blockers.forEach { Text("✕ $it", color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
            warnings.forEach { Text("! $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium) }
            if (warnings.isNotEmpty()) {
                Row(
                    Modifier.fillMaxWidth().toggleable(value = understood, role = Role.Checkbox, onValueChange = onUnderstood),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Checkbox(checked = understood, onCheckedChange = null, modifier = Modifier.padding(end = 12.dp, top = 6.dp, bottom = 6.dp))
                    Text(stringResource(R.string.cluster_upgrade_understood), style = MaterialTheme.typography.bodyMedium)
                }
            }
            if (blockers.isEmpty() && warnings.isEmpty()) {
                Text(stringResource(R.string.upgrade_no_issues), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}
