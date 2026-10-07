package name.levis.ichor.ui.components

import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.unit.Velocity

/**
 * Keeps a list's leftover upward scroll and fling from the bottom sheet around it. Swiped up at
 * its end, the list hands the rest of the fling to the sheet, which springs past its expanded
 * position and bounces up and down. Downward, the sheet still gets it: pulled down from the
 * top of the list, the sheet closes.
 */
fun Modifier.upwardScrollStaysInSheet(): Modifier = nestedScroll(UpwardScrollStaysInSheet)

private object UpwardScrollStaysInSheet : NestedScrollConnection {
    override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset =
        if (available.y < 0) available.copy(x = 0f) else Offset.Zero

    override suspend fun onPostFling(consumed: Velocity, available: Velocity): Velocity =
        if (available.y < 0) available.copy(x = 0f) else Velocity.Zero
}
