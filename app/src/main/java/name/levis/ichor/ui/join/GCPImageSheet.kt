package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import name.levis.ichor.model.GCPImageEstimate
import name.levis.ichor.model.GCPImageSpec
import name.levis.ichor.model.GCPSubnet
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** What the image sheet opens with: the card's project, zone, subnet and public address, and the picked type's arch. */
internal data class GCPImageAsk(
    val project: String,
    val zone: String,
    val arch: String,
    val subnet: GCPSubnet,
    val publicIp: Boolean,
)

/**
 * "Make a Talos image": the architecture of the machine type picked on the card, where the image
 * goes and where the helper VM runs (the card's picks), and the cost line, then a plain
 * confirmation: nothing of the cluster is touched and the helper is deleted afterwards.
 */
@Composable
internal fun GCPImageSheet(
    ask: GCPImageAsk,
    template: String,
    estimate: suspend (arch: String, zone: String) -> GCPImageEstimate,
    onStart: (GCPImageSpec) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    var cost by remember { mutableStateOf<UiState<GCPImageEstimate>?>(null) }
    var retry by remember { mutableStateOf(0) }

    LaunchedEffect(retry) {
        cost = UiState.Loading
        cost = uiStateOf { estimate(ask.arch, ask.zone) }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.gcp_make_image)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.gcp_image_intro))
                MutedText(stringResource(R.string.gcp_image_place, ask.project, ask.zone, ask.subnet.name.ifBlank { ask.subnet.selfLink }))
                Text(stringResource(R.string.hetzner_snapshot_arch), style = MaterialTheme.typography.labelLarge)
                MutedText(ask.arch)
                MutedText(stringResource(if (ask.publicIp) R.string.azure_image_network_hint else R.string.gcp_image_nat_hint))
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
                onClick = {
                    onStart(
                        GCPImageSpec(
                            templateNode = template, arch = ask.arch, project = ask.project, zone = ask.zone,
                            subnetwork = ask.subnet.selfLink.ifBlank { ask.subnet.name }, associatePublicIp = ask.publicIp,
                        ),
                    )
                },
                enabled = cost is UiState.Loaded,
            ) { Text(stringResource(R.string.azure_image_confirm)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** "About 10 minutes of e2-small (0.0168 USD/h), then 0.0500 USD per GB-month for the image", or the same without the prices. */
@Composable
private fun costLine(cost: GCPImageEstimate): String =
    if (cost.hourly.isBlank() || cost.imageMonthlyPerGB.isBlank()) {
        stringResource(R.string.gcp_image_cost_unknown, cost.minutes, cost.machineType)
    } else {
        stringResource(
            R.string.gcp_image_cost,
            cost.minutes,
            cost.machineType,
            "${cost.hourly} ${cost.currency}",
            "${cost.imageMonthlyPerGB} ${cost.currency}",
        )
    }
