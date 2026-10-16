package name.levis.ichor.ui.tailscale

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.TailscaleConnector
import name.levis.ichor.model.TailscaleDNSConfig
import name.levis.ichor.model.TailscaleOperator
import name.levis.ichor.model.TailscaleProxyClass
import name.levis.ichor.model.TailscaleProxyGroup
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.MutedText

/**
 * One row: health dot and name, the details that matter, then (for a problem) the operator's
 * reason and message, shown in full so the row reads as expanded.
 */
@Composable
private fun RowShell(health: ServiceHealth, title: String, line: String, reason: String, message: String, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            HealthDot(health)
            Text(
                title,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }
        line.takeIf { it.isNotEmpty() }?.let { MutedText(it, maxLines = 3, overflow = TextOverflow.Ellipsis) }
        val problem = listOf(reason, message).filter { it.isNotEmpty() }.joinToString(": ")
        if (health.needsAttention && problem.isNotEmpty()) {
            Text(
                problem,
                style = MaterialTheme.typography.bodySmall,
                color = if (health == ServiceHealth.CRITICAL) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** "1 critical", in the header; red when there is any. */
@Composable
internal fun CountText(text: String, critical: Boolean) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        color = if (critical) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** "Operator tailscale/operator · 1/1 ready · tailscale/k8s-operator:v1.90.0". */
@Composable
internal fun OperatorLine(op: TailscaleOperator) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        HealthDot(op.serviceHealth)
        MutedText(
            listOf(
                stringResource(R.string.tailscale_operator, "${op.namespace}/${op.name}"),
                stringResource(R.string.tailscale_ready_of, "${op.ready}/${op.desired}"),
                op.image,
            ).filter { it.isNotEmpty() }.joinToString(" · "),
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** A subnet router, exit node or app connector: what it does, its tailnet IPs and devices. */
@Composable
fun ConnectorRow(c: TailscaleConnector, onClick: () -> Unit) {
    val routes = c.routes
    val line = listOfNotNull(
        c.hostname.takeIf { it.isNotEmpty() && it != c.name },
        routes.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.tailscale_subnet_router, it.joinToString(", ")) },
        stringResource(R.string.tailscale_exit_node).takeIf { c.exitNode },
        stringResource(R.string.tailscale_app_connector).takeIf { c.appConnector },
        c.appConnectorRoutes.takeIf { c.appConnector && it.isNotEmpty() }?.joinToString(", "),
        c.tailnetIPs.takeIf { it.isNotEmpty() }?.joinToString(", "),
        stringResource(R.string.tailscale_devices, c.devices.size),
        c.proxyClass.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.tailscale_proxy_class, it) },
    ).joinToString(" · ")
    RowShell(c.serviceHealth, c.name, line, c.reason, c.message, onClick)
}

/** An egress, ingress or kube-apiserver proxy pool: its type, size, devices and URL. */
@Composable
fun ProxyGroupRow(g: TailscaleProxyGroup, onClick: () -> Unit) {
    val line = listOfNotNull(
        g.type.takeIf { it.isNotEmpty() },
        stringResource(R.string.tailscale_replicas, g.replicas),
        stringResource(R.string.tailscale_devices, g.devices.size),
        g.url.takeIf { it.isNotEmpty() },
        g.proxyClass.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.tailscale_proxy_class, it) },
        readiness(g.ready),
        stringResource(R.string.tailscale_some_running).takeIf { !g.ready && g.available },
    ).joinToString(" · ")
    RowShell(g.serviceHealth, g.name, line, g.reason, g.message, onClick)
}

/** A ProxyClass: whether it validates, and how many connectors and proxy groups name it. */
@Composable
fun ProxyClassRow(pc: TailscaleProxyClass, onClick: () -> Unit) {
    val line = listOf(readiness(pc.ready), stringResource(R.string.tailscale_used_by, pc.usedBy)).joinToString(" · ")
    RowShell(pc.serviceHealth, pc.name, line, pc.reason, pc.message, onClick)
}

/** The DNSConfig: the nameserver that resolves tailnet names in the cluster. */
@Composable
fun DNSConfigRow(d: TailscaleDNSConfig, onClick: () -> Unit) {
    val line = listOfNotNull(
        d.nameserverIP.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.tailscale_nameserver, it) },
        readiness(d.ready),
    ).joinToString(" · ")
    RowShell(d.serviceHealth, d.name, line, d.reason, d.message, onClick)
}

@Composable
private fun readiness(ready: Boolean): String =
    stringResource(if (ready) R.string.common_status_ready else R.string.common_status_not_ready)
