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
import name.levis.ichor.model.GCPNetworkResult
import name.levis.ichor.model.GCPNetworkRule
import name.levis.ichor.model.GCPNetworkSpec
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.uiStateOf

/**
 * "Use a network Ichor sets up" or "Pick mine", above the network and subnet pickers. The first
 * readies the default network of [project] (its subnet of [zone]'s region and the firewall rules
 * aimed at the ichor-node tag), then [onReady] fills the pickers; the rules it now has are listed.
 * Nothing runs until the person taps it: it writes at Google Cloud.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun GCPGuidedNetwork(
    project: String,
    zone: String,
    omni: Boolean,
    /** The new member's role; "" on Omni, where it is a worker until allocated. */
    role: String,
    enabled: Boolean,
    prepare: suspend (GCPNetworkSpec) -> GCPNetworkResult,
    onReady: (GCPNetworkResult) -> Unit,
) {
    val scope = rememberCoroutineScope()
    var guided by rememberSaveable(project, zone) { mutableStateOf(false) }
    var state by remember(project, zone) { mutableStateOf<UiState<GCPNetworkResult>?>(null) }

    fun run() {
        state = UiState.Loading
        scope.launch {
            val spec = GCPNetworkSpec(project = project, zone = zone, role = role.ifBlank { "worker" }, omni = omni)
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
    MutedText(stringResource(R.string.gcp_network_hint))
    when (val current = state) {
        null -> Unit
        UiState.Loading -> {
            MutedText(stringResource(R.string.aws_network_working))
            LoadingBox()
        }
        // The refusal (no default network) says what to do at Google Cloud; the pickers below still work.
        is UiState.Failed -> ErrorBox(current.message, ::run)
        is UiState.Loaded -> GCPNetworkRules(current.data)
    }
}

@Composable
private fun GCPNetworkRules(network: GCPNetworkResult) {
    val added = network.created + network.updated
    MutedText(
        if (added.isEmpty()) stringResource(R.string.gcp_network_reused) else stringResource(R.string.gcp_network_created, added.joinToString(", ")),
    )
    network.rules.forEach { MutedText("• ${gcpRuleLabel(it, network.phoneIp)}") }
    network.warnings.forEach { InfoNotice(it) }
}

@Composable
private fun gcpRuleLabel(rule: GCPNetworkRule, phoneIp: String): String = when {
    rule.self -> stringResource(R.string.gcp_network_rule_self)
    rule.isPhone(phoneIp) -> stringResource(R.string.aws_network_rule_phone, phoneIp)
    else -> rule.plain
}
