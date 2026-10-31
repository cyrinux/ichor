package name.levis.ichor.ui.node

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeLabelDiff
import name.levis.ichor.model.NodeMetaPlan
import name.levis.ichor.model.NodeTaintDiff
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The plan of a taints and labels edit of [hostname]: what refuses it, what to know first, the
 * pods a NoExecute taint evicts at once (above Apply), then the diff. Apply stays off while
 * something refuses it; [onReload] reads the node again when it changed since.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun NodeMetaPlanSheet(
    plan: NodeMetaPlan,
    hostname: String,
    applying: Boolean,
    onApply: () -> Unit,
    onReload: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .padding(horizontal = 16.dp)
                .padding(bottom = 16.dp)
                .navigationBarsPadding()
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(stringResource(R.string.node_meta_plan_title, hostname), style = MaterialTheme.typography.titleLarge)
            if (plan.blockers.isNotEmpty()) {
                Text(stringResource(R.string.node_meta_blocked), style = MaterialTheme.typography.titleSmall, color = colors.bad)
                plan.blockers.forEach { Text("• $it", style = MaterialTheme.typography.bodyMedium, color = colors.bad) }
            }
            plan.warnings.forEach { Text("• $it", style = MaterialTheme.typography.bodyMedium, color = colors.warn) }
            if (plan.evicts.isNotEmpty()) {
                Text(stringResource(R.string.node_meta_evicts), style = MaterialTheme.typography.titleSmall, color = colors.warn)
                plan.evicts.forEach { pod ->
                    val owner = if (pod.owner.isEmpty()) "" else " (${pod.owner})"
                    MonoLine("${pod.namespace}/${pod.name}$owner")
                }
            }
            if (plan.noChange) {
                MutedText(stringResource(R.string.node_meta_no_change))
            } else {
                TaintDiff(plan.taints)
                LabelDiff(plan.labels)
            }
            Row(
                Modifier.fillMaxWidth().padding(top = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IchorTextButton(onClick = onDismiss, enabled = !applying) { Text(stringResource(R.string.common_cancel)) }
                if (plan.stale || plan.blockers.isNotEmpty()) {
                    IchorTextButton(onClick = onReload, enabled = !applying) { Text(stringResource(R.string.node_meta_reload)) }
                }
                IchorButton(onClick = onApply, enabled = plan.canApply && !applying) {
                    if (applying) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                    else Text(stringResource(R.string.node_meta_apply))
                }
            }
        }
    }
}

@Composable
private fun TaintDiff(diff: NodeTaintDiff) {
    if (diff.isEmpty) return
    val colors = LocalStatusColors.current
    SectionTitle(stringResource(R.string.node_meta_taints))
    DiffGroup(R.string.node_meta_added, diff.added.map { "+ ${it.display}" }, colors.ok)
    DiffGroup(R.string.node_meta_removed, diff.removed.map { "− ${it.display}" }, colors.bad)
    DiffGroup(
        R.string.node_meta_changed,
        diff.changed.map { change ->
            val effect = if (change.effect.isEmpty()) "" else ":${change.effect}"
            "~ ${change.key}$effect: ${change.fromText} → ${change.toText}"
        },
        colors.warn,
    )
}

@Composable
private fun LabelDiff(diff: NodeLabelDiff) {
    if (diff.isEmpty) return
    val colors = LocalStatusColors.current
    SectionTitle(stringResource(R.string.node_meta_labels))
    DiffGroup(R.string.node_meta_added, diff.added.map { "+ ${it.key}=${it.value}" }, colors.ok)
    DiffGroup(R.string.node_meta_removed, diff.removed.map { "− ${it.key}=${it.value}" }, colors.bad)
    DiffGroup(R.string.node_meta_changed, diff.changed.map { "~ ${it.key}: ${it.from} → ${it.to}" }, colors.warn)
}

@Composable
private fun DiffGroup(@StringRes title: Int, lines: List<String>, color: Color) {
    if (lines.isEmpty()) return
    Text(stringResource(title), style = MaterialTheme.typography.labelLarge)
    lines.forEach { MonoLine(it, color) }
}

@Composable
private fun MonoLine(text: String, color: Color = Color.Unspecified) {
    Text(text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = color)
}

/** The plan's changes as plain lines, for the labels-only confirmation. */
internal fun NodeMetaPlan.summaryLines(): String = buildList {
    labels.added.forEach { add("+ ${it.key}=${it.value}") }
    labels.removed.forEach { add("− ${it.key}=${it.value}") }
    labels.changed.forEach { add("~ ${it.key}: ${it.from} → ${it.to}") }
}.joinToString("\n")
