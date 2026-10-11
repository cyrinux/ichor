package name.levis.ichor.ui.storage

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.LockOpen
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.BarSlice
import name.levis.ichor.model.DiskLayout
import name.levis.ichor.model.DiskLayoutList
import name.levis.ichor.model.DiskSegment
import name.levis.ichor.model.EncryptionState
import name.levis.ichor.model.Partition
import name.levis.ichor.model.barFractions
import name.levis.ichor.model.encryptionState
import name.levis.ichor.model.isFree
import name.levis.ichor.model.knownLabel
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** Slices narrower than this share of the bar show no text inside. */
private const val LABEL_MIN_FRACTION = 0.18f

/** The usual Talos partitions keep their colour on every disk (same palette on iOS). */
private val WELL_KNOWN_COLORS = mapOf(
    "EFI" to Color(0xFFE0A100),
    "BIOS" to Color(0xFF8D6E63),
    "META" to Color(0xFF2E9E4F),
    "STATE" to Color(0xFF9C4DE0),
    "EPHEMERAL" to Color(0xFF3B82F6),
)

private val OTHER_COLORS = listOf(
    Color(0xFF14A3A3),
    Color(0xFFD9468F),
    Color(0xFFE36B2C),
    Color(0xFF5C6BC0),
    Color(0xFF7CB342),
    Color(0xFF0097A7),
)

private fun partitionColor(label: String?, index: Int): Color =
    label?.let { WELL_KNOWN_COLORS[it.uppercase()] } ?: OTHER_COLORS[Math.floorMod(index, OTHER_COLORS.size)]

/** Each disk of the node: its partition bar, legend and one row per partition. */
@Composable
internal fun DiskLayoutContent(list: DiskLayoutList) {
    when {
        !list.supported -> UnsupportedReason(list.reason)
        list.disks.isEmpty() -> InfoNotice(stringResource(R.string.storage_disks_empty))
        else -> list.disks.forEachIndexed { i, disk ->
            if (i > 0) HorizontalDivider(Modifier.padding(vertical = 4.dp))
            DiskCard(disk)
        }
    }
}

@Composable
private fun DiskCard(disk: DiskLayout) {
    val slices = remember(disk) { disk.barFractions() }
    val labels = disk.partitions.associate { it.index to segmentLabel(it) }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        DiskHeader(disk)
        if (slices.isNotEmpty()) {
            PartitionBar(slices, labels)
            Legend(disk, labels)
        }
        disk.partitions.forEach { PartitionRow(it, partitionColor(it.knownLabel(), it.index)) }
    }
}

@Composable
private fun segmentLabel(p: Partition): String = p.knownLabel() ?: stringResource(R.string.storage_disk_partition_n, p.index)

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun DiskHeader(disk: DiskLayout) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Storage, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(disk.name, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.Bold)
            MutedText(disk.devPath)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(formatBytes(disk.size), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (disk.systemDisk) StatusPill(stringResource(R.string.hardware_system_disk), MaterialTheme.colorScheme.primary)
                if (disk.transport.isNotBlank()) StatusPill(disk.transport, colors.muted)
            }
        }
    }
}

/** The disk cut into its partitions and free space, by offset, as the core computed them. */
@Composable
private fun PartitionBar(slices: List<BarSlice>, labels: Map<Int, String>) {
    val unallocated = stringResource(R.string.storage_disk_unallocated)
    val descriptions = slices.map { s ->
        stringResource(R.string.storage_disk_bar_desc, sliceLabel(s.segment, labels, unallocated), formatBytes(s.segment.size))
    }
    Row(
        Modifier
            .fillMaxWidth()
            .height(28.dp)
            .clip(RoundedCornerShape(6.dp))
            .semantics { contentDescription = descriptions.joinToString("; ") },
    ) {
        slices.forEachIndexed { i, slice ->
            val segment = slice.segment
            val free = segment.isFree
            val color = if (free) MaterialTheme.colorScheme.surfaceVariant else partitionColor(labels[segment.index] ?: segment.label, segment.index)
            Box(
                Modifier
                    .weight(slice.fraction)
                    .fillMaxHeight()
                    .background(color)
                    .then(if (free) Modifier.border(1.dp, MaterialTheme.colorScheme.outlineVariant) else Modifier)
                    .semantics { contentDescription = descriptions[i] },
                contentAlignment = Alignment.Center,
            ) {
                if (slice.fraction >= LABEL_MIN_FRACTION) {
                    Text(
                        "${sliceLabel(segment, labels, unallocated)} ${formatBytes(segment.size)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = if (free) MaterialTheme.colorScheme.onSurfaceVariant else contentColorOn(color),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(horizontal = 4.dp),
                    )
                }
            }
        }
    }
}

