package name.levis.ichor.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.VpnLock
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.versionNotice
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * A section, card or sheet whose read failed: a warning glyph, "Couldn't load: <reason>" in muted
 * text and Retry when [onRetry] reruns that read. Red stays for what is red in the cluster and
 * for invalid input; a failed read is not the cluster's fault, and a retry may fix it.
 */
@Composable
fun LoadFailureRow(message: UiText, onRetry: (() -> Unit)?, modifier: Modifier = Modifier, compact: Boolean = false) {
    if (message.isVpnRequired) {
        FailureLine(stringResource(R.string.common_vpn_required), onRetry, modifier, compact, vpn = true)
        return
    }
    LoadFailureRow(message.asString(), onRetry, modifier, compact)
}

/** [LoadFailureRow] for a reason already turned into text. */
@Composable
fun LoadFailureRow(message: String, onRetry: (() -> Unit)?, modifier: Modifier = Modifier, compact: Boolean = false) {
    // "Needs Talos vX" is information, not a failure: said as such, Retry kept for a booting node.
    val notice = versionNotice(message)
    if (notice != null) {
        Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            InfoNotice(notice.text(), Modifier.weight(1f))
            onRetry?.let { RetryButton(it) }
        }
        return
    }
    FailureLine(stringResource(R.string.common_load_failed, message), onRetry, modifier, compact, vpn = false)
}

@Composable
private fun FailureLine(text: String, onRetry: (() -> Unit)?, modifier: Modifier, compact: Boolean, vpn: Boolean) {
    Row(
        modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (vpn) Icons.Outlined.VpnLock else Icons.Outlined.ErrorOutline,
            contentDescription = null,
            tint = LocalStatusColors.current.warn,
            modifier = Modifier.size(if (compact) 16.dp else 20.dp),
        )
        Text(
            text,
            style = if (compact) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).semantics { liveRegion = LiveRegionMode.Polite },
        )
        onRetry?.let { RetryButton(it) }
    }
}

@Composable
private fun RetryButton(onRetry: () -> Unit) {
    IchorTextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
}

/** What a section shows for its [UiState]; kept apart from the composable so it can be tested. */
internal enum class SectionPart { LOADING, CONTENT, FAILURE }

internal fun sectionParts(state: UiState<*>): List<SectionPart> = when (state) {
    UiState.Loading -> listOf(SectionPart.LOADING)
    is UiState.Failed -> listOf(SectionPart.FAILURE)
    is UiState.Loaded -> if (state.error != null && !state.refreshing) {
        listOf(SectionPart.CONTENT, SectionPart.FAILURE)
    } else {
        listOf(SectionPart.CONTENT)
    }
}

/**
 * The section-sized sibling of [Loaded]: [loading] while it loads, a compact [LoadFailureRow]
 * when it failed, [content] once loaded, followed by the row when a refresh failed with the data
 * still shown.
 */
@Composable
fun <T> SectionLoaded(
    state: UiState<T>,
    onRetry: (() -> Unit)?,
    modifier: Modifier = Modifier,
    verticalArrangement: Arrangement.Vertical = Arrangement.Top,
    loading: @Composable () -> Unit = { MutedText(stringResource(R.string.common_loading)) },
    content: @Composable (T) -> Unit,
) {
    Column(modifier, verticalArrangement = verticalArrangement) {
        sectionParts(state).forEach { part ->
            when (part) {
                SectionPart.LOADING -> loading()
                SectionPart.FAILURE -> failureOf(state)?.let { LoadFailureRow(it, onRetry, compact = true) }
                SectionPart.CONTENT -> (state as? UiState.Loaded<T>)?.let { content(it.data) }
            }
        }
    }
}

private fun failureOf(state: UiState<*>): UiText? = when (state) {
    is UiState.Failed -> state.message
    is UiState.Loaded -> state.error
    UiState.Loading -> null
}
