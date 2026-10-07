package name.levis.ichor.ui.netpol

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Block
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Lan
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.Isolation
import name.levis.ichor.model.NETPEER_CIDR
import name.levis.ichor.model.NETPEER_ENTITY
import name.levis.ichor.model.NETPEER_FQDN
import name.levis.ichor.model.NETPEER_NAMESPACES
import name.levis.ichor.model.NETPEER_NODES
import name.levis.ichor.model.NETPEER_NONE
import name.levis.ichor.model.NETPEER_OTHER
import name.levis.ichor.model.NETPEER_PODS
import name.levis.ichor.model.NETPEER_SERVICE
import name.levis.ichor.model.NETRULE_LOG
import name.levis.ichor.model.NETRULE_PASS
import name.levis.ichor.model.NetPeer
import name.levis.ichor.model.NetPolicy
import name.levis.ichor.model.NetRule
import name.levis.ichor.model.label
import name.levis.ichor.ui.theme.LocalStatusColors

/** A small tinted label: a policy kind, a direction, a verdict. */
@Composable
fun TagBadge(label: String, color: Color, modifier: Modifier = Modifier, mono: Boolean = false) {
    Surface(modifier, shape = RoundedCornerShape(6.dp), color = color.copy(alpha = 0.14f), contentColor = color) {
        Text(
            label,
            style = MaterialTheme.typography.labelSmall,
            fontFamily = if (mono) FontFamily.Monospace else null,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
        )
    }
}

/** NP, CNP or CCNP. */
@Composable
fun KindBadge(policy: NetPolicy, modifier: Modifier = Modifier) =
    TagBadge(policy.kindShort, MaterialTheme.colorScheme.primary, modifier, mono = true)

/** Green when every pod is isolated, amber for some, muted for none. */
@Composable
fun Isolation.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        Isolation.FULL -> colors.ok
        Isolation.PARTIAL -> colors.warn
        Isolation.NONE -> colors.muted
    }
}

/** "app=db", "All pods" or the node selector of a host policy. */
@Composable
fun subjectText(policy: NetPolicy): String = when {
    policy.nodes -> policy.subject.ifEmpty { stringResource(R.string.netpol_all_nodes) }
    else -> policy.subject.ifEmpty { stringResource(R.string.netpol_all_pods) }
}

private val NetPeer.icon: ImageVector
    get() = when (kind) {
        NETPEER_PODS -> Icons.Outlined.ViewInAr
        NETPEER_NAMESPACES -> Icons.Outlined.Folder
        NETPEER_CIDR -> Icons.Outlined.Lan
        NETPEER_ENTITY -> if (value == "world") Icons.Outlined.Public else Icons.Outlined.Category
        NETPEER_FQDN -> Icons.Outlined.Language
        NETPEER_SERVICE -> Icons.Outlined.Hub
        NETPEER_NODES -> Icons.Outlined.Dns
        NETPEER_NONE -> Icons.Outlined.Block
        else -> Icons.Outlined.Category
    }

/** What a peer stands for, in words: "app=db in media", "All namespaces", "10.0.0.0/8 except …". */
@Composable
private fun peerText(peer: NetPeer, policy: NetPolicy): String {
    val all = stringResource(R.string.netpol_all_pods)
    return when (peer.kind) {
        NETPEER_PODS -> {
            val pods = peer.selector.ifEmpty { all }
            when {
                peer.namespaceSelector.isNotEmpty() -> stringResource(R.string.netpol_peer_in, pods, stringResource(R.string.netpol_peer_namespaces, peer.namespaceSelector))
                peer.namespace == "*" || (peer.namespace.isEmpty() && policy.clusterWide) ->
                    stringResource(R.string.netpol_peer_in, pods, stringResource(R.string.netpol_peer_any_namespace))
                peer.namespace.isNotEmpty() -> stringResource(R.string.netpol_peer_in, pods, peer.namespace)
                else -> pods
            }
        }
        NETPEER_NAMESPACES -> if (peer.namespaceSelector.isEmpty()) {
            stringResource(R.string.workloads_all_namespaces)
        } else {
            stringResource(R.string.netpol_peer_namespaces, peer.namespaceSelector)
        }
        NETPEER_CIDR -> if (peer.except.isEmpty()) peer.value else stringResource(R.string.netpol_peer_except, peer.value, peer.except.joinToString(", "))
        NETPEER_SERVICE -> stringResource(
            R.string.netpol_peer_service,
            peer.value.ifEmpty { listOf(peer.namespace, peer.selector).filter { it.isNotEmpty() }.joinToString(" ") },
        )
        NETPEER_NODES -> peer.selector.ifEmpty { stringResource(R.string.netpol_all_nodes) }
        NETPEER_NONE -> stringResource(R.string.netpol_peer_none)
        NETPEER_OTHER -> listOf(peer.value, peer.selector).filter { it.isNotEmpty() }.joinToString(" ")
        else -> peer.value.ifEmpty { peer.kind }
    }
}

@Composable
private fun PeerChip(peer: NetPeer, policy: NetPolicy) {
    Surface(shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.secondaryContainer) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(peer.icon, contentDescription = peer.kind, modifier = Modifier.size(14.dp))
            Text(
                peerText(peer, policy),
                style = MaterialTheme.typography.labelMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 4.dp),
            )
        }
    }
}

/** One rule: Allow/Deny, the peers it is "From" or "To", its ports and L7 filters. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun RuleCard(rule: NetRule, ingress: Boolean, policy: NetPolicy) {
    val colors = LocalStatusColors.current
    OutlinedCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                when {
                    rule.deny -> TagBadge(stringResource(R.string.netpol_deny), colors.bad)
                    rule.action == NETRULE_PASS -> TagBadge(stringResource(R.string.netpol_pass), colors.warn)
                    rule.action == NETRULE_LOG -> TagBadge(stringResource(R.string.netpol_log), colors.muted)
                    else -> TagBadge(stringResource(R.string.netpol_allow), colors.ok)
                }
                Text(
                    stringResource(if (ingress) R.string.netpol_from else R.string.netpol_to),
                    style = MaterialTheme.typography.labelLarge,
                )
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (rule.peers.isEmpty()) {
                    Text(stringResource(R.string.netpol_any_peer), style = MaterialTheme.typography.bodyMedium)
                }
                rule.peers.forEach { PeerChip(it, policy) }
            }
            val ports = rule.ports.mapNotNull { it.label }
            Text(
                ports.joinToString("  ·  ").ifEmpty { stringResource(R.string.netpol_any_port) },
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
            )
            rule.l7.forEach { line ->
                Text(line, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
