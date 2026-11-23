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
import name.levis.ichor.model.ScalewayNetwork
import name.levis.ichor.model.ScalewayNetworkRule
import name.levis.ichor.model.ScalewayNetworkSpec
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.uiStateOf

/**
 * "Make a security group for the nodes" or "Pick mine", above the security group picker. The
 * first makes (or reuses) the ichor-nodes group in [zone], then [onReady] selects it; the rules it
 * now has are listed so the person can check them before creating. Nothing runs until the person
 * taps it: it writes at Scaleway.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ScalewayGuidedNetwork(
    zone: String,
    omni: Boolean,
    /** The new member's role; "" on Omni, where it is a worker until allocated. */
    role: String,
    enabled: Boolean,
    prepare: suspend (ScalewayNetworkSpec) -> ScalewayNetwork,
    onReady: (ScalewayNetwork) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var guided by rememberSaveable(zone) { mutableStateOf(false) }
    var state by remember(zone) { mutableStateOf<UiState<ScalewayNetwork>?>(null) }

    fun run() {
        state = UiState.Loading
        scope.launch {
            val spec = ScalewayNetworkSpec(zone = zone, role = role.ifBlank { "worker" }, omni = omni)
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
            label = { Text(stringResource(R.string.scaleway_network_guided)) },
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
    MutedText(stringResource(R.string.scaleway_network_hint))
    when (val current = state) {
        null -> Unit
        UiState.Loading -> {
            MutedText(stringResource(R.string.aws_network_working))
            LoadingBox()
        }
        // The refusal (no member with a public address, a key without the right) says what to do; the picker below still works.
        is UiState.Failed -> ErrorBox(current.message, ::run)
        is UiState.Loaded -> ScalewayNetworkRules(current.data)
    }
}

@Composable
private fun ScalewayNetworkRules(network: ScalewayNetwork) {
    val group = network.name.ifBlank { network.securityGroupId }
    MutedText(stringResource(if (network.created) R.string.scaleway_network_created else R.string.aws_network_reused, group))
    network.rules.forEach { MutedText("• ${ruleLabel(it, network.phoneIp)}") }
    network.warnings.forEach { InfoNotice(it) }
}

@Composable
private fun ruleLabel(rule: ScalewayNetworkRule, phoneIp: String): String = when {
    rule.isPhone(phoneIp) -> stringResource(R.string.aws_network_rule_phone, phoneIp)
    rule.isMember -> stringResource(R.string.scaleway_network_rule_member, rule.description, rule.source)
    else -> rule.plain
}
