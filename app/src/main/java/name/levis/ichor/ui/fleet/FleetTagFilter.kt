package name.levis.ichor.ui.fleet

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.FleetGroup
import name.levis.ichor.model.activeTagFilter
import name.levis.ichor.model.needAttention
import name.levis.ichor.ui.design.IchorFilterChip

/**
 * One chip per tag of [groups] with its attention count, "Untagged" for the clusters without
 * any: several can be on, a cluster shows when it is in any of them.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun FleetTagFilter(groups: List<FleetGroup>, selected: Set<String>, onSelected: (Set<String>) -> Unit) {
    val active = activeTagFilter(groups, selected)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        groups.forEach { group ->
            val on = group.tag in active
            IchorFilterChip(
                selected = on,
                onClick = { onSelected(if (on) active - group.tag else active + group.tag) },
                label = { Text(chipLabel(group)) },
            )
        }
    }
}

/** "prod · Need attention: 2", or the tag alone when all is well. */
@Composable
private fun chipLabel(group: FleetGroup): String {
    val tag = group.tag.ifEmpty { stringResource(R.string.fleet_filter_untagged) }
    val attention = group.counts.needAttention
    return if (attention > 0) "$tag · ${stringResource(R.string.fleet_summary_attention, attention)}" else tag
}
