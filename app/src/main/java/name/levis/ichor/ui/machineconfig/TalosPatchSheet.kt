package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Science
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.TalosPatchInfo
import name.levis.ichor.model.patchNode
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/**
 * A repository file that is a Talos patch: "Try on a node…" when it can be tried, else why not
 * (a secret, a whole config, a talenv placeholder).
 */
@Composable
fun TalosPatchCard(info: TalosPatchInfo, onTry: () -> Unit, modifier: Modifier = Modifier) {
    SectionCard(
        stringResource(R.string.talos_patch_title),
        modifier,
        leading = { Icon(Icons.Outlined.Science, contentDescription = null) },
    ) {
        if (info.tryable) {
            MutedText(stringResource(R.string.talos_patch_hint))
            IchorOutlinedButton(onClick = onTry, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.talos_patch_try)) }
        } else {
            MutedText(stringResource(R.string.talos_patch_cannot, info.note))
        }
    }
}

/**
 * The node to try the patch [info] on: the cluster's reachable nodes, the one the patch sets
 * the hostname of picked first, with a warning when another one is picked. The review then
 * offers other nodes too when the patch only sets values.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TalosPatchNodeSheet(talos: TalosRepository, info: TalosPatchInfo, onPick: (NodeOverview) -> Unit, onDismiss: () -> Unit) {
    var nodes by remember { mutableStateOf<UiState<List<NodeOverview>>>(UiState.Loading) }
    var picked by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        nodes = uiStateOf { (talos.cached<ClusterOverview>(OVERVIEW)?.value ?: talos.overview()).nodes.filter { it.reachable } }
        picked = (nodes as? UiState.Loaded)?.data?.let { list -> (patchNode(info, list) ?: list.singleOrNull())?.node }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.talos_patch_pick_title), style = MaterialTheme.typography.titleLarge)
            when (val n = nodes) {
                UiState.Loading -> CircularProgressIndicator(Modifier.padding(vertical = 24.dp))
                is UiState.Failed -> InlineError(n.message.asString())
                is UiState.Loaded -> {
                    val meant = patchNode(info, n.data)
                    if (n.data.isEmpty()) MutedText(stringResource(R.string.talos_patch_no_nodes))
                    n.data.forEach { node ->
                        NodeChoice(node, selected = node.node == picked, onSelect = { picked = node.node })
                    }
                    val hostnames = info.hostnames
                    if (hostnames.isNotEmpty() && picked != null && picked != meant?.node) {
                        Text(
                            stringResource(R.string.talos_patch_other_node, hostnames.joinToString()),
                            color = LocalStatusColors.current.warn,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    MutedText(stringResource(R.string.talos_patch_pick_hint))
                    IchorButton(
                        onClick = { n.data.firstOrNull { it.node == picked }?.let(onPick) },
                        enabled = picked != null,
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text(stringResource(R.string.talos_patch_review)) }
                }
            }
        }
    }
}

@Composable
private fun NodeChoice(node: NodeOverview, selected: Boolean, onSelect: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().selectable(selected = selected, onClick = onSelect, role = Role.RadioButton).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        RadioButton(selected = selected, onClick = null)
        Column(Modifier.weight(1f)) {
            Text(node.hostname.ifEmpty { node.node }, style = MaterialTheme.typography.bodyLarge)
            MutedText("${node.node} · ${node.role}")
        }
    }
}
