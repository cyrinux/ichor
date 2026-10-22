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
import name.levis.ichor.ui.components.ResourceDetails
import name.levis.ichor.ui.components.ResourceRow

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
    ResourceRow(c.serviceHealth, c.name, Modifier.padding(horizontal = 16.dp), onClick = onClick) {
        ResourceDetails(line, c.serviceHealth, reasonMessage(c.reason, c.message))
    }
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
    ResourceRow(g.serviceHealth, g.name, Modifier.padding(horizontal = 16.dp), onClick = onClick) {
        ResourceDetails(line, g.serviceHealth, reasonMessage(g.reason, g.message))
    }
}

/** A ProxyClass: whether it validates, and how many connectors and proxy groups name it. */
@Composable
fun ProxyClassRow(pc: TailscaleProxyClass, onClick: () -> Unit) {
    val line = listOf(readiness(pc.ready), stringResource(R.string.tailscale_used_by, pc.usedBy)).joinToString(" · ")
    ResourceRow(pc.serviceHealth, pc.name, Modifier.padding(horizontal = 16.dp), onClick = onClick) {
        ResourceDetails(line, pc.serviceHealth, reasonMessage(pc.reason, pc.message))
    }
}

/** The DNSConfig: the nameserver that resolves tailnet names in the cluster. */
@Composable
fun DNSConfigRow(d: TailscaleDNSConfig, onClick: () -> Unit) {
    val line = listOfNotNull(
        d.nameserverIP.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.tailscale_nameserver, it) },
        readiness(d.ready),
    ).joinToString(" · ")
    ResourceRow(d.serviceHealth, d.name, Modifier.padding(horizontal = 16.dp), onClick = onClick) {
        ResourceDetails(line, d.serviceHealth, reasonMessage(d.reason, d.message))
    }
}

@Composable
private fun readiness(ready: Boolean): String =
    stringResource(if (ready) R.string.common_status_ready else R.string.common_status_not_ready)

/** A condition's reason and message, as the row's problem line. */
private fun reasonMessage(reason: String, message: String): String =
    listOf(reason, message).filter { it.isNotEmpty() }.joinToString(": ")
