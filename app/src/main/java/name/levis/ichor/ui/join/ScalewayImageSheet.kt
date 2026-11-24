package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ScalewayImageCost
import name.levis.ichor.model.ScalewayImageSpec
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** What the image sheet opens with: the card's zone, the zone's architectures and the one to start from. */
internal data class ScalewayImageAsk(
    val zone: String,
    val arches: List<String>,
    val arch: String,
)

/**
 * "Make a Talos image": the architecture (the card's pick first), where the image goes, what is
 * created and deleted, and the cost line, then a plain confirmation: nothing of the cluster is
 * touched and the helper is deleted afterwards. The demo shows the sheet without a cost (it has no
 * key); its run is refused by the core.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ScalewayImageSheet(
    ask: ScalewayImageAsk,
    template: String,
    demo: Boolean,
    estimate: suspend (arch: String, zone: String) -> ScalewayImageCost,
    onStart: (ScalewayImageSpec) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    var arch by remember { mutableStateOf(ask.arch) }
    var cost by remember { mutableStateOf<UiState<ScalewayImageCost>?>(null) }
    var retry by remember { mutableStateOf(0) }

    LaunchedEffect(arch, retry) {
        if (demo) return@LaunchedEffect
        cost = UiState.Loading
        cost = uiStateOf { estimate(arch, ask.zone) }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.gcp_make_image)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.scaleway_image_intro))
                MutedText(stringResource(R.string.scaleway_image_place, ask.zone))
                Text(stringResource(R.string.hetzner_snapshot_arch), style = MaterialTheme.typography.labelLarge)
                if (ask.arches.size > 1) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        ask.arches.forEach { a -> IchorFilterChip(selected = a == arch, onClick = { arch = a }, label = { Text(a) }) }
                    }
                } else {
                    MutedText(arch)
                }
                if (demo) InfoNotice(stringResource(R.string.hetzner_demo))
                when (val state = cost) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, { retry++ })
                    is UiState.Loaded -> Text(costLine(state.data), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            IchorButton(
                onClick = { onStart(ScalewayImageSpec(templateNode = template, arch = arch, zone = ask.zone)) },
                enabled = demo || cost is UiState.Loaded,
            ) { Text(stringResource(R.string.azure_image_confirm)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** "About 10 minutes of DEV1-S (0.0099 EUR/h), then the 10 GB snapshot…", or the same without the price. */
@Composable
private fun costLine(cost: ScalewayImageCost): String =
    if (cost.hourly.isBlank()) {
        stringResource(R.string.scaleway_image_cost_unknown, cost.minutes, cost.type, cost.snapshotGB)
    } else {
        val hourly = if (cost.currency.isBlank()) cost.hourly else "${cost.hourly} ${cost.currency}"
        stringResource(R.string.scaleway_image_cost, cost.minutes, cost.type, hourly, cost.snapshotGB)
    }
