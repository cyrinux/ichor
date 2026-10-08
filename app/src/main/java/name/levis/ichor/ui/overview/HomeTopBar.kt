package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.RowScope
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.ui.components.ClusterLabels

/**
 * The top bar both homes share (the Talos overview, the Kubernetes home): the cluster's title
 * and menu, a swipe sideways for the previous or next cluster, and the arranged [actions]; while
 * [customizing], the editor's title and its Done button instead.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun HomeTopBar(
    customizing: Boolean,
    editTitle: String,
    onDone: () -> Unit,
    config: StoredConfig?,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onSelect: (String) -> Unit,
    onManage: () -> Unit,
    actions: @Composable RowScope.() -> Unit,
) {
    var clusterMenu by remember { mutableStateOf(false) }
    if (customizing) {
        TopAppBar(
            title = { Text(editTitle) },
            actions = { TextButton(onClick = onDone) { Text(stringResource(R.string.overview_edit_done)) } },
        )
    } else {
        TopAppBar(
            // Swipe the bar sideways for the previous/next cluster, tap the title for the menu.
            modifier = Modifier.clusterSwipe(config, onSelect),
            title = {
                Box {
                    ClusterTitle(config, colors, labels, onOpen = { clusterMenu = true }) { ScreenshotModeIcon() }
                    config?.let { stored ->
                        ClusterMenu(
                            expanded = clusterMenu,
                            config = stored,
                            colors = colors,
                            labels = labels,
                            onSelect = onSelect,
                            onManage = onManage,
                            onDismiss = { clusterMenu = false },
                        )
                    }
                }
            },
            actions = actions,
        )
    }
}
