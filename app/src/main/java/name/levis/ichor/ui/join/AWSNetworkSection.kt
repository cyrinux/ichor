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
import name.levis.ichor.model.AWSNetwork
import name.levis.ichor.model.AWSNetworkRule
import name.levis.ichor.model.AWSNetworkSpec
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.uiStateOf

/**
 * "Use a network Ichor sets up" or "Pick mine", above the subnet and group pickers. The first
 * makes (or reuses) the ichor-nodes group in the default VPC, then [onReady] fills the pickers;
 * the rules it now has are listed. Nothing runs until the person taps it: it writes at AWS.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun GuidedNetwork(
    region: String,
    omni: Boolean,
    /** The new member's role; "" on Omni, where it is a worker until allocated. */
    role: String,
    enabled: Boolean,
    prepare: suspend (AWSNetworkSpec) -> AWSNetwork,
    onReady: (AWSNetwork) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var guided by rememberSaveable(region) { mutableStateOf(false) }
    var state by remember(region) { mutableStateOf<UiState<AWSNetwork>?>(null) }

    fun run() {
        state = UiState.Loading
        scope.launch {
            val spec = AWSNetworkSpec(region = region, role = role.ifBlank { "worker" }, omni = omni)
            val result = uiStateOf { prepare(spec) }
            state = result
            if (result is UiState.Loaded) onReady(result.data)
        }
    }

    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorFilterChip(
            selected = guided,
            enabled = enabled && state != UiState.Loading,
            onClick = {
                guided = true
                run()
            },
            label = { Text(stringResource(R.string.aws_network_guided)) },
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
    MutedText(stringResource(R.string.aws_network_hint))
    when (val current = state) {
        null -> Unit
        UiState.Loading -> {
            MutedText(stringResource(R.string.aws_network_working))
            LoadingBox()
        }
        // The refusal (no default VPC) says what to do at AWS; the pickers below still work.
        is UiState.Failed -> ErrorBox(current.message, ::run)
        is UiState.Loaded -> NetworkRules(current.data)
    }
}

@Composable
private fun NetworkRules(network: AWSNetwork) {
    val group = network.securityGroupId
    MutedText(stringResource(if (network.created) R.string.aws_network_created else R.string.aws_network_reused, group))
    network.rules.forEach { MutedText("• ${ruleLabel(it, network.phoneIp)}") }
    network.warnings.forEach { InfoNotice(it) }
}

@Composable
private fun ruleLabel(rule: AWSNetworkRule, phoneIp: String): String = when {
    rule.self -> stringResource(R.string.aws_network_rule_self)
    rule.isPhone(phoneIp) -> stringResource(R.string.aws_network_rule_phone, phoneIp)
    else -> rule.plain
}
