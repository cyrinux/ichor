package name.levis.ichor.ui.search

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ScreenMatch
import name.levis.ichor.ui.components.SectionTitle

/** Rows of the Screens group before "Show all". */
private const val SCREENS_SHOWN = 5

/**
 * The Screens group of the search (U3 step 5): the active cluster's screens that match, above
 * the clusters' objects; absent with no match. A row opens its screen ([onOpen] with its route).
 */
internal fun LazyListScope.screensGroup(
    matches: List<ScreenMatch>,
    specs: Map<String, ScreenSpec>,
    expanded: Boolean,
    onExpand: () -> Unit,
    onOpen: (String) -> Unit,
) {
    val rows = matches.filter { it.id in specs }
    if (rows.isEmpty()) return
    item(key = "screens") {
        SectionTitle(stringResource(R.string.search_group_screens), Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp))
    }
    items(if (expanded) rows else rows.take(SCREENS_SHOWN), key = { "screen-${it.id}" }) { match ->
        val spec = specs.getValue(match.id)
        ListItem(
            headlineContent = { Text(match.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            supportingContent = { Text(match.subtitle.ifEmpty { match.section }, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            leadingContent = { Icon(spec.icon, contentDescription = null) },
            modifier = Modifier.clickable { onOpen(spec.route()) },
        )
    }
    if (!expanded && rows.size > SCREENS_SHOWN) {
        item(key = "screens-all") {
            ListItem(
                headlineContent = { Text(stringResource(R.string.search_show_all)) },
                leadingContent = { Icon(Icons.Outlined.ExpandMore, contentDescription = null) },
                modifier = Modifier.clickable(onClick = onExpand),
            )
        }
    }
}
