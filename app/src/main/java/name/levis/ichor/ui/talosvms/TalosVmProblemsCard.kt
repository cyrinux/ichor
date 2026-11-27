package name.levis.ichor.ui.talosvms

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.TalosClusterVms
import name.levis.ichor.model.TalosNodeVmsEntry
import name.levis.ichor.model.TalosVm
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.TalosVmDetail
import name.levis.ichor.monitor.talosVmProblemTitle
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorOutlinedCard
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The VMs that should run and do not, worded like their background alert, the node's own words
 * under each; one opens that VM's sheet. Shown at the top of the list, only when there is one.
 */
@Composable
internal fun TalosVmProblemsCard(report: TalosClusterVms, onOpen: (TalosNodeVmsEntry, TalosVm) -> Unit) {
    val context = LocalContext.current
    val bad = LocalStatusColors.current.bad
    IchorOutlinedCard(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                stringResource(R.string.talos_vms_problems_title),
                style = MaterialTheme.typography.titleSmall,
                color = bad,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
            report.problems.forEach { (entry, problem) ->
                val host = entry.hostname.ifEmpty { entry.node }
                val title = talosVmProblemTitle(context, TalosVmDetail(DATA_CRITICAL, problem.kind, host, problem.vm))
                val machine = entry.vms.firstOrNull { it.name == problem.vm }
                Column(
                    Modifier
                        .fillMaxWidth()
                        .let { m -> if (machine != null) m.clickable(role = Role.Button) { onOpen(entry, machine) } else m }
                        .heightIn(min = 48.dp)
                        .padding(horizontal = 16.dp, vertical = 6.dp),
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    Text(title, style = MaterialTheme.typography.bodyMedium, color = bad)
                    if (problem.detail.isNotBlank()) MutedText(problem.detail, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}
