package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CellTone
import name.levis.ichor.model.KubeObjectSummary
import name.levis.ichor.model.ObjectOwner
import name.levis.ichor.model.SummaryCondition
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.workloads.KubeEventRows

/** Labels and annotations shown before "Show all". */
private const val METADATA_PREVIEW = 6

/**
 * The summary tab of an object: its health, the spec fields read first, its conditions, who
 * owns or manages it ([onOwner] opens one), its events and metadata.
 */
@Composable
fun ObjectSummaryContent(state: UiState<KubeObjectSummary>, onRetry: () -> Unit, onOwner: (ObjectOwner) -> Unit, modifier: Modifier = Modifier) {
    when (state) {
        UiState.Loading -> Box(modifier) { LoadingBox(style = SkeletonStyle.TEXT) }
        is UiState.Failed -> Box(modifier) { ErrorBox(state.message, onRetry) }
        is UiState.Loaded -> {
            val s = state.data
            val now = remember(s) { System.currentTimeMillis() }
            Column(modifier.verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                HealthLine(s)
                Highlights(s)
                SectionTitle(stringResource(R.string.kb_summary_conditions))
                if (s.conditions.isEmpty()) MutedText(stringResource(R.string.kb_summary_no_conditions))
                s.conditions.forEach { ConditionRow(it, now) }
                if (s.owners.isNotEmpty()) {
                    SectionTitle(stringResource(R.string.kb_summary_owners))
                    s.owners.forEach { OwnerRow(it, onOwner) }
                }
                SectionTitle(stringResource(R.string.kube_events_title))
                if (s.eventsError.isNotEmpty()) {
                    MutedText(stringResource(R.string.kb_summary_events_error, s.eventsError))
                } else {
                    KubeEventRows(s.events, showObject = false, now = now, key = "${s.namespace}/${s.kind}/${s.name}")
                }
                Metadata(s, now)
            }
        }
    }
}

@Composable
private fun healthText(tone: CellTone): String = when (tone) {
    CellTone.OK -> stringResource(R.string.kb_summary_health_ok)
    CellTone.WARN -> stringResource(R.string.kb_summary_health_warn)
    CellTone.BAD -> stringResource(R.string.kb_summary_health_bad)
    CellTone.NONE -> stringResource(R.string.kb_summary_health_none)
}

@Composable
private fun CellTone.colorOrMuted() = if (this == CellTone.NONE) LocalStatusColors.current.muted else color()

@Composable
private fun HealthLine(s: KubeObjectSummary) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        ToneLabel(healthText(s.healthTone), s.healthTone.colorOrMuted())
        if (s.healthReason.isNotEmpty()) MutedText(s.healthReason, Modifier.weight(1f), maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Highlights(s: KubeObjectSummary) {
    if (s.highlights.isEmpty() && s.phase.isEmpty()) return
    FlowRow(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (s.phase.isNotEmpty()) FieldChip(stringResource(R.string.kb_summary_phase), s.phase)
        s.highlights.forEach { FieldChip(highlightLabel(it.key), it.value) }
    }
}

@Composable
private fun highlightLabel(key: String): String = when (key) {
    "replicas" -> stringResource(R.string.kb_summary_replicas)
    "selector" -> stringResource(R.string.kb_summary_selector)
    "image" -> stringResource(R.string.kb_summary_image)
    "node" -> stringResource(R.string.kb_summary_node)
    "suspended" -> stringResource(R.string.kb_summary_suspended)
    "schedule" -> stringResource(R.string.kb_summary_schedule)
    else -> key
}

/** A condition: its type tinted by tone, status and reason, how long ago it changed, its message. */
@Composable
private fun ConditionRow(c: SummaryCondition, now: Long) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            ToneLabel(c.type, c.toneValue.colorOrMuted())
            Text(listOf(c.status, c.reason).filter { it.isNotEmpty() }.joinToString("  ·  "), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (c.lastTransition > 0) MutedText(stringResource(R.string.kube_events_ago, ageSince(c.lastTransition, now)), maxLines = 1)
        }
        if (c.message.isNotEmpty()) Text(c.message, style = MaterialTheme.typography.bodySmall, maxLines = 6, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun ownerTag(o: ObjectOwner): String? = when (o.via) {
    ObjectOwner.VIA_FLUX -> "Flux"
    ObjectOwner.VIA_ARGO -> "Argo CD"
    ObjectOwner.VIA_HELM -> stringResource(R.string.kb_summary_helm_release)
    else -> if (o.controller) stringResource(R.string.kb_summary_controller) else null
}

/** An owner or manager; tappable when its summary (or its Helm release) can open. */
@Composable
private fun OwnerRow(o: ObjectOwner, onOwner: (ObjectOwner) -> Unit) {
    val tappable = o.openable || o.isHelmRelease
    Row(
        Modifier.fillMaxWidth().then(if (tappable) Modifier.clickable { onOwner(o) } else Modifier).padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(if (o.isHelmRelease) "Helm" else o.kind, style = MaterialTheme.typography.labelMedium)
                ownerTag(o)?.let { ToneLabel(it, LocalStatusColors.current.muted) }
            }
            Text(
                listOf(o.namespace, o.name).filter { it.isNotEmpty() }.joinToString("/"),
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (tappable) Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null)
    }
}

@Composable
private fun Metadata(s: KubeObjectSummary, now: Long) {
    SectionTitle(stringResource(R.string.kb_summary_metadata))
    if (s.created > 0) MutedText(stringResource(R.string.kb_summary_created, ageSince(s.created, now)))
    if (s.deleting > 0) Text(stringResource(R.string.kb_summary_deleting, ageSince(s.deleting, now)), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn)
    if (s.finalizers.isNotEmpty()) KeyValues(stringResource(R.string.kb_summary_finalizers), s.finalizers.map { it to "" }, "finalizers")
    if (s.labels.isNotEmpty()) KeyValues(stringResource(R.string.kb_summary_labels), s.labels.toSortedMap().toList(), "labels")
    if (s.annotations.isNotEmpty()) KeyValues(stringResource(R.string.kb_summary_annotations), s.annotations.toSortedMap().toList(), "annotations")
}

/** "key=value" lines (or keys alone), selectable, the first few then "Show all". */
@Composable
private fun KeyValues(title: String, entries: List<Pair<String, String>>, key: String) {
    var all by rememberSaveable(key) { mutableStateOf(false) }
    Text(title, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(top = 6.dp))
    SelectionContainer {
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            (if (all) entries else entries.take(METADATA_PREVIEW)).forEach { (k, v) ->
                Text(if (v.isEmpty()) k else "$k=$v", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            }
        }
    }
    if (entries.size > METADATA_PREVIEW) {
        TextButton(onClick = { all = !all }) {
            Text(if (all) stringResource(R.string.checkup_show_less) else stringResource(R.string.checkup_show_all, entries.size.toString()))
        }
    }
}
