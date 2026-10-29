package name.levis.ichor.ui.overview

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.TalosApp
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.InventoryApp
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.apps.AppIconTile
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard

/**
 * The frame of a GitOps tool's overview card, opening its screen: the shared header (the tool's
 * tile, [title], its app count and one "N need a look" pill from [attention], the levels of the
 * apps that need a look), then a skeleton while loading, one muted line on failure, else [body].
 */
@Composable
internal fun <T> GitOpsCardFrame(
    tile: InventoryApp,
    @StringRes title: Int,
    state: UiState<T>,
    appCount: (T) -> Int,
    attention: (T) -> List<ServiceHealth>,
    onOpen: () -> Unit,
    body: @Composable (T) -> Unit,
) {
    val data = (state as? UiState.Loaded)?.data
    SectionCard(
        stringResource(title),
        subtitle = data?.let(appCount)?.let { pluralStringResource(R.plurals.argo_apps, it, it) },
        pill = data?.let { attentionPill(attention(it)) },
        onOpen = onOpen,
        leading = { AppIconTile(tile, size = 28.dp) },
    ) {
        when (state) {
            UiState.Loading -> Box(
                Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceContainerHigh),
            )
            is UiState.Failed -> MutedText(
                stringResource(R.string.data_services_unreadable, state.message.asString()),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            is UiState.Loaded -> {
                body(state.data)
                UpdatesCount()
            }
        }
    }
}

/** "Updates available: N" once the Updates screen looked this cluster up in this session. */
@Composable
private fun UpdatesCount() {
    val talos = LocalContext.current.applicationContext as? TalosApp ?: return
    val outdated by talos.updatesRepository.outdated.collectAsStateWithLifecycle()
    val count = outdated[talos.updatesRepository.clusterKey()] ?: return
    if (count > 0) MutedText(stringResource(R.string.updates_card_count, count))
}
