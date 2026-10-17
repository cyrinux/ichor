package name.levis.ichor.ui.insights

import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DriftChange
import name.levis.ichor.model.DriftFix
import name.levis.ichor.model.DriftRow
import name.levis.ichor.model.DriftRowKind
import name.levis.ichor.model.beforeAfter
import name.levis.ichor.model.fix
import name.levis.ichor.model.rows
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionHeader
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.theme.LocalStatusColors

/** Cards with more itemised rows than this start collapsed to their header. */
private const val EXPANDED_MAX_ROWS = 3

/**
 * One drift difference: the node's [hostname] (its IP muted at the end, or the IP alone), the
 * setting as a tag, how many items differ and against which node; then one coloured row per
 * added, removed or changed item, or one "before → after" line for a scalar setting (and for
 * an older core). Starts collapsed when it has more than three rows.
 */
@Composable
fun DriftChangeCard(change: DriftChange, hostname: String, onFix: (DriftFix) -> Unit) {
    val rows = change.rows()
    val collapsible = rows.size > EXPANDED_MAX_ROWS
    var expanded by rememberSaveable(change.node, change.key) { mutableStateOf(!collapsible) }
    val reference = stringResource(R.string.insights_reference, change.referenceName.ifEmpty { change.reference })
    val count = if (rows.isEmpty()) null else pluralStringResource(R.plurals.insights_differences, rows.size, rows.size)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SectionHeader(
                hostname,
                Modifier.fillMaxWidth().expandable(expanded, enabled = collapsible) { expanded = !expanded },
                subtitle = listOfNotNull(count, reference).joinToString(" · "),
                trailing = if (hostname != change.node) {
                    { MutedText(change.node, Modifier.padding(end = 8.dp)) }
                } else {
                    null
                },
                pill = { Tag(change.key) },
                chevron = when {
                    !collapsible -> null
                    expanded -> Icons.Outlined.ExpandLess
                    else -> Icons.Outlined.ExpandMore
                },
            )
            if (rows.isEmpty()) {
                Text(change.beforeAfter(), fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
            } else if (expanded) {
                rows.forEach { DriftRowLine(it) }
            }
            change.fix()?.let { fix ->
                TextButton(onClick = { onFix(fix) }) { Text(stringResource(R.string.insights_fix)) }
            }
        }
    }
}

/** "+ x" (ok), "− x" (bad), "~ name before → after" (warn); a long-press copies the full value. */
@Composable
private fun DriftRowLine(row: DriftRow) {
    val context = LocalContext.current
    val colors = LocalStatusColors.current
    val (sign, color, description) = when (row.kind) {
        DriftRowKind.ADDED -> Triple("+", colors.ok, stringResource(R.string.insights_diff_added, row.full))
        DriftRowKind.REMOVED -> Triple("−", colors.bad, stringResource(R.string.insights_diff_removed, row.full))
        DriftRowKind.CHANGED -> Triple("~", colors.warn, stringResource(R.string.insights_diff_changed, row.full))
    }
    val copyLabel = stringResource(R.string.insights_diff_copy)
    Row(
        Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = {},
                onLongClick = { copyToClipboard(context, row.full, row.full) },
                onLongClickLabel = copyLabel,
            )
            .semantics(mergeDescendants = true) { contentDescription = description },
    ) {
        Text(
            "$sign ${row.text}",
            color = color,
            fontFamily = FontFamily.Monospace,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}
