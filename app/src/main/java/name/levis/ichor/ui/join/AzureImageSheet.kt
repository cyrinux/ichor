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
import name.levis.ichor.model.AzureImageEstimate
import name.levis.ichor.model.AzureImageSpec
import name.levis.ichor.model.AzureOptions
import name.levis.ichor.model.locationLabel
import name.levis.ichor.model.subnetsIn
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

private val azureImageArchitectures = listOf("amd64", "arm64")

/** What the image sheet opens with: the card's current picks. */
internal data class AzureImageAsk(
    val options: AzureOptions,
    val resourceGroup: String,
    val arch: String,
    val networkId: String,
    val subnetId: String,
    val nsgId: String,
)

/**
 * "Make a Talos image": the architecture (that of the size or image picked on the card, editable),
 * the helper VM's network, subnet and security group (the card's picks, editable) and the cost
 * line, then a plain confirmation: nothing of the cluster is touched and the helper is deleted
 * afterwards. The image goes in the card's resource group and location.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun AzureImageSheet(
    ask: AzureImageAsk,
    template: String,
    estimate: suspend (arch: String, location: String) -> AzureImageEstimate,
    onStart: (AzureImageSpec) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val options = ask.options
    var arch by remember { mutableStateOf(ask.arch.ifBlank { azureImageArchitectures.first() }) }
    var networkId by remember { mutableStateOf(ask.networkId) }
    var subnetId by remember { mutableStateOf(ask.subnetId) }
    var nsgId by remember { mutableStateOf(ask.nsgId) }
    var cost by remember { mutableStateOf<UiState<AzureImageEstimate>?>(null) }
    var retry by remember { mutableStateOf(0) }
    val subnet = options.subnetsIn(networkId).firstOrNull { it.id == subnetId }

    LaunchedEffect(arch, retry) {
        cost = UiState.Loading
        cost = uiStateOf { estimate(arch, options.location) }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.azure_make_image)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                MutedText(stringResource(R.string.azure_image_intro))
                MutedText(stringResource(R.string.azure_image_group, ask.resourceGroup, options.locationLabel(options.location)))
                Text(stringResource(R.string.hetzner_snapshot_arch), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    azureImageArchitectures.forEach { a -> IchorFilterChip(selected = a == arch, onClick = { arch = a }, label = { Text(a) }) }
                }
                HelperNetwork(options, networkId, subnetId, nsgId, onNetwork = {
                    if (it != networkId) subnetId = ""
                    networkId = it
                }, onSubnet = { subnetId = it }, onNsg = { nsgId = if (it == nsgId) "" else it })
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
                        AzureImageSpec(
                            templateNode = template, arch = arch, resourceGroup = ask.resourceGroup, location = options.location,
                            subnetId = subnetId, securityGroupId = nsgId,
                        ),
                    )
                },
                enabled = cost is UiState.Loaded && subnet != null,
            ) { Text(stringResource(R.string.azure_image_confirm)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The helper's virtual network, its subnet and an optional security group; it gets a public IP. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun HelperNetwork(
    options: AzureOptions,
    networkId: String,
    subnetId: String,
    nsgId: String,
    onNetwork: (String) -> Unit,
    onSubnet: (String) -> Unit,
    onNsg: (String) -> Unit,
) {
    Text(stringResource(R.string.azure_network), style = MaterialTheme.typography.labelLarge)
    MutedText(stringResource(R.string.azure_image_network_hint))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.networks.forEach { n ->
            IchorFilterChip(selected = n.id == networkId, onClick = { onNetwork(n.id) }, label = { Text("${n.name} · ${n.resourceGroup}") })
        }
    }
    if (networkId.isBlank()) return
    Text(stringResource(R.string.azure_subnet), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.subnetsIn(networkId).forEach { s ->
            IchorFilterChip(selected = s.id == subnetId, onClick = { onSubnet(s.id) }, label = { Text(subnetLabel(s)) })
        }
    }
    if (subnetId.isBlank()) return
    Text(stringResource(R.string.azure_nsg), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.securityGroups.forEach { g ->
            IchorFilterChip(selected = g.id == nsgId, onClick = { onNsg(g.id) }, label = { Text("${g.name} · ${g.resourceGroup}") })
        }
    }
    if (nsgId.isBlank()) MutedText(stringResource(R.string.azure_nsg_none))
}

/** "About 10 minutes of Standard_B2s (0.0416 USD/h), then the image's storage…", or the same without the price. */
@Composable
private fun costLine(cost: AzureImageEstimate): String =
    if (cost.hourly.isBlank()) {
        stringResource(R.string.azure_image_cost_unknown, cost.minutes, cost.size)
    } else {
        stringResource(R.string.azure_image_cost, cost.minutes, cost.size, "${cost.hourly} ${cost.currency}")
    }
