package name.levis.ichor.ui.maintenance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DrainPod
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.drainGroups
import name.levis.ichor.model.maintenanceAcknowledged
import name.levis.ichor.model.maintenanceCanStart
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.theme.LocalStatusColors

/** Choices made on the plan, passed to the confirmation and the run. */
data class MaintenanceChoice(val action: MaintenanceAction, val includeBare: Boolean, val acknowledged: Boolean)

/**
 * The maintenance plan: what follows the drain, the pods it evicts or leaves, and the checks
 * of a reboot or shutdown. [busyWith] names what already runs in the app (start disabled).
 */
@Composable
fun MaintenancePlanView(plan: MaintenancePlan, demo: Boolean, busyWith: String?, onStart: (MaintenanceChoice) -> Unit) {
    val colors = LocalStatusColors.current
    var action by rememberSaveable { mutableStateOf(MaintenanceAction.REBOOT) }
    var includeBare by rememberSaveable { mutableStateOf(false) }
    var ticked by rememberSaveable(plan.acknowledge) { mutableStateOf(setOf<Int>()) }
    val groups = remember(plan.pods) { plan.drainGroups() }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (demo) InfoNotice(stringResource(R.string.maintenance_demo))
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                InfoRow(stringResource(R.string.maintenance_kube_node), plan.kubeNode.ifEmpty { "—" }, mono = true)
                if (plan.controlPlane) InfoRow(stringResource(R.string.upgrade_role), stringResource(R.string.upgrade_control_plane))
                if (plan.cordoned) StatusPill(stringResource(R.string.maintenance_already_cordoned), colors.warn, Modifier.padding(top = 6.dp))
            }
        }

        SectionTitle(stringResource(R.string.maintenance_action))
        ActionPicker(action) { action = it }

        PodGroup(stringResource(R.string.maintenance_pods_evict, groups.evict.size), groups.evict, stringResource(R.string.maintenance_no_pods))
        if (groups.bare.isNotEmpty()) {
            ToggleRow(
                title = stringResource(R.string.maintenance_include_bare),
                description = stringResource(R.string.maintenance_include_bare_desc),
                checked = includeBare,
                onChange = { includeBare = it },
            )
            PodGroup(stringResource(R.string.maintenance_pods_bare, groups.bare.size), groups.bare, "")
        }
        if (groups.leftAlone.isNotEmpty()) LeftAlone(groups.leftAlone)

        Checks(plan, action, ticked) { i, on -> ticked = if (on) ticked + i else ticked - i }
        if (busyWith != null) Text(busyWith, color = colors.warn, style = MaterialTheme.typography.bodySmall)
        Button(
            onClick = { onStart(MaintenanceChoice(action, includeBare, maintenanceAcknowledged(plan, action, ticked.size))) },
            enabled = maintenanceCanStart(plan, action, ticked.size, busyWith != null),
            modifier = Modifier.fillMaxWidth(),
        ) { Text(stringResource(R.string.maintenance_start)) }
    }
}

@Composable
private fun ActionPicker(selected: MaintenanceAction, onSelect: (MaintenanceAction) -> Unit) {
    Column(Modifier.selectableGroup()) {
        MaintenanceAction.entries.forEach { action ->
            Row(
                Modifier.fillMaxWidth()
                    .selectable(selected = action == selected, role = Role.RadioButton, onClick = { onSelect(action) })
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.Top,
            ) {
                RadioButton(selected = action == selected, onClick = null)
                Column(Modifier.padding(start = 8.dp)) {
                    Text(stringResource(action.label), style = MaterialTheme.typography.bodyLarge)
                    MutedText(stringResource(action.description))
                }
            }
        }
    }
}

@Composable
private fun PodGroup(title: String, pods: List<DrainPod>, empty: String) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            SectionTitle(title)
            if (pods.isEmpty() && empty.isNotEmpty()) MutedText(empty)
            pods.forEach { DrainPodRow(it) }
        }
    }
}

/** DaemonSet and static pods, collapsed: the drain does not touch them. */
@Composable
private fun LeftAlone(pods: List<DrainPod>) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(Modifier.fillMaxWidth().expandable(expanded) { expanded = !expanded }, verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    SectionTitle(stringResource(R.string.maintenance_pods_left, pods.size))
                    MutedText(stringResource(R.string.maintenance_pods_left_desc))
                }
                Icon(if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
            }
            if (expanded) pods.forEach { DrainPodRow(it) }
        }
    }
}

@Composable
private fun Checks(plan: MaintenancePlan, action: MaintenanceAction, ticked: Set<Int>, onTick: (Int, Boolean) -> Unit) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SectionTitle(stringResource(R.string.upgrade_checks))
            plan.blockers.forEach { Text("✕ $it", color = colors.bad, style = MaterialTheme.typography.bodyMedium) }
            if (plan.blockers.isNotEmpty() && action == MaintenanceAction.NONE) MutedText(stringResource(R.string.maintenance_blockers_drain_only))
            plan.warnings.forEach { Text("! $it", color = colors.warn, style = MaterialTheme.typography.bodyMedium) }
            // Acknowledgments concern the reboot or shutdown: a drain alone does not need them.
            if (plan.acknowledge.isNotEmpty() && action != MaintenanceAction.NONE) {
                Text(stringResource(R.string.maintenance_acknowledge_title), style = MaterialTheme.typography.labelMedium)
                plan.acknowledge.forEachIndexed { i, text ->
                    Row(
                        Modifier.fillMaxWidth().toggleable(value = i in ticked, role = Role.Checkbox) { onTick(i, it) },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(checked = i in ticked, onCheckedChange = null, modifier = Modifier.padding(end = 12.dp, top = 6.dp, bottom = 6.dp))
                        Text(text, color = colors.warn, style = MaterialTheme.typography.bodyMedium)
                    }
                }
            }
            if (plan.blockers.isEmpty() && plan.warnings.isEmpty() && plan.acknowledge.isEmpty()) {
                Text(stringResource(R.string.upgrade_no_issues), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}
