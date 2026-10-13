package name.levis.ichor.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.ichor.R

/** One section of a long list: [index] is the position of its header item in the `LazyColumn`. */
data class JumpSection(val key: Any, val label: String, val index: Int)

/**
 * The section the list is showing: the last one whose header is at or above the first visible
 * row, the first one before any header, the last one once the list cannot scroll further.
 */
fun currentSection(sections: List<JumpSection>, firstVisibleIndex: Int, atEnd: Boolean): JumpSection? = when {
    sections.isEmpty() -> null
    atEnd -> sections.last()
    else -> sections.lastOrNull { it.index <= firstVisibleIndex } ?: sections.first()
}

/** The section under a finger at [fraction] (0 top, 1 bottom) of the rail, each section an equal slice. */
fun sectionAt(sections: List<JumpSection>, fraction: Float): JumpSection? {
    if (sections.isEmpty()) return null
    val slot = (fraction.coerceIn(0f, 1f) * sections.size).toInt()
    return sections[slot.coerceAtMost(sections.lastIndex)]
}

private const val LABELS_LINGER_MS = 600L

/**
 * A rail of dots at the trailing edge of a long list, one per section: the filled dot follows
 * the scroll, a tap jumps to that section's header, a drag along the rail shows the labels and
 * scrubs through the sections with a haptic tick on each change. Hidden when there are fewer
 * than two sections or the list fits. Place it over the list in a `Box`, not beside it.
 */
@Composable
fun SectionJumpRail(sections: List<JumpSection>, listState: LazyListState, modifier: Modifier = Modifier) {
    val visible by remember(sections) {
        derivedStateOf { sections.size >= 2 && (listState.canScrollForward || listState.canScrollBackward) }
    }
    if (!visible) return

    val current by remember(sections) {
        derivedStateOf { currentSection(sections, listState.firstVisibleItemIndex, atEnd = !listState.canScrollForward) }
    }
    val scope = rememberCoroutineScope()
    val haptics = LocalHapticFeedback.current
    val latestSections by rememberUpdatedState(sections)
    var labelsShown by remember { mutableStateOf(false) }
    var railHeight by remember { mutableStateOf(0) }
    var hideJob by remember { mutableStateOf<Job?>(null) }
    var scrubbed by remember { mutableStateOf<JumpSection?>(null) }

    fun scrubTo(y: Float) {
        if (railHeight <= 0) return
        val target = sectionAt(latestSections, y / railHeight) ?: return
        if (target == scrubbed) return
        scrubbed = target
        haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
        scope.launch { listState.scrollToItem(target.index) }
    }

    fun endScrub() {
        scrubbed = null
        hideJob = scope.launch {
            delay(LABELS_LINGER_MS)
            labelsShown = false
        }
    }

    Column(
        modifier
            .padding(end = 4.dp)
            .onSizeChanged { railHeight = it.height }
            .pointerInput(Unit) {
                detectVerticalDragGestures(
                    onDragStart = { offset ->
                        hideJob?.cancel()
                        labelsShown = true
                        scrubTo(offset.y)
                    },
                    onDragEnd = { endScrub() },
                    onDragCancel = { endScrub() },
                    onVerticalDrag = { change, _ ->
                        change.consume()
                        scrubTo(change.position.y)
                    },
                )
            }
            .semantics { isTraversalGroup = true },
        horizontalAlignment = Alignment.End,
    ) {
        sections.forEach { section ->
            RailDot(
                section = section,
                filled = section == current,
                labelShown = labelsShown,
                onClick = { scope.launch { listState.animateScrollToItem(section.index) } },
            )
        }
    }
}

@Composable
private fun RailDot(section: JumpSection, filled: Boolean, labelShown: Boolean, onClick: () -> Unit) {
    val description = stringResource(R.string.section_rail_jump_to, section.label)
    val colors = MaterialTheme.colorScheme
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.End) {
        AnimatedVisibility(labelShown, enter = fadeIn(), exit = fadeOut()) {
            Text(
                section.label,
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier
                    .padding(end = 4.dp)
                    .background(colors.surfaceContainerHigh, RoundedCornerShape(50))
                    .padding(horizontal = 8.dp, vertical = 2.dp),
            )
        }
        Box(
            Modifier
                .size(width = 24.dp, height = 32.dp)
                .clickable(role = Role.Button, onClick = onClick)
                .semantics {
                    contentDescription = description
                    selected = filled
                },
            contentAlignment = Alignment.Center,
        ) {
            val dot = Modifier.size(8.dp)
            Box(
                if (filled) dot.background(colors.primary, CircleShape) else dot.border(1.dp, colors.outlineVariant, CircleShape),
            )
        }
    }
}
