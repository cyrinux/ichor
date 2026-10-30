package name.levis.ichor.ui.autoscaling

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.HpaRow
import name.levis.ichor.model.MAX_SCALE_REPLICAS
import name.levis.ichor.model.hpaBoundsValid

/**
 * An HPA's min and max, with the count it runs now between them: Save stays off until the
 * bounds change and while max is below min (min cannot go under 1).
 */
@Composable
fun HpaBoundsDialog(row: HpaRow, onSave: (Int, Int) -> Unit, onDismiss: () -> Unit) {
    var min by rememberSaveable(row.key) { mutableIntStateOf(row.min.coerceIn(1, MAX_SCALE_REPLICAS)) }
    var max by rememberSaveable(row.key) { mutableIntStateOf(row.max.coerceIn(1, MAX_SCALE_REPLICAS)) }
    val valid = hpaBoundsValid(min, max)
    // Against what the form started from: an HPA at min 0 (HPAScaleToZero) opens at 1, unsaved.
    val changed = min != row.min.coerceIn(1, MAX_SCALE_REPLICAS) || max != row.max.coerceIn(1, MAX_SCALE_REPLICAS)

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.autoscaling_bounds_title, row.name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                ReplicaStepper(stringResource(R.string.autoscaling_bounds_min), min, 1..MAX_SCALE_REPLICAS) { min = it }
                Text(
                    stringResource(R.string.autoscaling_bounds_current, row.current.toString()),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                ReplicaStepper(stringResource(R.string.autoscaling_bounds_max), max, 1..MAX_SCALE_REPLICAS) { max = it }
                if (!valid) {
                    Text(stringResource(R.string.autoscaling_bounds_invalid), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onSave(min, max) }, enabled = valid && changed) { Text(stringResource(R.string.autoscaling_bounds_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** [label] and a count within [range], one step per tap. */
@Composable
fun ReplicaStepper(label: String, value: Int, range: IntRange, onChange: (Int) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        IchorIconButton(onClick = { onChange(value - 1) }, enabled = value > range.first) {
            Icon(Icons.Outlined.Remove, stringResource(R.string.autoscaling_stepper_less, label))
        }
        Text(value.toString(), style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
        IchorIconButton(onClick = { onChange(value + 1) }, enabled = value < range.last) {
            Icon(Icons.Outlined.Add, stringResource(R.string.autoscaling_stepper_more, label))
        }
    }
}
