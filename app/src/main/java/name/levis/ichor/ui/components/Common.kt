package name.levis.ichor.ui.components

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.versionNotice
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.theme.LocalStatusColors

@Composable
fun LoadingBox(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator()
    }
}

@Composable
fun ErrorBox(message: UiText, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    if (message.isVpnRequired) {
        VpnRequiredBox(onRetry, modifier)
        return
    }
    val text = message.asString()
    // "This node's Talos version cannot do that" is information, not a failure. Retry stays:
    // a node that is still booting may answer the same before its API is complete.
    versionNotice(text)?.let {
        InfoBox(it.text(), modifier, onRetry)
        return
    }
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            Icons.Outlined.ErrorOutline,
            contentDescription = null,
            tint = LocalStatusColors.current.bad,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(text, textAlign = TextAlign.Center, style = MaterialTheme.typography.bodyMedium)
        Spacer(Modifier.height(16.dp))
        Button(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
    }
}

/**
 * Loading, error or the data with pull-to-refresh. A [header] (filters, a summary) sits
 * above the refreshable [content], outside the pull gesture.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> Loaded(
    state: UiState<T>,
    refresh: () -> Unit,
    modifier: Modifier = Modifier,
    header: (@Composable ColumnScope.(T) -> Unit)? = null,
    content: @Composable (T) -> Unit,
) {
    when (state) {
        UiState.Loading -> LoadingBox(modifier)
        is UiState.Failed -> ErrorBox(state.message, refresh, modifier)
        is UiState.Loaded -> if (header == null) {
            PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = refresh, modifier = modifier.fillMaxSize()) {
                content(state.data)
            }
        } else {
            Column(modifier.fillMaxSize()) {
                header(state.data)
                PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = refresh, modifier = Modifier.weight(1f)) {
                    content(state.data)
                }
            }
        }
    }
}

/** Small coloured dot + label, e.g. "Ready". */
@Composable
fun StatusPill(label: String, color: Color, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(50),
        color = color.copy(alpha = 0.15f),
        contentColor = color,
    ) {
        Row(
            Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(Modifier.size(8.dp).background(color, CircleShape))
            Spacer(Modifier.size(6.dp))
            Text(label, style = MaterialTheme.typography.labelMedium)
        }
    }
}

@Composable
fun NodeHealthPill(health: NodeHealth, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    when (health) {
        NodeHealth.READY -> StatusPill(stringResource(R.string.common_status_ready), colors.ok, modifier)
        NodeHealth.NOT_READY -> StatusPill(stringResource(R.string.common_status_not_ready), colors.warn, modifier)
        NodeHealth.UNREACHABLE -> StatusPill(stringResource(R.string.common_status_unreachable), colors.bad, modifier)
    }
}

/** Label/value row used in detail cards. */
@Composable
fun InfoRow(label: String, value: String, mono: Boolean = false, valueColor: Color = Color.Unspecified) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(0.4f),
        )
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = if (mono) FontFamily.Monospace else null,
            color = valueColor,
            modifier = Modifier.weight(0.6f),
        )
    }
}

/** Usage bar coloured by how full it is. */
@Composable
fun UsageBar(fraction: Float, modifier: Modifier = Modifier, warnAt: Float = 0.75f) {
    val colors = LocalStatusColors.current
    val color = when {
        fraction >= 0.9f -> colors.bad
        fraction >= warnAt -> colors.warn
        else -> colors.ok
    }
    LinearProgressIndicator(
        progress = { fraction },
        color = color,
        trackColor = MaterialTheme.colorScheme.surfaceVariant,
        modifier = modifier.fillMaxWidth().height(6.dp),
        drawStopIndicator = {},
    )
}

/** Top app bar "back" arrow. */
@Composable
fun BackButton(onClick: () -> Unit) {
    IconButton(onClick = onClick) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) }
}

/** Single-line filter box with a search icon. */
@Composable
fun SearchField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    shape: Shape = OutlinedTextFieldDefaults.shape,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        placeholder = { Text(placeholder, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
        singleLine = true,
        shape = shape,
        modifier = modifier,
    )
}

/** Muted placeholder line for an empty list. */
@Composable
fun EmptyText(text: String, modifier: Modifier = Modifier) {
    Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier.padding(16.dp))
}

/** Secondary small text: hints, captions, sub-lines. */
@Composable
fun MutedText(
    text: String,
    modifier: Modifier = Modifier,
    maxLines: Int = Int.MAX_VALUE,
    overflow: TextOverflow = TextOverflow.Clip,
) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier,
        maxLines = maxLines,
        overflow = overflow,
    )
}

/** [empty] when there is no [query], else [noMatch] formatted with the trimmed query. */
@Composable
fun emptyOrNoMatch(query: String, @StringRes empty: Int, @StringRes noMatch: Int): String =
    if (query.isBlank()) stringResource(empty) else stringResource(noMatch, query.trim())

/** Full-width switch row: [title], optional muted [description], tap anywhere to toggle. */
@Composable
fun ToggleRow(title: String, checked: Boolean, onChange: (Boolean) -> Unit, description: String? = null) {
    Row(
        Modifier.fillMaxWidth().toggleable(value = checked, role = Role.Switch, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            if (description != null) {
                MutedText(description)
            }
        }
        Switch(checked = checked, onCheckedChange = null, modifier = Modifier.padding(start = 8.dp))
    }
}

@Composable
fun SectionTitle(text: String, modifier: Modifier = Modifier) {
    Text(
        text,
        style = MaterialTheme.typography.titleSmall,
        color = MaterialTheme.colorScheme.primary,
        modifier = modifier.padding(top = 8.dp, bottom = 4.dp).semantics { heading() },
    )
}