private fun sliceLabel(segment: DiskSegment, labels: Map<Int, String>, unallocated: String): String =
    if (segment.isFree) unallocated else labels[segment.index] ?: segment.label

private fun contentColorOn(background: Color): Color = if (background.luminance() > 0.4f) Color.Black else Color.White

/** Dot, label and size per partition, then the unallocated space. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Legend(disk: DiskLayout, labels: Map<Int, String>) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        disk.partitions.forEach { p ->
            LegendEntry(partitionColor(p.knownLabel(), p.index), labels[p.index].orEmpty(), p.size)
        }
        if (disk.unallocated > 0) {
            LegendEntry(MaterialTheme.colorScheme.outlineVariant, stringResource(R.string.storage_disk_unallocated), disk.unallocated)
        }
    }
}

@Composable
private fun LegendEntry(color: Color, label: String, size: Long) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(color))
        Text(label, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.Bold, modifier = Modifier.padding(start = 4.dp))
        Text(
            formatBytes(size),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp),
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PartitionRow(p: Partition, color: Color) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(10.dp).clip(CircleShape).background(color))
            Column(Modifier.weight(1f).padding(start = 10.dp)) {
                Text(segmentLabel(p), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold)
                Text(
                    p.devPath,
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(formatBytes(p.size), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        }
        FlowRow(
            Modifier.padding(start = 20.dp, top = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            itemVerticalAlignment = Alignment.CenterVertically,
        ) {
            if (p.filesystem.isNotBlank()) Text(p.filesystem, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            EncryptionChip(p)
        }
        if (p.uuid.isNotBlank()) {
            SelectionContainer(Modifier.padding(start = 20.dp, top = 2.dp)) {
                Text(
                    stringResource(R.string.storage_disk_uuid, p.uuid),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        val details = listOfNotNull(
            p.mountedOn.ifBlank { null },
            p.phase.takeIf { it.isNotBlank() && it != "ready" },
        )
        if (details.isNotEmpty()) MutedText(details.joinToString("  ·  "), Modifier.padding(start = 20.dp))
        if (p.error.isNotBlank()) InlineError(p.error)
    }
}

@Composable
private fun EncryptionChip(p: Partition) {
    val colors = LocalStatusColors.current
    val (icon, label, color) = when (p.encryptionState) {
        EncryptionState.ENABLED -> Triple(
            Icons.Outlined.Lock,
            listOf(stringResource(R.string.storage_disk_encryption_enabled), p.encryptionProvider).filter { it.isNotBlank() }.joinToString(" · "),
            colors.ok,
        )
        EncryptionState.DISABLED -> Triple(Icons.Outlined.LockOpen, stringResource(R.string.storage_disk_encryption_disabled), colors.warn)
        EncryptionState.UNKNOWN -> Triple(Icons.AutoMirrored.Outlined.HelpOutline, stringResource(R.string.storage_disk_encryption_unknown), colors.muted)
    }
    Row(
        Modifier
            .clip(RoundedCornerShape(50))
            .background(color.copy(alpha = 0.15f))
            .padding(horizontal = 8.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(14.dp))
        Text(label, style = MaterialTheme.typography.labelMedium, color = color, modifier = Modifier.padding(start = 4.dp))
    }
}
