package name.levis.ichor.ui.storage

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material3.AssistChip
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DISK_USAGE_SHORTCUTS
import name.levis.ichor.model.DiskUsage
import name.levis.ichor.model.DiskUsageRow
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.breadcrumbs
import name.levis.ichor.model.diskUsageRows
import name.levis.ichor.model.diskUsageTotal
import name.levis.ichor.model.notice
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.Section
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.text
import name.levis.ichor.util.formatBytes

/**
 * The disk usage explorer (`talosctl usage`): start paths to choose from, a breadcrumb to go
 * up, then the entries of [path] by size. Nothing is measured until a path is chosen
 * ([path] null); tapping a directory opens it.
 */
fun LazyListScope.diskUsageSection(
    path: String?,
    usage: Section<DiskUsage>,
    support: FeatureSupport,
    onOpen: (String) -> Unit,
    onCancel: () -> Unit,
) {
    item(key = "usage-header") {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SectionTitle(stringResource(R.string.storage_section_usage))
            support.notice?.let {
                InfoNotice(it.text())
                return@Column
            }
            MutedText(stringResource(R.string.storage_usage_hint))
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                DISK_USAGE_SHORTCUTS.forEach { shortcut ->
                    FilterChip(
                        selected = shortcut == path,
                        onClick = { onOpen(shortcut) },
                        label = { Text(shortcut, fontFamily = FontFamily.Monospace) },
                    )
                }
            }
            if (path != null) Breadcrumb(path, onOpen)
        }
    }
    if (!support.supported || path == null) return
    when (usage) {
        Section.Loading -> item(key = "usage-loading") {
            Column {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        stringResource(R.string.storage_usage_measuring, path),
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = onCancel) { Text(stringResource(R.string.common_cancel)) }
                }
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        }
        // As the core says it, e.g. "/var is too large to measure within 3m0s: open one of its sub-directories instead".
        is Section.Failed -> item(key = "usage-error") { InlineError(usage.message) }
        is Section.Ok -> {
            val rows = diskUsageRows(usage.value.entries, path)
            item(key = "usage-total") {
                MutedText(stringResource(R.string.storage_usage_total, formatBytes(diskUsageTotal(usage.value.entries, path))))
            }
            if (usage.value.truncated) {
                item(key = "usage-truncated") { InfoNotice(stringResource(R.string.resources_truncated, rows.size)) }
            }
            if (rows.isEmpty()) item(key = "usage-empty") { InfoNotice(stringResource(R.string.storage_usage_empty)) }
            items(rows, key = { "usage|${it.path}" }) { row -> UsageRow(row, onOpen) }
        }
    }
}

@Composable
private fun Breadcrumb(path: String, onOpen: (String) -> Unit) {
    val crumbs = breadcrumbs(path)
    Row(Modifier.horizontalScroll(rememberScrollState()), verticalAlignment = Alignment.CenterVertically) {
        crumbs.forEachIndexed { i, crumb ->
            if (i > 0) Icon(Icons.Outlined.ChevronRight, contentDescription = null, modifier = Modifier.size(16.dp))
            AssistChip(
                onClick = { onOpen(crumb.path) },
                enabled = i < crumbs.lastIndex, // the last one is where we are
                label = { Text(crumb.label, fontFamily = FontFamily.Monospace) },
            )
        }
    }
}

@Composable
private fun UsageRow(row: DiskUsageRow, onOpen: (String) -> Unit) {
    val modifier = if (row.isDir) Modifier.clickable(role = Role.Button, onClickLabel = stringResource(R.string.common_open)) { onOpen(row.path) } else Modifier
    Column(modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
                if (row.isDir) Icons.Outlined.Folder else Icons.AutoMirrored.Outlined.InsertDriveFile,
                contentDescription = stringResource(if (row.isDir) R.string.storage_usage_directory else R.string.storage_usage_file),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
            Text(
                row.name,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).padding(horizontal = 8.dp),
            )
            Text(formatBytes(row.size), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
        }
        row.error?.takeIf { it.isNotBlank() }?.let { InlineError(it) }
        LinearProgressIndicator(
            progress = { row.fraction },
            trackColor = MaterialTheme.colorScheme.surfaceVariant,
            modifier = Modifier.fillMaxWidth().padding(top = 4.dp).height(4.dp),
            drawStopIndicator = {},
        )
    }
}
