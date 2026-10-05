package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import java.text.NumberFormat
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import name.levis.ichor.R
import name.levis.ichor.model.PagedLoad
import name.levis.ichor.ui.components.InfoNotice

/** Rows from the end of the list at which the next page is asked for. */
private const val LOAD_AHEAD = 30

@Composable
private fun formatCount(n: Long): String {
    val locale = LocalConfiguration.current.locales[0]
    return NumberFormat.getIntegerInstance(locale).format(n)
}

/**
 * A thin bar over the list while pages load ("1,500 / ~4,000"); nothing for a list that
 * came in one page, so a small cluster looks as before.
 */
@Composable
internal fun PagedProgress(progress: PagedLoad<*>?) {
    if (progress == null || progress.done) return
    val total = progress.estimatedTotal
    val loaded = progress.items.size.toLong()
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        if (total != null && total > 0) {
            LinearProgressIndicator(progress = { (loaded.toFloat() / total).coerceIn(0f, 1f) }, modifier = Modifier.fillMaxWidth())
        } else {
            LinearProgressIndicator(Modifier.fillMaxWidth())
        }
        Text(
            if (total != null) stringResource(R.string.kube_paged_progress, formatCount(loaded), formatCount(total))
            else stringResource(R.string.kube_paged_progress_unknown, formatCount(loaded)),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/**
 * What an incomplete list means (L8): past the cap, how much is shown and a hint to pick a
 * namespace, and buttons to load the next page or the rest ([onLoadMore], [onLoadAll]: while
 * searching, scrolling does not load more); and, while [searching] or capped, that sorting and
 * search only cover the loaded rows.
 */
@Composable
internal fun IncompleteNotice(load: PagedLoad<*>, searching: Boolean, onLoadMore: () -> Unit, onLoadAll: () -> Unit) {
    if (load.done) return
    val capped = load.hasMore && load.capped
    if (!capped && !searching) return
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        if (capped) {
            val shown = formatCount(load.items.size.toLong())
            val total = load.estimatedTotal
            InfoNotice(
                if (total != null) stringResource(R.string.kube_paged_capped, shown, formatCount(total))
                else stringResource(R.string.kube_paged_capped_unknown, shown),
            )
        }
        InfoNotice(stringResource(R.string.kube_paged_partial_note))
        if (capped) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(onClick = onLoadMore) { Text(stringResource(R.string.kube_paged_load_more)) }
                TextButton(onClick = onLoadAll) { Text(stringResource(R.string.kube_paged_load_all)) }
            }
        }
    }
}

/**
 * Asks for the next page ([onLoadMore]) when the list is scrolled near its end, while
 * [enabled] (a capped list with more pages). Keyed on [loaded]: a page that does not fill the
 * screen asks for the next one.
 */
@Composable
internal fun LoadMoreOnScroll(state: LazyListState, enabled: Boolean, loaded: Int, onLoadMore: () -> Unit) {
    val load by rememberUpdatedState(onLoadMore)
    LaunchedEffect(state, enabled, loaded) {
        if (!enabled) return@LaunchedEffect
        snapshotFlow {
            val info = state.layoutInfo
            (info.visibleItemsInfo.lastOrNull()?.index ?: 0) >= info.totalItemsCount - LOAD_AHEAD
        }.distinctUntilChanged().filter { it }.collect { load() }
    }
}
