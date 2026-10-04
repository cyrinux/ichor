package name.levis.ichor.ui.maintenance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DrainPod
import name.levis.ichor.model.pdbBlocks
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A pod of the plan or the run: name, owner, its budget and emptyDir badges, and during a run
 * its state icon and why it waits.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DrainPodRow(pod: DrainPod) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.Top) {
        pod.stateLabel?.let { label ->
            val state = stringResource(label)
            Box(Modifier.size(24.dp).semantics { contentDescription = state }, contentAlignment = Alignment.Center) {
                when (pod.state) {
                    DrainPod.STATE_GONE -> Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.ok)
                    DrainPod.STATE_EVICTING -> CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                    DrainPod.STATE_BLOCKED -> Icon(Icons.Outlined.Block, contentDescription = null, tint = colors.warn)
                    else -> Icon(Icons.Outlined.RadioButtonUnchecked, contentDescription = null, tint = colors.muted)
                }
            }
        }
        Column(Modifier.weight(1f).padding(start = if (pod.stateLabel != null) 12.dp else 0.dp)) {
            Text(pod.key, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            if (pod.owner.isNotEmpty()) MutedText(pod.owner)
            if (pod.pdb.isNotEmpty() || pod.emptyDir) {
                FlowRow(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    if (pod.pdb.isNotEmpty()) {
                        StatusPill(
                            stringResource(R.string.maintenance_pdb, pod.pdb, pod.pdbAllowed.coerceAtLeast(0)),
                            if (pod.pdbBlocks) colors.bad else colors.muted,
                        )
                    }
                    if (pod.emptyDir) StatusPill(stringResource(R.string.maintenance_empty_dir), colors.warn)
                }
            }
            if (pod.reason.isNotEmpty()) {
                Text(
                    pod.reason,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (pod.state == DrainPod.STATE_BLOCKED) colors.warn else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
