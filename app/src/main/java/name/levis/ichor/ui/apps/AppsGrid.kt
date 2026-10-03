package name.levis.ichor.ui.apps

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridScope
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AppFilter
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.attention
import name.levis.ichor.model.attentionCount
import name.levis.ichor.model.categoryCounts
import name.levis.ichor.model.driftVersions
import name.levis.ichor.model.filtered
import name.levis.ichor.model.groups
import name.levis.ichor.ui.theme.LocalStatusColors

/** Rounded like the mockup's cards. */
internal val AppCardShape = RoundedCornerShape(18.dp)

/**
 * Search, filter chips, the grid of recognised apps, then the Kubernetes/Talos plumbing and
 * the unrecognised apps in sections collapsed by default (opened while searching or filtering).
 */
@Composable
fun AppsGrid(inventory: Inventory, onOpen: (InventoryApp) -> Unit) {
    var query by rememberSaveable { mutableStateOf("") }
    var filterKey by rememberSaveable { mutableStateOf(FILTER_ALL) }
    var systemOpen by rememberSaveable { mutableStateOf(false) }
    var unknownOpen by rememberSaveable { mutableStateOf(false) }
    val apps = inventory.apps
    val categories = remember(apps) { apps.categoryCounts() }
    // A filter whose chip went away after a refresh falls back to all apps.
    val filter = filterOf(filterKey).takeIf { f ->
        when (f) {
            AppFilter.All -> true
            AppFilter.Attention -> apps.attentionCount > 0
            is AppFilter.Category -> categories.any { it.first == f.id }
        }
    } ?: AppFilter.All
    val groups = remember(apps, query, filter) { apps.filtered(query, filter).groups() }
    val narrowed = query.isNotBlank() || filter != AppFilter.All
    val systemTitle = stringResource(R.string.apps_section_system)
    val unknownTitle = stringResource(R.string.apps_section_unknown)

    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 104.dp),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        fullWidth("search") {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                placeholder = { Text(stringResource(R.string.apps_search)) },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                singleLine = true,
                shape = AppCardShape,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        fullWidth("filters") {
            FilterRow(apps, categories, filter, onSelect = { filterKey = keyOf(it) })
        }
        if (apps.isEmpty()) {
            fullWidth("empty") { Message(stringResource(R.string.apps_empty)) }
            return@LazyVerticalGrid
        }
        if (groups.main.isEmpty() && groups.system.isEmpty() && groups.unknown.isEmpty()) {
            fullWidth("nomatch") {
                Message(if (query.isBlank()) stringResource(R.string.apps_no_filter_match) else stringResource(R.string.apps_no_match, query.trim()))
            }
            return@LazyVerticalGrid
        }
        items(groups.main, key = { it.id }) { app -> AppTile(app) { onOpen(app) } }
        section("system", systemTitle, groups.system, systemOpen || narrowed, { systemOpen = !systemOpen }, onOpen)
        section("unknown", unknownTitle, groups.unknown, unknownOpen || narrowed, { unknownOpen = !unknownOpen }, onOpen)
    }
}

private fun LazyGridScope.fullWidth(key: String, content: @Composable () -> Unit) {
    item(key = key, span = { GridItemSpan(maxLineSpan) }) { content() }
}

private fun LazyGridScope.section(
    key: String,
    title: String,
    apps: List<InventoryApp>,
    open: Boolean,
    onToggle: () -> Unit,
    onOpen: (InventoryApp) -> Unit,
) {
    if (apps.isEmpty()) return
    fullWidth("section-$key") { SectionHeader(title, apps, open, onToggle) }
    if (open) items(apps, key = { "$key-${it.id}" }) { app -> AppTile(app) { onOpen(app) } }
}

@Composable
private fun Message(text: String) {
    Text(
        text,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        style = MaterialTheme.typography.bodyMedium,
        modifier = Modifier.padding(vertical = 24.dp),
    )
}

