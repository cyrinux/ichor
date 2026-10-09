package name.levis.ichor.ui.alerts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.MenuBook
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AmAlert
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.objectLinks
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDateTime
import name.levis.ichor.util.timeAgo

/** Annotations the sheet already shows on their own. */
private val SHOWN_ANNOTATIONS = setOf("summary", "description", "message", "runbook_url", "runbook")

/** Where an alert's labels lead in the app: a pod, a namespace, a node. */
class AlertObjectLinks(
    val onPod: (namespace: String, pod: String) -> Unit,
    val onNamespace: (String) -> Unit,
    val onNode: (String) -> Unit,
)

/**
 * One alert: its summary and description, when it started, its receivers and state, its
 * labels and other annotations, the runbook and the rule's graph, the pod, namespace or node
 * it is about ([nodes]: the cluster's node names and addresses), and "Silence".
 * [silenceDenial]: why the credentials cannot silence, if known.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun AlertSheet(
    alert: AmAlert,
    nodes: Set<String>,
    links: AlertObjectLinks,
    silenceDenial: KubePermission?,
    onSilence: () -> Unit,
    onDismiss: () -> Unit,
) {
    val uri = LocalUriHandler.current
    val objects = remember(alert, nodes) { alert.objectLinks(nodes) }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                SeverityDot(alert.severity)
                Text(alert.alertname, style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(start = 10.dp).weight(1f))
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                StatusPill(severityLabel(alert.severity), severityColor(alert.severity))
                StatusPill(stateLabel(alert), if (alert.suppressed) LocalStatusColors.current.muted else severityColor(alert.severity))
            }
            if (alert.summary.isNotEmpty()) Text(alert.summary, style = MaterialTheme.typography.bodyLarge)
            if (alert.description.isNotEmpty()) MutedText(alert.description)
            if (alert.startsAt > 0) {
                InfoRow(stringResource(R.string.alerts_started), "${timeAgo(alert.startsAt)} · ${formatDateTime(alert.startsAt)}")
            }
            if (alert.receivers.isNotEmpty()) InfoRow(stringResource(R.string.alerts_receivers), alert.receivers.joinToString(", "))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (alert.runbookURL.isNotEmpty()) {
                    LinkButton(Icons.AutoMirrored.Outlined.MenuBook, stringResource(R.string.alerts_runbook)) { runCatching { uri.openUri(alert.runbookURL) } }
                }
                if (alert.generatorURL.isNotEmpty()) {
                    LinkButton(Icons.AutoMirrored.Outlined.OpenInNew, stringResource(R.string.alerts_generator)) { runCatching { uri.openUri(alert.generatorURL) } }
                }
                objects.pod?.let { pod ->
                    LinkButton(Icons.Outlined.Widgets, stringResource(R.string.alerts_open_pod)) { links.onPod(objects.namespace.orEmpty(), pod) }
                }
                objects.namespace?.let { ns ->
                    LinkButton(Icons.Outlined.Folder, stringResource(R.string.alerts_open_namespace, ns)) { links.onNamespace(ns) }
                }
                objects.node?.let { node ->
                    LinkButton(Icons.Outlined.Dns, stringResource(R.string.alerts_open_node, node)) { links.onNode(node) }
                }
            }
            Button(onClick = onSilence, enabled = silenceDenial == null, modifier = Modifier.fillMaxWidth().padding(top = 4.dp)) {
                Icon(Icons.Outlined.NotificationsOff, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.alerts_silence), modifier = Modifier.padding(start = 8.dp))
            }
            KubeDenialNote(silenceDenial)
            SectionTitle(stringResource(R.string.alerts_labels))
            alert.labels.toSortedMap().forEach { (name, value) -> InfoRow(name, value, mono = true) }
            val others = alert.annotations.filterKeys { it !in SHOWN_ANNOTATIONS }
            if (others.isNotEmpty()) {
                SectionTitle(stringResource(R.string.alerts_annotations))
                others.toSortedMap().forEach { (name, value) -> InfoRow(name, value) }
            }
        }
    }
}

@Composable
private fun LinkButton(icon: ImageVector, label: String, onClick: () -> Unit) {
    OutlinedButton(onClick = onClick) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(label, modifier = Modifier.padding(start = 6.dp))
    }
}
