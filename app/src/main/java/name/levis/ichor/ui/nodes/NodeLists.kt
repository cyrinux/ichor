package name.levis.ichor.ui.nodes

import androidx.annotation.StringRes
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeFilter

// What the Talos and the Kubernetes nodes screens share: the status chips and the grouped list.

/** All, needing attention, or one of [filters] (the statuses the cluster's kind can have). */
@Composable
internal fun NodeFilterChips(current: NodeFilter?, filters: List<NodeFilter>, onSelect: (NodeFilter?) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = current == null, onClick = { onSelect(null) }, label = { Text(stringResource(R.string.nodes_filter_all)) })
        filters.forEach { filter ->
            FilterChip(selected = current == filter, onClick = { onSelect(filter) }, label = { Text(stringResource(filterLabel(filter))) })
        }
    }
}

@StringRes
internal fun filterLabel(filter: NodeFilter): Int = when (filter) {
    NodeFilter.ATTENTION -> R.string.nodes_filter_attention
    NodeFilter.READY -> R.string.common_status_ready
    NodeFilter.NOT_READY -> R.string.common_status_not_ready
    NodeFilter.UNREACHABLE -> R.string.common_status_unreachable
}

/**
 * The nodes of [groups] under a [header] per group (null: bare), so a scroll lands on the
 * problems, or a site, first; [row] draws one node, a divider follows each.
 */
@Composable
internal fun <G, N> GroupedNodeList(
    groups: List<G>,
    groupKey: (G) -> String,
    header: (@Composable (G) -> String)?,
    nodes: (G) -> List<N>,
    nodeKey: (N) -> String,
    row: @Composable (N) -> Unit,
) {
    LazyColumn(Modifier.fillMaxSize()) {
        groups.forEach { group ->
            if (header != null) item(key = "group:${groupKey(group)}") {
                Text(
                    header(group),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                )
            }
            items(nodes(group), key = { "node:${nodeKey(it)}" }) { node ->
                row(node)
                HorizontalDivider()
            }
        }
    }
}
