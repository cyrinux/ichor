package name.levis.ichor.ui.overview

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedContentTransitionScope.SlideDirection
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.UnfoldMore
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.StoredConfig
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.adjacentContext
import name.levis.ichor.model.CLUSTER_NAME_MAX
import name.levis.ichor.model.CLUSTER_SEEDS
import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.accessLabel
import name.levis.ichor.model.hueOf
import name.levis.ichor.model.seedFromHue
import name.levis.ichor.model.seedOf
import name.levis.ichor.monitor.CERT_WARN_DAYS
import name.levis.ichor.ui.importconfig.certExpiry
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.daysUntil

/** Horizontal drag past which the header switches cluster. */
private val SWIPE_THRESHOLD = 56.dp

/** Past this many clusters the page dots become a "3/12" counter. */
private const val MAX_DOTS = 6

private const val SWATCHES_PER_ROW = 4

/**
 * Swiping the overview's top bar sideways shows the previous or next cluster, like pages
 * (left: the next one). The top bar is where a horizontal swipe is free: the node rows
 * have their own.
 */
fun Modifier.clusterSwipe(config: StoredConfig?, onSelect: (String) -> Unit): Modifier {
    if (config == null || config.summary.contexts.size < 2) return this
    return pointerInput(config.summary, config.activeContext) {
        val threshold = SWIPE_THRESHOLD.toPx()
        var dragged = 0f
        detectHorizontalDragGestures(
            onDragStart = { dragged = 0f },
            onDragEnd = {
                val step = when {
                    dragged <= -threshold -> 1
                    dragged >= threshold -> -1
                    else -> 0
                }
                adjacentContext(config.summary, config.activeContext, step)?.let(onSelect)
            },
            onHorizontalDrag = { _, amount -> dragged += amount },
        )
    }
}

/**
 * The overview's title: the cluster on screen, its access level and, with several clusters,
 * which one of them it is. Tapping it opens the cluster list; [badge] leads the second line.
 */
