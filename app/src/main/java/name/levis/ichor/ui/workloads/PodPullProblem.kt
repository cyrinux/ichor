package name.levis.ichor.ui.workloads

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.PullKind
import name.levis.ichor.model.PullProblem
import name.levis.ichor.model.hint
import name.levis.ichor.model.hintArg
import name.levis.ichor.model.title
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * Where a pull problem's hint leads, on a Talos cluster: [talosNode] is the Talos node of a
 * Kubernetes node name (null when none), [onNetTools] opens its network tools, and
 * [onMirrorSnippet] its machine config on the registry mirror snippet for a registry.
 */
class PodPullLinks(
    val talosNode: (String) -> NodeOverview?,
    val onNetTools: (NodeOverview) -> Unit,
    val onMirrorSnippet: (node: NodeOverview, registry: String) -> Unit,
)

/** The pull problem's one line, in plain words. */
@Composable
private fun PullProblem.titleText(): String {
    val title = pullKind.title()
    return if (title.takesRegistry) stringResource(title.res, registry) else stringResource(title.res)
}

/**
 * Why [pod]'s image cannot be pulled, as a line under its status (nothing when it can); a tap
 * shows the kubelet's message, what to do, and the links of [links] that fit.
 */
@Composable
internal fun PodPullLine(pod: KubePod, links: PodPullLinks?) {
    val pull = pod.pull ?: return
    var open by rememberSaveable(pod.key) { mutableStateOf(false) }
    val title = pull.titleText()
    Text(
        title,
        style = MaterialTheme.typography.labelSmall,
        color = LocalStatusColors.current.warn,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.clickable(onClickLabel = stringResource(R.string.pod_pull_message_title), role = Role.Button) { open = true },
    )
    if (open) PullProblemDialog(pod, pull, title, links, onDismiss = { open = false })
}

@Composable
private fun PullProblemDialog(pod: KubePod, pull: PullProblem, title: String, links: PodPullLinks?, onDismiss: () -> Unit) {
    val node = links?.let { pod.node.takeIf { n -> n.isNotEmpty() }?.let(it.talosNode) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.pod_pull_message_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(title, style = MaterialTheme.typography.bodyLarge)
                val hintArg = pull.hintArg()
                Text(if (hintArg != null) stringResource(pull.hint(), hintArg) else stringResource(pull.hint()), style = MaterialTheme.typography.bodyMedium)
                if (pod.waiting.isNotEmpty()) {
                    SelectionContainer {
                        Text(pod.waiting, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                    }
                }
                if (links != null && node != null) PullLinks(pull, node, links, onDismiss)
            }
        },
        confirmButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) } },
    )
}

@Composable
private fun PullLinks(pull: PullProblem, node: NodeOverview, links: PodPullLinks, onDismiss: () -> Unit) {
    when (pull.pullKind) {
        PullKind.RATE_LIMITED -> IchorTextButton(onClick = {
            onDismiss()
            links.onMirrorSnippet(node, pull.registry)
        }) { Text(stringResource(R.string.pod_pull_open_mirror_snippet)) }
        PullKind.NETWORK -> IchorTextButton(onClick = {
            onDismiss()
            links.onNetTools(node)
        }) { Text(stringResource(R.string.pod_pull_open_net_tools, node.hostname.ifEmpty { node.node })) }
        else -> Unit
    }
}
