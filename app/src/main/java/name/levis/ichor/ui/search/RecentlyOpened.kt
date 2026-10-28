package name.levis.ichor.ui.search

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.RecentTarget
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.ui.components.SectionTitle

/**
 * The search's "Recently opened" (blank query): the objects opened lately, newest first, with
 * their cluster when it is not the one on screen ([clusterOf] gives null then). A tap opens
 * one again like a share link; a long press forgets it.
 */
@OptIn(ExperimentalFoundationApi::class)
internal fun LazyListScope.recentlyOpened(
    opened: List<RecentTarget>,
    clusterOf: (String) -> String?,
    onOpen: (ShareTarget) -> Unit,
    onForget: (ShareTarget) -> Unit,
) {
    if (opened.isEmpty()) return
    item(key = "recent-targets") {
        SectionTitle(stringResource(R.string.search_recent_targets), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
    }
    items(opened, key = { "opened-${it.key}" }) { recent ->
        val kind = kindLabel(recent.target.target)
        val detail = listOfNotNull(kind, recent.detail.ifEmpty { null }, clusterOf(recent.target.cluster)).joinToString(" · ")
        ListItem(
            headlineContent = { Text(recent.label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            supportingContent = { Text(detail, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            leadingContent = { Icon(kindIcon(recent.target.target), contentDescription = null) },
            modifier = Modifier.combinedClickable(
                onClick = { onOpen(recent.target) },
                onLongClickLabel = stringResource(R.string.search_recent_targets_remove),
                onLongClick = { onForget(recent.target) },
            ),
        )
    }
}
