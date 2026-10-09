package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeObjectScale
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.MAX_SCALE_REPLICAS
import name.levis.ichor.model.confirmationMatches
import name.levis.ichor.model.scaleNeedsConfirm
import name.levis.ichor.model.scaleNeedsTypedName
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Scales [ref] like `kubectl scale`: its count read first, a stepper to the target, then Apply.
 * Scaling down says how many pods stop; scaling to 0 needs the object's name typed, as for the
 * workload screen's scale. A Job's count is its parallelism (0 pauses it). [denial] (the
 * credentials may not scale it) disables Apply and says why.
 */
@Composable
fun KubeObjectScaleDialog(
    ref: KubeObjectRef,
    scale: ObjectScale,
    denial: KubePermission?,
    onTarget: (Int) -> Unit,
    onApply: () -> Unit,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
) {
    var typed by remember { mutableStateOf("") }
    val loaded = (scale.scale as? UiState.Loaded)?.data
    val target = scale.target
    val toZero = loaded != null && scaleNeedsTypedName(target) && target != loaded.replicas
    val enabled = loaded != null && target != loaded.replicas && !scale.applying && denial == null &&
        (!toZero || confirmationMatches(typed, ref.name))

    AlertDialog(
        onDismissRequest = { if (!scale.applying) onDismiss() },
        title = { Text(stringResource(R.string.kb_scale_title, ref.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                when (val s = scale.scale) {
                    UiState.Loading -> CircularProgressIndicator(Modifier.padding(4.dp))
                    is UiState.Failed -> {
                        InlineError(s.message.asString())
                        TextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
                    }
                    is UiState.Loaded -> ScaleStepper(ref, s.data, target, scale.applying, onTarget)
                }
                if (toZero) {
                    Text(stringResource(R.string.power_type_to_confirm, ref.name), style = MaterialTheme.typography.bodyMedium)
                    OutlinedTextField(
                        value = typed,
                        onValueChange = { typed = it },
                        singleLine = true,
                        textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                scale.error?.let { InlineError(it.asString()) }
                KubeDenialNote(denial)
            }
        },
        confirmButton = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (scale.applying) CircularProgressIndicator(Modifier.padding(end = 8.dp))
                TextButton(onClick = onApply, enabled = enabled) { Text(stringResource(R.string.workloads_scale_apply)) }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !scale.applying) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun ScaleStepper(ref: KubeObjectRef, s: KubeObjectScale, target: Int, applying: Boolean, onTarget: (Int) -> Unit) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { onTarget(target - 1) }, enabled = target > 0 && !applying) {
            Icon(Icons.Outlined.Remove, stringResource(R.string.workloads_scale_less))
        }
        Text(
            stringResource(R.string.workloads_scale_change, s.replicas, target),
            style = MaterialTheme.typography.titleMedium,
            fontFamily = FontFamily.Monospace,
        )
        IconButton(onClick = { onTarget(target + 1) }, enabled = target < MAX_SCALE_REPLICAS && !applying) {
            Icon(Icons.Outlined.Add, stringResource(R.string.workloads_scale_more))
        }
    }
    MutedText(stringResource(R.string.kb_scale_running, s.current.toString()))
    if (s.isParallelism) MutedText(stringResource(R.string.kb_scale_job_hint))
    when {
        target == s.replicas -> Unit
        scaleNeedsTypedName(target) ->
            Text(stringResource(R.string.workloads_scale_zero_text, ref.namespace), color = colors.warn, style = MaterialTheme.typography.bodySmall)
        scaleNeedsConfirm(s.replicas, target) ->
            Text(stringResource(R.string.workloads_scale_down_text, s.replicas - target, ref.namespace), color = colors.warn, style = MaterialTheme.typography.bodySmall)
    }
}
