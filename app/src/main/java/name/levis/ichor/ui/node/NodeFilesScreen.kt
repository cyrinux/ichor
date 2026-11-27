package name.levis.ichor.ui.node

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.TravelExplore
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.ui.components.predictiveBackPreview
import name.levis.ichor.ui.components.rememberPredictiveBack
import java.time.OffsetDateTime
import name.levis.ichor.R
import name.levis.ichor.model.NodeDirListing
import name.levis.ichor.model.NodeFileEntry
import name.levis.ichor.model.NodePaths
import name.levis.ichor.ui.UiState
import name.levis.ichor.TalosApp
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorListItem
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.timeAgo

/** The default directory: where the pod and service logs are. */
private const val START_DIR = "/var/log"

/**
 * A read-only file browser on the node, like `talosctl ls` and `talosctl read`: starting points,
 * a path bar, one directory at a time; a file opens in [NodeFileSheet]. Back retraces the
 * directories visited before leaving the screen.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeFilesScreen(node: String, hostname: String, onBack: () -> Unit) {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    var history by rememberSaveable { mutableStateOf(listOf(START_DIR)) }
    // Directories reached through a link, which may be a file: their empty state offers a read.
    var viaLink by rememberSaveable { mutableStateOf(setOf<String>()) }
    var attempt by rememberSaveable { mutableIntStateOf(0) }
    var opened by rememberSaveable { mutableStateOf<String?>(null) }
    var goTo by rememberSaveable { mutableStateOf(false) }
    val dir = history.last()
    val state by produceState<UiState<NodeDirListing>>(UiState.Loading, node, dir, attempt) {
        value = UiState.Loading
        value = uiStateOf { talos.nodeFiles(node, dir) }
    }
    val open = { path: String, link: Boolean ->
        if (path != dir) history = history + path
        if (link) viaLink = viaLink + path
    }

    val back = rememberPredictiveBack(enabled = history.size > 1) { history = history.dropLast(1) }
    opened?.let { path -> NodeFileSheet(node, path, onDismiss = { opened = null }) }
    if (goTo) {
        GoToPathDialog(
            initial = dir,
            onList = { goTo = false; open(it, false) },
            onRead = { goTo = false; opened = it },
            onDismiss = { goTo = false },
        )
    }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.node_files_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.TravelExplore, stringResource(R.string.node_files_go_to), onClick = { goTo = true })
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            RootChips(dir, (state as? UiState.Loaded)?.data?.roots.orEmpty()) { open(it, false) }
            PathBar(dir) { open(it, false) }
            Loaded(state, refresh = { attempt++ }, modifier = Modifier.weight(1f).predictiveBackPreview(back)) { listing ->
                DirectoryList(
                    listing,
                    viaLink = dir in viaLink,
                    onDir = { open(it.path, it.isLink) },
                    onFile = { opened = it },
                )
            }
        }
    }
}

@Composable
private fun RootChips(dir: String, roots: List<String>, onRoot: (String) -> Unit) {
    if (roots.isEmpty()) return
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        roots.forEach { root ->
            IchorFilterChip(
                selected = dir == root || dir.startsWith("$root/"),
                onClick = { onRoot(root) },
                label = { Text(root, fontFamily = FontFamily.Monospace) },
            )
        }
    }
}

/** The path as tappable steps; the last one is where the list is. */
@Composable
private fun PathBar(dir: String, onCrumb: (String) -> Unit) {
    val crumbs = NodePaths.crumbs(dir)
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        crumbs.forEachIndexed { i, crumb ->
            if (i > 1) Text("/", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            val last = i == crumbs.lastIndex
            IchorTextButton(onClick = { onCrumb(crumb.path) }, enabled = !last) {
                Text(crumb.name, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

@Composable
private fun DirectoryList(listing: NodeDirListing, viaLink: Boolean, onDir: (NodeFileEntry) -> Unit, onFile: (String) -> Unit) {
    if (listing.entries.isEmpty()) {
        Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            EmptyText(stringResource(R.string.node_files_empty))
            if (viaLink) {
                IchorTextButton(onClick = { onFile(listing.dir) }) { Text(stringResource(R.string.node_files_open_as_file)) }
            }
        }
        return
    }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        if (listing.truncated) {
            item(key = "truncated") {
                MutedText(
                    stringResource(R.string.node_files_truncated, listing.entries.size.toString()),
                    Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                )
            }
        }
        items(listing.entries, key = { it.path }) { entry ->
            EntryRow(entry) { if (entry.isDir || entry.isLink) onDir(entry) else onFile(entry.path) }
        }
    }
}

@Composable
private fun EntryRow(entry: NodeFileEntry, onClick: () -> Unit) {
    val icon = when {
        entry.isDir -> Icons.Outlined.Folder
        entry.isLink -> Icons.Outlined.Link
        else -> Icons.AutoMirrored.Outlined.InsertDriveFile
    }
    val detail = when {
        entry.isLink -> stringResource(R.string.node_files_link_to, entry.target)
        entry.isDir -> modifiedAgo(entry.modified)
        else -> listOf(formatBytes(entry.size), modifiedAgo(entry.modified)).filter { it.isNotEmpty() }.joinToString(" · ")
    }
    IchorListItem(
        headlineContent = { Text(entry.name, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = detail.takeIf { it.isNotEmpty() }?.let { { Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        leadingContent = { Icon(icon, contentDescription = null) },
        modifier = Modifier.clickable(role = Role.Button, onClick = onClick),
    )
}

/** "2 hours ago" for an RFC 3339 time, "" when the node did not say. */
private fun modifiedAgo(modified: String): String =
    runCatching { timeAgo(OffsetDateTime.parse(modified).toInstant().toEpochMilli()) }.getOrDefault("")
