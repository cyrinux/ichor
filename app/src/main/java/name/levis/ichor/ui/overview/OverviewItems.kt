package name.levis.ichor.ui.overview

import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.staggeredgrid.LazyStaggeredGridScope
import androidx.compose.foundation.lazy.staggeredgrid.StaggeredGridItemSpan
import androidx.compose.runtime.Composable
import name.levis.ichor.model.OverviewCard

/**
 * Whether [card] spans both columns of the two-column overview (tablets, unfolded foldables):
 * the cluster summary heads it; the other cards flow into the columns.
 */
fun spansBothColumns(card: OverviewCard): Boolean = card == OverviewCard.SUMMARY

/**
 * The overview's items, added the same way to the one-column list (phones) and the two-column
 * staggered grid (tablets). A [fullLine] item spans both columns of the grid.
 */
internal interface OverviewItems {
    fun item(key: Any? = null, fullLine: Boolean = false, content: @Composable () -> Unit)
}

internal fun LazyListScope.overviewItems(): OverviewItems = object : OverviewItems {
    override fun item(key: Any?, fullLine: Boolean, content: @Composable () -> Unit) {
        this@overviewItems.item(key = key) { content() }
    }
}

internal fun LazyStaggeredGridScope.overviewItems(): OverviewItems = object : OverviewItems {
    override fun item(key: Any?, fullLine: Boolean, content: @Composable () -> Unit) {
        this@overviewItems.item(key = key, span = if (fullLine) StaggeredGridItemSpan.FullLine else null) { content() }
    }
}
