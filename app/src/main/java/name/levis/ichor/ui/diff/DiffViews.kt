package name.levis.ichor.ui.diff

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.DiffLine
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.ui.theme.LocalStatusColors

// What a GitOps tool would change, object by object: shared by the Flux diff and, later,
// Argo CD's. Each object is a card with its change badge; its unified diff unfolds below in
// red and green lines, scrolling sideways rather than wrapping (YAML indentation matters).

/** Green created, amber changed, red deleted and errors, grey otherwise. */
@Composable
fun DiffChange.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        DiffChange.CREATED -> colors.ok
        DiffChange.CHANGED -> colors.warn
        DiffChange.DELETED, DiffChange.ERROR -> colors.bad
        DiffChange.ENCRYPTED, DiffChange.IGNORED, DiffChange.UNCHANGED -> colors.muted
    }
}

@Composable
fun DiffChange.label(): String = stringResource(
    when (this) {
        DiffChange.CREATED -> R.string.diff_change_created
        DiffChange.CHANGED -> R.string.diff_change_changed
        DiffChange.DELETED -> R.string.diff_change_deleted
        DiffChange.ENCRYPTED -> R.string.diff_change_encrypted
        DiffChange.IGNORED -> R.string.diff_change_ignored
        DiffChange.ERROR -> R.string.diff_change_error
        DiffChange.UNCHANGED -> R.string.diff_change_unchanged
    },
)

/** The change as a small tinted label, with [count] when given ("Changed 3"). */
@Composable
fun DiffChangeBadge(change: DiffChange, count: Int? = null) {
    val color = change.color()
    Text(
        if (count == null) change.label() else "${change.label()} $count",
        style = MaterialTheme.typography.labelSmall,
        color = color,
        maxLines = 1,
        modifier = Modifier
            .background(color.copy(alpha = 0.14f), RoundedCornerShape(6.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** One badge per kind of change, with its count. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DiffCounts(counts: List<Pair<DiffChange, Int>>) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        counts.forEach { (change, n) -> DiffChangeBadge(change, n) }
    }
}

/**
 * One object: its badge, kind, name and namespace; tapping unfolds its diff (or why it was
 * refused, or why it is not compared) when it has one.
 */
@Composable
fun DiffResourceCard(resource: KubeDiffResource, expanded: Boolean, onToggle: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val detail = resource.diff.isNotEmpty() || resource.error.isNotEmpty() || resource.change.hint != null
    Column(
        Modifier
            .fillMaxWidth()
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(12.dp))
            .padding(12.dp),
    ) {
        Row(
            (if (detail) Modifier.clickable(role = Role.Button, onClick = onToggle) else Modifier).fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    DiffChangeBadge(resource.change)
                    Text(resource.kind, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Text(
                    resource.name,
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = 4.dp),
                )
                if (resource.namespace.isNotEmpty()) Text(resource.namespace, style = MaterialTheme.typography.labelSmall, color = muted)
            }
            if (detail) {
                Icon(
                    if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
                    contentDescription = stringResource(if (expanded) R.string.diff_hide else R.string.diff_show),
                    tint = muted,
                )
            }
        }
        if (expanded && detail) DiffDetail(resource)
    }
}

/** Why an object is not compared, for the changes that say nothing by themselves. */
private val DiffChange.hint: Int?
    get() = when (this) {
        DiffChange.ENCRYPTED -> R.string.diff_change_encrypted_hint
        DiffChange.IGNORED -> R.string.diff_change_ignored_hint
        else -> null
    }

@Composable
private fun DiffDetail(resource: KubeDiffResource) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.padding(top = 8.dp)) {
        resource.change.hint?.let { Text(stringResource(it), style = MaterialTheme.typography.bodySmall, color = muted) }
        if (resource.error.isNotEmpty()) {
            Text(stringResource(R.string.diff_refused, resource.error), style = MaterialTheme.typography.bodySmall, color = colors.bad)
        }
        if (resource.diff.isNotEmpty()) DiffLines(resource.lines)
        if (resource.truncated) {
            Text(stringResource(R.string.diff_truncated), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(top = 4.dp))
        }
    }
}

/** The lines of a unified diff, monospace, scrolling sideways together. */
@Composable
fun DiffLines(lines: List<DiffLine>) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainerLow, RoundedCornerShape(8.dp))
            .horizontalScroll(rememberScrollState())
            .padding(vertical = 6.dp),
    ) {
        lines.forEach { line ->
            val (sign, tint) = when (line.kind) {
                DiffLine.Kind.ADDED -> "+" to colors.ok
                DiffLine.Kind.REMOVED -> "-" to colors.bad
                DiffLine.Kind.HUNK -> "" to muted
                DiffLine.Kind.CONTEXT -> " " to Color.Unspecified
            }
            Row(
                Modifier
                    .background(if (line.kind == DiffLine.Kind.ADDED || line.kind == DiffLine.Kind.REMOVED) tint.copy(alpha = 0.12f) else Color.Transparent)
                    .padding(horizontal = 8.dp),
            ) {
                Text(sign, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall, color = tint, modifier = Modifier.width(12.dp))
                Text(line.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall, color = tint, softWrap = false)
            }
        }
    }
}

/** The folded list of the objects that would not change: names only. */
@Composable
fun UnchangedList(resources: List<KubeDiffResource>) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(start = 4.dp)) {
        resources.forEach { r ->
            Row(Modifier.padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(r.kind, style = MaterialTheme.typography.labelMedium, color = muted, modifier = Modifier.width(120.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    if (r.namespace.isEmpty()) r.name else "${r.namespace}/${r.name}",
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** A row that folds or unfolds a section: its label and a chevron. */
@Composable
fun FoldRow(label: String, expanded: Boolean, onToggle: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onToggle).padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
        Icon(
            if (expanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore,
            contentDescription = stringResource(if (expanded) R.string.diff_hide else R.string.diff_show),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(24.dp),
        )
    }
}
