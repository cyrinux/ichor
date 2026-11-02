package name.levis.ichor.ui.search

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.design.IchorAssistChip

/**
 * Nothing searched or opened yet: what typing finds, that Refresh is the only cluster call, and
 * a few of the active cluster's screens. Opening one is a navigation, not a cluster call.
 * Scrolls so a large font never clips the chips.
 */
@Composable
internal fun FirstUseState(specs: List<ScreenSpec>, canRefresh: Boolean, onOpenRoute: (String) -> Unit) {
    val context = LocalContext.current
    val shortcuts = firstUseShortcuts(specs)
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        EmptyState(Icons.Outlined.Search, stringResource(R.string.search_first_use_body)) {
            if (canRefresh) {
                Text(
                    stringResource(R.string.search_first_use_refresh),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                shortcuts.forEach { spec ->
                    IchorAssistChip(
                        onClick = { onOpenRoute(spec.route()) },
                        label = { Text(screenTitle(spec, context::getString), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        leadingIcon = { Icon(spec.icon, contentDescription = null) },
                    )
                }
            }
        }
    }
}
