package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.AzureNetworkResult
import name.levis.ichor.model.AzureNetworkSpec
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.uiStateOf

/**
 * "Make a network for the node" or "Pick mine", above the network pickers. The first makes (or
 * reuses) the ichor-nodes security group and virtual network in [resourceGroup], then [onReady]
 * fills the pickers. Nothing runs until the person taps it: it writes at Azure. Offered first
 * when the location has no network yet.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun AzureGuidedNetwork(
    resourceGroup: String,
    location: String,
    omni: Boolean,
    /** The location has no virtual network: the guided one is what to use. */
    noNetwork: Boolean,
    enabled: Boolean,
    prepare: suspend (AzureNetworkSpec) -> AzureNetworkResult,
    onReady: (AzureNetworkResult) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var guided by rememberSaveable(resourceGroup, location) { mutableStateOf(false) }
    var state by remember(resourceGroup, location) { mutableStateOf<UiState<AzureNetworkResult>?>(null) }

    fun run() {
        state = UiState.Loading
        scope.launch {
            val result = uiStateOf { prepare(AzureNetworkSpec(resourceGroup, location, omni)) }
            state = result
            if (result is UiState.Loaded) onReady(result.data)
        }
    }

    if (noNetwork && !guided) MutedText(stringResource(R.string.azure_network_none_yet))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorFilterChip(
            selected = guided,
            enabled = enabled && resourceGroup.isNotBlank() && state != UiState.Loading,
            onClick = {
                guided = true
                run()
            },
            label = { Text(stringResource(R.string.azure_network_guided)) },
        )
        IchorFilterChip(
            selected = !guided,
            onClick = {
                guided = false
                state = null
            },
            label = { Text(stringResource(R.string.aws_network_mine)) },
        )
    }
    if (!guided) return
    MutedText(stringResource(if (omni) R.string.azure_network_hint_omni else R.string.azure_network_hint))
    when (val current = state) {
        null -> Unit
        UiState.Loading -> {
            MutedText(stringResource(R.string.aws_network_working))
            LoadingBox()
        }
        // The refusal names the missing action; the pickers below still work.
        is UiState.Failed -> ErrorBox(current.message, ::run)
        is UiState.Loaded -> {
            val network = current.data
            MutedText(stringResource(if (network.created.isEmpty()) R.string.azure_network_reused else R.string.azure_network_created))
            if (network.phoneIp.isNotBlank()) MutedText("• ${stringResource(R.string.aws_network_rule_phone, network.phoneIp)}")
            network.warnings.forEach { InfoNotice(it) }
        }
    }
}