@Composable
fun ClusterTitle(
    config: StoredConfig?,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onOpen: () -> Unit,
    badge: @Composable () -> Unit,
) {
    val contexts = config?.summary?.contexts.orEmpty()
    val active = contexts.indexOfFirst { it.name == config?.activeContext }
    Column(
        Modifier
            .clip(MaterialTheme.shapes.small)
            .clickable(
                enabled = config != null,
                onClickLabel = stringResource(R.string.clusters_switch),
                role = Role.Button,
                onClick = onOpen,
            )
            .padding(end = 4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            AnimatedContent(
                // Position and name: a renamed cluster stays in place, another one slides in.
                targetState = active to (config?.activeSummary?.let(labels::of) ?: stringResource(R.string.overview_title)),
                transitionSpec = {
                    // The name slides the way the swipe goes: the next cluster comes from the end.
                    val forward = targetState.first >= initialState.first
                    val towards = if (forward) SlideDirection.Start else SlideDirection.End
                    (slideIntoContainer(towards) + fadeIn()) togetherWith (slideOutOfContainer(towards) + fadeOut())
                },
                label = "cluster",
                modifier = Modifier.weight(1f, fill = false),
            ) { (_, name) ->
                Text(name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (config != null) {
                Icon(
                    Icons.Outlined.UnfoldMore,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 2.dp).size(18.dp),
                )
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            badge()
            config?.activeSummary?.let { active ->
                Text(
                    stringResource(active.accessLabel),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false).padding(end = 8.dp),
                )
            }
            ClusterPosition(contexts, active, colors)
        }
    }
}

/** Page dots, each in its cluster's color, the one on screen larger. */
@Composable
private fun ClusterPosition(contexts: List<ContextSummary>, active: Int, colors: Map<String, Int>) {
    if (contexts.size < 2 || active < 0) return
    if (contexts.size > MAX_DOTS) {
        Text(
            "${active + 1}/${contexts.size}",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        contexts.forEachIndexed { index, context ->
            val color = Color(colors.seedOf(context))
            Box(
                Modifier
                    .size(if (index == active) 8.dp else 5.dp)
                    .background(if (index == active) color else color.copy(alpha = 0.55f), CircleShape),
            )
        }
    }
}

/**
 * The imported clusters: pick the one to show, rename it, change its color, remove it, or
 * add one. Removing asks first; [onRemove] then drops the cluster's credentials from the device.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ClusterSheet(
    config: StoredConfig,
    colors: Map<String, Int>,
    labels: ClusterLabels,
    onSelect: (String) -> Unit,
    onRename: (ContextSummary, String) -> Unit,
    onColor: (ContextSummary, Int) -> Unit,
    onAdd: () -> Unit,
    onRemove: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var removing by remember { mutableStateOf<ContextSummary?>(null) }
    var renaming by remember { mutableStateOf<ContextSummary?>(null) }
    var coloring by remember { mutableStateOf<ContextSummary?>(null) }
    val contexts = config.summary.contexts

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(bottom = 24.dp)) {
            Text(
                stringResource(R.string.clusters_title),
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
            )
            contexts.forEach { context ->
                ClusterRow(
                    context = context,
                    labels = labels,
                    selected = context.name == config.activeContext,
                    color = Color(colors.seedOf(context)),
                    onSelect = { onSelect(context.name) },
                    // Not in screenshot mode: the dialog would show the given name.
                    onRename = { renaming = context }.takeIf { !labels.masked && context.fingerprint.isNotBlank() },
                    onColor = { coloring = context },
                    onRemove = { removing = context },
                )
            }
            if (contexts.size > 1) {
                Text(
                    stringResource(R.string.clusters_swipe_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
                )
            }
            OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 8.dp)) {
                Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.padding(end = 8.dp).size(18.dp))
                Text(stringResource(R.string.clusters_add))
            }
        }
    }

    removing?.let { context ->
        AlertDialog(
            onDismissRequest = { removing = null },
            title = { Text(stringResource(R.string.clusters_remove_title, labels.of(context))) },
            text = { Text(stringResource(R.string.clusters_remove_body)) },
            confirmButton = {
                TextButton(onClick = {
                    removing = null
                    onRemove(context.name)
                }) { Text(stringResource(R.string.common_delete)) }
            },
            dismissButton = { TextButton(onClick = { removing = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }

    renaming?.let { context ->
        ClusterNameDialog(
            context = context,
            given = labels.given(context).orEmpty(),
            onRename = {
                onRename(context, it)
                renaming = null
            },
            onDismiss = { renaming = null },
        )
    }

    coloring?.let { context ->
        ClusterColorDialog(
            name = labels.of(context),
            color = colors.seedOf(context),
            onPick = {
                onColor(context, it)
                coloring = null
            },
            onDismiss = { coloring = null },
        )
    }
}

@Composable
private fun ClusterRow(
    context: ContextSummary,
    labels: ClusterLabels,
    selected: Boolean,
    color: Color,
    onSelect: () -> Unit,
    onRename: (() -> Unit)?,
    onColor: () -> Unit,
    onRemove: () -> Unit,
) {
    val label = labels.of(context)
    ListItem(
        headlineContent = { Text(label, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = {
            Column {
                // Renamed: which talosconfig context that is.
                if (labels.given(context) != null) {
                    Text(context.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Text(
                    listOfNotNull(context.endpoints.firstOrNull(), stringResource(context.accessLabel)).joinToString(" · "),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                // Which cluster needs a new talosconfig soon, without having to open each.
                if (context.certNotAfter > 0 && daysUntil(context.certNotAfter) <= CERT_WARN_DAYS) {
                    Text(
                        "${stringResource(R.string.common_label_cert_expires)}: ${certExpiry(context.certNotAfter)}",
                        color = LocalStatusColors.current.warn,
                    )
                }
            }
        },
        leadingContent = { RadioButton(selected = selected, onClick = null) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                onRename?.let {
                    IconButton(onClick = it) { Icon(Icons.Outlined.Edit, stringResource(R.string.clusters_rename, label)) }
                }
                IconButton(onClick = onColor) {
                    Swatch(color, contentDescription = stringResource(R.string.clusters_color, label))
                }
                IconButton(onClick = onRemove) {
                    Icon(Icons.Outlined.Delete, stringResource(R.string.clusters_remove, label))
                }
            }
        },
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        modifier = Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onSelect),
    )
}

@Composable
private fun Swatch(color: Color, contentDescription: String?, selected: Boolean = false) {
    val outline = MaterialTheme.colorScheme.onSurface
    Box(
        Modifier
            .size(24.dp)
            .background(color, CircleShape)
            .then(if (selected) Modifier.border(2.dp, outline, CircleShape) else Modifier)
            .semantics { contentDescription?.let { this.contentDescription = it } },
    )
}

/**
 * The name to show for [context] instead of its talosconfig one, [given] so far. Left
 * empty, the context name comes back. Only on this device: the talosconfig is unchanged.
 */
@Composable
private fun ClusterNameDialog(context: ContextSummary, given: String, onRename: (String) -> Unit, onDismiss: () -> Unit) {
    var name by remember { mutableStateOf(given) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.clusters_rename, given.ifEmpty { context.name })) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it.take(CLUSTER_NAME_MAX) },
                    label = { Text(stringResource(R.string.clusters_rename_label)) },
                    placeholder = { Text(context.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(stringResource(R.string.clusters_rename_hint, context.name), style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { TextButton(onClick = { onRename(name) }) { Text(stringResource(R.string.common_ok)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** A cluster's main color: one of the preset ones, or any hue. The app's palette derives from it. */
@Composable
private fun ClusterColorDialog(name: String, color: Int, onPick: (Int) -> Unit, onDismiss: () -> Unit) {
    var picked by remember { mutableIntStateOf(color) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.clusters_color, name)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.clusters_color_hint), style = MaterialTheme.typography.bodySmall)
                CLUSTER_SEEDS.chunked(SWATCHES_PER_ROW).forEach { seeds ->
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        seeds.forEach { seed ->
                            IconButton(onClick = { picked = seed }) {
                                Swatch(Color(seed), contentDescription = null, selected = seed == picked)
                            }
                        }
                    }
                }
                // Any other hue: the bar shows what the slider under it picks.
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(10.dp)
                        .background(Brush.horizontalGradient(HUE_BAR), MaterialTheme.shapes.small),
                )
                Slider(
                    value = hueOf(picked),
                    onValueChange = { picked = seedFromHue(it) },
                    valueRange = 0f..MAX_HUE,
                )
                Box(Modifier.fillMaxWidth().height(28.dp).background(Color(picked), MaterialTheme.shapes.small))
            }
        },
        confirmButton = { TextButton(onClick = { onPick(picked) }) { Text(stringResource(R.string.common_ok)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

private const val MAX_HUE = 359f
private const val HUE_BAR_STEP = 30

private val HUE_BAR: List<Color> = (0..MAX_HUE.toInt() step HUE_BAR_STEP).map { Color(seedFromHue(it.toFloat())) } +
    Color(seedFromHue(MAX_HUE))
