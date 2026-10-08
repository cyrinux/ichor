package name.levis.ichor.ui.overview

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.dataservices.color

/**
 * The frame of a GitOps tool's overview card, opening its screen: the tool's tile, [title] and
 * app count, then a skeleton while loading, one muted line on failure, else [body].
 */
@Composable
internal fun <T> GitOpsCardFrame(
    tile: InventoryApp,
    @StringRes title: Int,
    state: UiState<T>,
    appCount: (T) -> Int,
    onOpen: () -> Unit,
    body: @Composable (T) -> Unit,
) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Header(tile, title, (state as? UiState.Loaded)?.data?.let(appCount))
            when (state) {
                UiState.Loading -> Box(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh))
                is UiState.Failed -> MutedText(stringResource(R.string.data_services_unreadable, state.message.asString()), maxLines = 2, overflow = TextOverflow.Ellipsis)
                is UiState.Loaded -> body(state.data)
            }
        }
    }
}

@Composable
private fun Header(tile: InventoryApp, @StringRes title: Int, apps: Int?) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        AppIconTile(tile, size = 28.dp)
        Spacer(Modifier.size(12.dp))
        Text(stringResource(title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
        if (apps != null) {
            Text(
                pluralStringResource(R.plurals.argo_apps, apps, apps),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Icon(
            Icons.AutoMirrored.Outlined.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp),
        )
    }
}