@Composable
private fun FilterRow(apps: List<InventoryApp>, categories: List<Pair<String, Int>>, filter: AppFilter, onSelect: (AppFilter) -> Unit) {
    val warn = LocalStatusColors.current.warn
    val attention = apps.attentionCount
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        CountChip(stringResource(R.string.apps_filter_all), apps.size, filter == AppFilter.All) { onSelect(AppFilter.All) }
        if (attention > 0) {
            CountChip(
                stringResource(R.string.apps_filter_attention),
                attention,
                filter == AppFilter.Attention,
                leading = { Box(Modifier.size(8.dp).background(warn, CircleShape)) },
            ) { onSelect(if (filter == AppFilter.Attention) AppFilter.All else AppFilter.Attention) }
        }
        categories.forEach { (id, count) ->
            val selected = filter == AppFilter.Category(id)
            CountChip(categoryLabel(id), count, selected) { onSelect(if (selected) AppFilter.All else AppFilter.Category(id)) }
        }
    }
}

@Composable
private fun CountChip(label: String, count: Int, selected: Boolean, leading: (@Composable () -> Unit)? = null, onClick: () -> Unit) {
    FilterChip(
        selected = selected,
        onClick = onClick,
        leadingIcon = leading,
        label = {
            Text(label)
            Text(
                count.toString(),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 6.dp),
            )
        },
    )
}

/** Icon, name and version; an amber dot when the app needs a look. */
@Composable
private fun AppTile(app: InventoryApp, onClick: () -> Unit) {
    Surface(onClick = onClick, shape = AppCardShape, color = MaterialTheme.colorScheme.surfaceContainer) {
        Box {
            Column(
                Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 14.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                AppIconTile(app)
                Spacer(Modifier.height(8.dp))
                Text(
                    app.name,
                    style = MaterialTheme.typography.labelLarge,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    textAlign = TextAlign.Center,
                )
                VersionLine(app)
            }
            if (app.attention) {
                Box(
                    Modifier.align(Alignment.TopEnd).padding(10.dp).size(8.dp)
                        .background(LocalStatusColors.current.warn, CircleShape),
                )
            }
        }
    }
}

/** The version, or what is wrong with it in amber: several running, or not pinned. */
@Composable
private fun VersionLine(app: InventoryApp) {
    val warn = LocalStatusColors.current.warn
    val (text, color) = when {
        app.drift -> pluralStringResource(R.plurals.apps_versions, app.driftVersions, app.driftVersions) to warn
        app.unpinned -> ":latest" to warn
        else -> app.version.ifEmpty { "—" } to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        fontFamily = if (app.drift) null else FontFamily.Monospace,
        color = color,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

/** "Kubernetes & Talos · 9" with a few of its icons, toggling the section open. */
@Composable
private fun SectionHeader(title: String, apps: List<InventoryApp>, open: Boolean, onToggle: () -> Unit) {
    Surface(onClick = onToggle, shape = AppCardShape, color = MaterialTheme.colorScheme.surfaceContainerLow, modifier = Modifier.padding(top = 6.dp)) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("$title · ${apps.size}", style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                apps.take(3).forEach { AppIconTile(it, size = 24.dp) }
            }
            Icon(
                if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                contentDescription = null,
                modifier = Modifier.padding(start = 8.dp),
            )
        }
    }
}

private const val FILTER_ALL = "all"
private const val FILTER_ATTENTION = "attention"
private const val FILTER_CATEGORY = "category:"

/** Filters are saved as text across configuration changes. */
private fun keyOf(filter: AppFilter): String = when (filter) {
    AppFilter.All -> FILTER_ALL
    AppFilter.Attention -> FILTER_ATTENTION
    is AppFilter.Category -> FILTER_CATEGORY + filter.id
}

private fun filterOf(key: String): AppFilter = when {
    key == FILTER_ATTENTION -> AppFilter.Attention
    key.startsWith(FILTER_CATEGORY) -> AppFilter.Category(key.removePrefix(FILTER_CATEGORY))
    else -> AppFilter.All
}
