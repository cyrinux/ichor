package name.levis.ichor.ui.flows

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.DropGroup
import name.levis.ichor.model.HUBBLE_AUDIT
import name.levis.ichor.model.HUBBLE_EGRESS
import name.levis.ichor.model.HUBBLE_INGRESS
import name.levis.ichor.model.HubbleFlow
import name.levis.ichor.model.HubblePeer
import name.levis.ichor.model.PolicyRef
import name.levis.ichor.model.find
import name.levis.ichor.model.label
import name.levis.ichor.model.portLabel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.netpol.PolicyDetailSheet
import name.levis.ichor.ui.netpol.TagBadge
import java.text.DateFormat
import java.util.Date

/**
 * One drop group: both endpoints, the policies that denied it or isolate its pod (each opens
 * its rules, read on the first tap), what to change, and the raw flow to copy.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DropDetailSheet(group: DropGroup, vm: FlowsViewModel, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val policies by vm.policies.collectAsStateWithLifecycle()
    var opening by remember { mutableStateOf<PolicyRef?>(null) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text("${group.source.label} → ${group.destination.label}", style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                TagBadge(
                    stringResource(if (group.verdict == HUBBLE_AUDIT) R.string.flows_would_drop else R.string.flows_verdict_dropped),
                    verdictColor(group.verdict),
                )
                directionText(group.direction)?.let { TagBadge(it, MaterialTheme.colorScheme.outline) }
                Text(stringResource(R.string.flows_count, group.count), style = MaterialTheme.typography.labelLarge)
            }
            if (group.reason.isNotEmpty()) Text(reasonText(group.reason), style = MaterialTheme.typography.bodyMedium)
            InfoRow(stringResource(R.string.flows_port), portLabel(group.protocol, group.port).ifEmpty { stringResource(R.string.netpol_any_port) }, mono = true)
            InfoRow(stringResource(R.string.flows_first_seen), formatTime(group.firstSeen))
            InfoRow(stringResource(R.string.flows_last_seen_label), formatTime(group.lastSeen))
            if (group.nodes.isNotEmpty()) InfoRow(stringResource(R.string.flows_nodes), group.nodes.joinToString(", "), mono = true)

            SectionTitle(stringResource(R.string.flows_source))
            PeerDetails(group.source)
            SectionTitle(stringResource(R.string.flows_destination))
            PeerDetails(group.destination)

            Policies(stringResource(R.string.flows_denied_by), group.deniedBy) { opening = it }
            Policies(stringResource(R.string.flows_isolated_by), group.isolating) { opening = it }
            when (val p = policies) {
                UiState.Loading -> if (opening != null) CircularProgressIndicator(Modifier.padding(8.dp).size(20.dp), strokeWidth = 2.dp)
                is UiState.Failed -> if (opening != null) InlineError(p.message.asString())
                else -> {}
            }
            Hint(group)

            OutlinedButton(onClick = { copyFlow(context, group.sample) }, modifier = Modifier.padding(top = 8.dp)) {
                Icon(Icons.Outlined.ContentCopy, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.flows_copy_json), modifier = Modifier.padding(start = 8.dp))
            }
        }
    }

    opening?.let { ref ->
        LaunchedEffect(ref) { vm.loadPolicies() }
        val report = (policies as? UiState.Loaded)?.data ?: return@let
        val policy = report.find(ref)
        if (policy != null) {
            PolicyDetailSheet(policy, onDismiss = { opening = null })
        } else {
            LaunchedEffect(ref) {
                Toast.makeText(context, context.getString(R.string.flows_policy_gone, ref.label), Toast.LENGTH_SHORT).show()
                opening = null
            }
        }
    }
}

@Composable
private fun PeerDetails(peer: HubblePeer) {
    if (peer.namespace.isNotEmpty()) InfoRow(stringResource(R.string.netpol_namespace), peer.namespace, mono = true)
    if (peer.pod.isNotEmpty()) InfoRow(stringResource(R.string.flows_peer_pod), peer.pod, mono = true)
    if (peer.workload.isNotEmpty()) InfoRow(stringResource(R.string.flows_peer_workload), peer.workload, mono = true)
    if (peer.reserved.isNotEmpty()) InfoRow(stringResource(R.string.flows_peer_entity), peer.reserved, mono = true)
    if (peer.ip.isNotEmpty()) InfoRow(stringResource(R.string.flows_peer_ip), peer.ip, mono = true)
    if (peer.names.isNotEmpty()) InfoRow(stringResource(R.string.flows_peer_names), peer.names.joinToString(", "), mono = true)
    if (peer.identity > 0) InfoRow(stringResource(R.string.flows_peer_identity), peer.identity.toString(), mono = true)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Policies(title: String, refs: List<PolicyRef>, onOpen: (PolicyRef) -> Unit) {
    if (refs.isEmpty()) return
    SectionTitle(title)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        refs.forEach { ref ->
            AssistChip(
                onClick = { onOpen(ref) },
                label = { Text(ref.label, fontFamily = FontFamily.Monospace) },
                leadingIcon = { Icon(Icons.Outlined.Policy, contentDescription = ref.kind, modifier = Modifier.size(18.dp)) },
            )
        }
    }
}

/** What to change: the deny rule to narrow, or the rule to add to an isolating policy. */
@Composable
private fun Hint(group: DropGroup) {
    val port = portLabel(group.protocol, group.port).ifEmpty { stringResource(R.string.netpol_any_port) }
    val hints = buildList {
        if (group.deniedBy.isNotEmpty()) add(stringResource(R.string.flows_hint_deny, group.deniedBy.joinToString(", ") { it.label }))
        if (group.isolating.isNotEmpty()) {
            when (group.direction) {
                HUBBLE_INGRESS -> add(stringResource(R.string.flows_hint_ingress, group.source.label, port))
                HUBBLE_EGRESS -> add(stringResource(R.string.flows_hint_egress, group.destination.label, port))
            }
        }
    }
    hints.forEach { MutedText(it, Modifier.padding(top = 8.dp)) }
}

private fun formatTime(millis: Long): String =
    if (millis > 0) DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.MEDIUM).format(Date(millis)) else "–"

private fun copyFlow(context: Context, flow: HubbleFlow) {
    val json = TalosJson.encodeToString(HubbleFlow.serializer(), flow)
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("flow", json))
    Toast.makeText(context, R.string.flows_copied, Toast.LENGTH_SHORT).show()
}
