package name.levis.ichor.ui.imagescan

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.ImageVuln
import name.levis.ichor.model.ScannedImage
import name.levis.ichor.model.VulnFilter
import name.levis.ichor.model.VulnSeverity
import name.levis.ichor.model.byPackage
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A vulnerability report: the totals, filters (fixable only, by default; severities), then
 * each image with its findings grouped by package. A finding opens in place (description,
 * score, advisory link). [onExport] writes and shares it in a format.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImageScanReportSheet(report: ImageScanReport, onExport: (ImageScanFormat) -> Unit, onDismiss: () -> Unit) {
    var fixableOnly by rememberSaveable { mutableStateOf(true) }
    var hidden by rememberSaveable { mutableStateOf(emptyList<String>()) }
    val filter = VulnFilter(fixableOnly, VulnSeverity.entries.filterNot { it.name in hidden }.toSet())
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(
            contentPadding = PaddingValues(start = 20.dp, end = 20.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            item { Header(report, onExport) }
            item { SeverityPills(report.summary) }
            item {
                Filters(filter) {
                    fixableOnly = it.fixableOnly
                    hidden = VulnSeverity.entries.filterNot { s -> s in it.severities }.map { s -> s.name }
                }
            }
            report.images.forEachIndexed { i, image ->
                item(key = "image/$i") { ImageHeader(image) }
                if (image.error.isNotEmpty()) return@forEachIndexed
                val groups = image.byPackage(filter)
                when {
                    image.vulnerabilities.isEmpty() -> item(key = "none/$i") { MutedText(stringResource(R.string.imagescan_none_found)) }
                    groups.isEmpty() -> item(key = "hidden/$i") { MutedText(stringResource(R.string.imagescan_none_shown)) }
                }
                groups.forEach { (pkg, vulns) ->
                    item(key = "pkg/$i/$pkg") { PackageHeader(vulns.first()) }
                    items(vulns, key = { "vuln/$i/$pkg/${it.id}/${it.target}" }) { v ->
                        val key = "$i/$pkg/${v.id}/${v.target}"
                        VulnRow(v, expanded = open == key) { open = if (open == key) null else key }
                    }
                }
            }
        }
    }
}

@Composable
private fun Header(report: ImageScanReport, onExport: (ImageScanFormat) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.imagescan_report_title), style = MaterialTheme.typography.titleLarge)
            MutedText(reportSource(report))
        }
        Box {
            TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.imagescan_export), onClick = { menu = true })
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                ImageScanFormat.entries.forEach { format ->
                    DropdownMenuItem(
                        text = { Text(stringResource(formatLabel(format))) },
                        onClick = {
                            menu = false
                            onExport(format)
                        },
                    )
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Filters(filter: VulnFilter, onChange: (VulnFilter) -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(
            selected = filter.fixableOnly,
            onClick = { onChange(filter.copy(fixableOnly = !filter.fixableOnly)) },
            label = { Text(stringResource(R.string.imagescan_fixable_only)) },
        )
        VulnSeverity.entries.forEach { severity ->
            FilterChip(
                selected = severity in filter.severities,
                onClick = { onChange(filter.toggle(severity)) },
                label = { Text(stringResource(severityLabel(severity))) },
            )
        }
    }
}

/** The image, its OS and digest, its pods, then its counts or why it was not scanned. */
@Composable
private fun ImageHeader(image: ScannedImage) {
    Column(Modifier.padding(top = 14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(image.image.ifEmpty { image.ref }, style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace)
        val meta = listOf(image.os, image.digest.take(19)).filter { it.isNotEmpty() }
        if (meta.isNotEmpty()) MutedText(meta.joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (image.pods.isNotEmpty()) {
            MutedText(stringResource(R.string.imagescan_pods, image.pods.joinToString(", ")), maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (image.error.isNotEmpty()) {
            Text(coreErrorText(image.error), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
            return@Column
        }
        if (image.summary.total > 0) SeverityPills(image.summary)
        if (image.summary.os > 0 && image.os.isNotEmpty()) {
            MutedText(stringResource(R.string.imagescan_base_hint, image.summary.os.toString(), image.os))
        }
    }
}

@Composable
private fun PackageHeader(first: ImageVuln) {
    Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.Bottom) {
        Text(
            first.`package`,
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false),
        )
        MutedText(" ${first.installed}", maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
    if (first.`class` != "os-pkgs" && first.target.isNotEmpty()) MutedText(first.target, maxLines = 1, overflow = TextOverflow.Ellipsis)
}

/** Severity, ID and title, the fixed version; tapped, the description, score and link. */
@Composable
private fun VulnRow(v: ImageVuln, expanded: Boolean, onToggle: () -> Unit) {
    val uri = LocalUriHandler.current
    Column(Modifier.fillMaxWidth().clickable(onClick = onToggle).padding(start = 8.dp, top = 2.dp, bottom = 2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                stringResource(severityLabel(v.level)),
                style = MaterialTheme.typography.labelSmall,
                color = severityColor(v.level),
                modifier = Modifier.width(64.dp),
            )
            Text(v.id, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            Text(
                if (v.fixable) stringResource(R.string.imagescan_fixed_in, v.fixed) else stringResource(R.string.imagescan_no_fix),
                style = MaterialTheme.typography.labelSmall,
                color = if (v.fixable) LocalStatusColors.current.ok else LocalStatusColors.current.muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 8.dp).weight(0.6f, fill = false),
            )
        }
        if (v.title.isNotEmpty()) {
            MutedText(
                v.title,
                modifier = Modifier.padding(start = 64.dp),
                maxLines = if (expanded) Int.MAX_VALUE else 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (!expanded) return@Column
        Column(Modifier.padding(start = 64.dp, top = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (v.description.isNotEmpty()) Text(v.description, style = MaterialTheme.typography.bodySmall)
            if (v.score > 0) MutedText(stringResource(R.string.imagescan_score, "%.1f".format(v.score)) + " " + v.vector)
            if (v.url.startsWith("https://") || v.url.startsWith("http://")) {
                TextButton(onClick = { uri.openUri(v.url) }, contentPadding = PaddingValues(0.dp)) {
                    Text(stringResource(R.string.imagescan_open_advisory))
                }
            }
        }
    }
}

private fun formatLabel(format: ImageScanFormat): Int = when (format) {
    ImageScanFormat.HTML -> R.string.imagescan_format_html
    ImageScanFormat.SARIF -> R.string.imagescan_format_sarif
    ImageScanFormat.CYCLONEDX -> R.string.imagescan_format_cyclonedx
    ImageScanFormat.CSV -> R.string.imagescan_format_csv
    ImageScanFormat.JSON -> R.string.imagescan_format_json
}
