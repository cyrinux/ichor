package name.levis.ichor.ui.apps

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ImageReport
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.ReportAppRef
import name.levis.ichor.model.ReportRegistry
import name.levis.ichor.model.ReportUnused
import name.levis.ichor.model.unusedBytes
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.SpinnerBox
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** The images report of the inventory the Apps screen holds; [load] it when the sheet opens. */
class ImageReportViewModel(private val talos: TalosRepository) : LoadingViewModel<ImageReport>() {
    override val loadsOnOpen = false
    private var inventory: Inventory? = null

    override suspend fun fetch(): ImageReport = talos.imageReport(checkNotNull(inventory))

    /** Reads the report of [inventory] again only when it changed (a refresh of the grid). */
    fun load(inventory: Inventory) {
        if (inventory == this.inventory) return
        this.inventory = inventory
        refresh(reset = true)
    }
}

/**
 * The images report as a sheet over the Apps screen: unpinned images, repositories on several
 * tags, registries, and the images a node keeps that none of its containers runs (Talos only:
 * [talos]). An app row opens its sheet ([onApp] with its id), an unused image its node's Images
 * screen ([onNodeImages] with the node address, null without one); [nodes] names the addresses.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ImageReportSheet(
    state: UiState<ImageReport>,
    talos: Boolean,
    nodes: Map<String, NodeOverview>,
    onApp: (String) -> Unit,
    onNodeImages: ((String) -> Unit)?,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text(
            stringResource(R.string.image_report_title),
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.padding(horizontal = 20.dp),
        )
        when (state) {
            UiState.Loading -> SpinnerBox(Modifier.fillMaxWidth().padding(32.dp))
            is UiState.Failed -> ErrorBox(state.message, onRetry, Modifier.fillMaxWidth())
            is UiState.Loaded -> ReportList(state.data, talos, { nodes[it]?.hostname ?: it }, onApp, onNodeImages)
        }
    }
}

@Composable
private fun ReportList(
    report: ImageReport,
    talos: Boolean,
    host: (String) -> String,
    onApp: (String) -> Unit,
    onNodeImages: ((String) -> Unit)?,
) {
    LazyColumn(
        contentPadding = PaddingValues(start = 20.dp, end = 20.dp, top = 8.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        item { Summary(report, talos) }
        section(R.string.image_report_unpinned, R.string.image_report_unpinned_hint, report.unpinned.isEmpty()) {
            items(report.unpinned, key = { "u|${it.image}" }) { row ->
                ReportRow(row.image, row.apps, row.nodes.map(host), onApp)
            }
        }
        section(R.string.image_report_multitag, R.string.image_report_multitag_hint, report.multiTag.isEmpty()) {
            report.multiTag.forEach { repo ->
                item(key = "m|${repo.repository}") {
                    Text(repo.repository, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                }
                items(repo.tags, key = { "m|${repo.repository}|${it.tag}" }) { tag ->
                    ReportRow(tag.tag, tag.apps, tag.nodes.map(host), onApp, Modifier.padding(start = 12.dp))
                }
            }
        }
        item { SectionTitle(stringResource(R.string.image_report_registries)) }
        if (report.summary.dockerHub > 0) {
            item { MutedText(stringResource(R.string.image_report_registries_hint, report.summary.dockerHub.toString())) }
        }
        items(report.registries, key = { "r|${it.host}" }) { RegistryRow(it) }
        unusedSection(report, talos, host, onNodeImages)
    }
}

/** A titled part with its [hint], [rows], or "nothing" when [empty]. */
private fun LazyListScope.section(title: Int, hint: Int, empty: Boolean, rows: LazyListScope.() -> Unit) {
    item(key = "t|$title") {
        Column {
            SectionTitle(stringResource(title))
            MutedText(stringResource(hint))
        }
    }
    if (empty) item(key = "e|$title") { MutedText(stringResource(R.string.image_report_nothing)) } else rows()
}

private fun LazyListScope.unusedSection(
    report: ImageReport,
    talos: Boolean,
    host: (String) -> String,
    onNodeImages: ((String) -> Unit)?,
) {
    if (!talos) {
        item { SectionTitle(stringResource(R.string.image_report_unused)) }
        item { MutedText(stringResource(R.string.image_report_unused_talos_only)) }
        return
    }
    val hint = R.string.image_report_unused_hint
    item(key = "t|unused") {
        Column {
            SectionTitle(stringResource(R.string.image_report_unused))
            MutedText(stringResource(hint, formatBytes(report.unusedBytes)))
        }
    }
    if (report.unusedOnNode.isEmpty()) {
        item { MutedText(stringResource(R.string.image_report_nothing)) }
    }
    items(report.unusedOnNode, key = { "n|${it.node}|${it.image}" }) { UnusedRow(it, host(it.node), onNodeImages) }
}

/** Unpinned, Several tags, Docker Hub and Unused counts side by side. */
@Composable
private fun Summary(report: ImageReport, talos: Boolean) {
    val s = report.summary
    Row(Modifier.padding(vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        CountTile(s.unpinned, stringResource(R.string.image_report_unpinned), Modifier.weight(1f))
        CountTile(s.multiTag, stringResource(R.string.image_report_multitag), Modifier.weight(1f))
        CountTile(s.dockerHub, stringResource(R.string.image_report_docker_hub), Modifier.weight(1f))
        if (talos) CountTile(s.unused, stringResource(R.string.image_report_unused), Modifier.weight(1f))
    }
}

@Composable
private fun CountTile(count: Int, label: String, modifier: Modifier) {
    Surface(modifier, shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Column(Modifier.padding(horizontal = 10.dp, vertical = 10.dp)) {
            Text(count.toString(), style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** An image or tag, then "app, app · node, node"; tapping it opens the first app's sheet. */
@Composable
private fun ReportRow(label: String, apps: List<ReportAppRef>, hosts: List<String>, onApp: (String) -> Unit, modifier: Modifier = Modifier) {
    val first = apps.firstOrNull()
    val clickable = if (first != null) Modifier.clickable { onApp(first.id) } else Modifier
    Column(modifier.fillMaxWidth().then(clickable).padding(vertical = 4.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
        MutedText(
            listOf(apps.joinToString(", ") { it.name }, hosts.joinToString(", ")).filter { it.isNotEmpty() }.joinToString(" · "),
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun RegistryRow(registry: ReportRegistry) {
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(registry.host, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(
                listOf(
                    stringResource(R.string.image_report_images, registry.images.toString()),
                    stringResource(R.string.image_report_apps, registry.apps.toString()),
                ).joinToString(" · "),
            )
        }
        if (registry.dockerHub) StatusPill(stringResource(R.string.image_report_docker_hub), LocalStatusColors.current.warn)
    }
}

@Composable
private fun UnusedRow(row: ReportUnused, host: String, onNodeImages: ((String) -> Unit)?) {
    val clickable = if (onNodeImages != null) Modifier.clickable { onNodeImages(row.node) } else Modifier
    Row(Modifier.fillMaxWidth().then(clickable).padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(row.image, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
            MutedText(host, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (row.sizeBytes > 0) MutedText(formatBytes(row.sizeBytes), Modifier.padding(start = 12.dp))
    }
}
