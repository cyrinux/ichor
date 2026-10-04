package name.levis.ichor.ui.argocd

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ArgoHealth
import name.levis.ichor.model.ArgoResource
import name.levis.ichor.model.ArgoSync
import name.levis.ichor.model.WaveState
import name.levis.ichor.model.WaveStep
import name.levis.ichor.ui.theme.LocalStatusColors

private val NODE = 26.dp

/**
 * The sync waves as a vertical stepper, lowest wave first: done waves green with a check, the
 * current one pulsing while a sync runs, a failed one red. Each step lists its resources (kind,
 * name, sync and health dots, hooks and prune candidates marked). With [selecting], each row
 * gets a box for selective sync; workloads offer a rollout restart ([onRestart]).
 */
@Composable
fun ArgoWaveTimeline(
    steps: List<WaveStep>,
    selecting: Boolean,
    selected: Set<String>,
    onToggle: (ArgoResource) -> Unit,
    onRestart: ((ArgoResource) -> Unit)?,
) {
    Column {
        steps.forEachIndexed { i, step ->
            StepRow(step, last = i == steps.lastIndex, selecting, selected, onToggle, onRestart)
        }
    }
}

@Composable
private fun StepRow(
    step: WaveStep,
    last: Boolean,
    selecting: Boolean,
    selected: Set<String>,
    onToggle: (ArgoResource) -> Unit,
    onRestart: ((ArgoResource) -> Unit)?,
) {
    val color = step.state.color()
    Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
        Column(Modifier.width(NODE).fillMaxHeight(), horizontalAlignment = Alignment.CenterHorizontally) {
            StepNode(step, color)
            if (!last) {
                Box(
                    Modifier.padding(vertical = 2.dp).width(2.dp).weight(1f)
                        .background(if (step.state == WaveState.DONE) color else MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(1.dp)),
                )
            }
        }
        Column(Modifier.weight(1f).padding(start = 12.dp, bottom = if (last) 0.dp else 16.dp)) {
            Row(Modifier.height(NODE), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.argo_wave_title, step.wave), style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                Text(stateLabel(step.state), style = MaterialTheme.typography.labelMedium, color = color)
            }
            step.resources.forEach { r ->
                ResourceRow(r, selecting, r.key in selected, { onToggle(r) }, onRestart?.takeIf { r.restartable }?.let { { it(r) } })
            }
        }
    }
}

@Composable
private fun StepNode(step: WaveStep, color: Color) {
    // Only the current wave pulses: no animation runs for the others.
    val modifier = Modifier.size(NODE).alpha(if (step.state == WaveState.CURRENT) pulsingAlpha() else 1f)
    when (step.state) {
        WaveState.PENDING -> Box(modifier.border(BorderStroke(2.dp, MaterialTheme.colorScheme.outline), CircleShape), contentAlignment = Alignment.Center) {
            Text(step.wave.toString(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        else -> Box(modifier.background(color, CircleShape), contentAlignment = Alignment.Center) {
            val icon = when (step.state) {
                WaveState.DONE -> Icons.Outlined.Check
                WaveState.FAILED -> Icons.Outlined.Close
                else -> Icons.Outlined.Sync
            }
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.surface, modifier = Modifier.size(16.dp))
        }
    }
}

@Composable
private fun pulsingAlpha(): Float {
    val pulse = rememberInfiniteTransition(label = "wave")
    val alpha by pulse.animateFloat(1f, 0.35f, infiniteRepeatable(tween(700), RepeatMode.Reverse), label = "alpha")
    return alpha
}

@Composable
private fun ResourceRow(r: ArgoResource, selecting: Boolean, checked: Boolean, onToggle: () -> Unit, onRestart: (() -> Unit)?) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier.fillMaxWidth().then(if (selecting) Modifier.clickable(onClick = onToggle) else Modifier).padding(vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (selecting) Checkbox(checked = checked, onCheckedChange = { onToggle() })
        val sync = ArgoSync.from(r.sync)
        Dot(sync.color(), sync.label())
        if (r.health.isNotEmpty()) ArgoHealth.from(r.health).let { Dot(it.color(), it.label(), Modifier.padding(start = 3.dp)) }
        Column(Modifier.weight(1f).padding(start = 8.dp)) {
            Text(r.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(r.kind, style = MaterialTheme.typography.labelSmall, color = muted)
                if (r.hook) Tag(stringResource(R.string.argo_hook), MaterialTheme.colorScheme.tertiary)
                if (r.prune) Tag(stringResource(R.string.argo_prune_tag), LocalStatusColors.current.warn)
                if (r.syncResult == "SyncFailed") Tag(r.syncResult, LocalStatusColors.current.bad)
            }
        }
        if (onRestart != null && !selecting) {
            IconButton(onClick = onRestart) {
                Icon(Icons.Outlined.RestartAlt, contentDescription = stringResource(R.string.workloads_restart_confirm), tint = muted, modifier = Modifier.size(18.dp))
            }
        }
    }
}

@Composable
private fun Dot(color: Color, description: String, modifier: Modifier = Modifier) {
    Box(modifier.size(8.dp).background(color, CircleShape).semantics { contentDescription = description })
}

@Composable
private fun Tag(text: String, color: Color) {
    Surface(shape = RoundedCornerShape(4.dp), color = color.copy(alpha = 0.14f), contentColor = color) {
        Text(text, style = MaterialTheme.typography.labelSmall, modifier = Modifier.padding(horizontal = 4.dp))
    }
}

@Composable
private fun WaveState.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        WaveState.DONE -> colors.ok
        WaveState.CURRENT -> MaterialTheme.colorScheme.primary
        WaveState.FAILED -> colors.bad
        WaveState.PENDING -> colors.muted
    }
}

@Composable
private fun stateLabel(state: WaveState): String = stringResource(
    when (state) {
        WaveState.DONE -> R.string.argo_wave_done
        WaveState.CURRENT -> R.string.argo_wave_current
        WaveState.FAILED -> R.string.argo_wave_failed
        WaveState.PENDING -> R.string.argo_wave_pending
    },
)
