package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * The widest a page's cards, lists and forms get: on an unfolded foldable, a tablet or a
 * desktop window they stay centred at this width instead of stretching edge to edge. A phone
 * (under 600 dp) never reaches it.
 */
val ReadableWidth: Dp = 720.dp

/** Centred, and no wider than [max]: the whole width on a phone. */
fun Modifier.readableWidth(max: Dp = ReadableWidth): Modifier =
    fillMaxWidth().wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = max).fillMaxWidth()

/**
 * A Scaffold's content: inside its bars ([padding]), at a [readableWidth]. The bars keep the
 * whole width. Pages that use every column (logs, a terminal, YAML, maps, charts) use
 * `Modifier.padding(padding)` instead.
 */
fun Modifier.pageContent(padding: PaddingValues): Modifier = padding(padding).readableWidth()
