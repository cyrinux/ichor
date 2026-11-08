package name.levis.ichor.ui.overview

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import name.levis.ichor.R
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.color

/**
 * The overview cards' one pill, "3 need a look" in the colour of the worst of [levels] (the
 * levels of what needs a look); null when nothing does, so the header shows no pill at all.
 */
internal fun attentionPill(levels: List<ServiceHealth>, modifier: Modifier = Modifier): (@Composable () -> Unit)? {
    val needing = levels.filter { it.needsAttention }
    if (needing.isEmpty()) return null
    // CRITICAL sorts before WARNING.
    val worst = needing.minBy { it.ordinal }
    return { StatusPill(pluralStringResource(R.plurals.apps_attention, needing.size, needing.size), worst.color(), modifier) }
}
