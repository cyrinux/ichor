package name.levis.ichor.ui.nettools

import android.content.Context
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NetDnsResult
import name.levis.ichor.model.NetHttpResult
import name.levis.ichor.model.NetPingResult
import name.levis.ichor.model.NetPortResult
import name.levis.ichor.model.NetToolResult
import name.levis.ichor.model.NetTraceHop
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.theme.LocalStatusColors

/** Below this many days left, a certificate's expiry is a warning. */
private const val CERT_WARN_DAYS = 14

/** A check's result as fields: only what the tool's section holds. */
@Composable
fun NetToolResultCard(result: NetToolResult) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            StatusPill(
                stringResource(if (result.ok) R.string.net_tools_ok else R.string.net_tools_not_ok),
                if (result.ok) colors.ok else colors.bad,
            )
            result.dns?.let { Dns(it) }
            result.ping?.let { Ping(it) }
            result.port?.let { Port(it) }
            result.trace?.let { Trace(it) }
            result.http?.let { Http(it) }
        }
    }
}

@Composable
private fun Dns(dns: NetDnsResult) {
    InfoRow(stringResource(R.string.net_tools_status), dns.status.ifEmpty { "—" }, mono = true)
    InfoRow(stringResource(R.string.net_tools_resolver), dns.server.ifEmpty { "—" }, mono = true)
    InfoRow(stringResource(R.string.net_tools_query_time), "${dns.queryMs} ms")
    SectionTitle(stringResource(R.string.net_tools_records, dns.records.size))
    if (dns.records.isEmpty()) MutedText(stringResource(R.string.net_tools_no_records))
    dns.records.forEach { r ->
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(r.type, style = MaterialTheme.typography.labelLarge)
            Text(r.value, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.net_tools_ttl, r.ttl), style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun Ping(ping: NetPingResult) {
    InfoRow(stringResource(R.string.net_tools_received), "${ping.received} / ${ping.sent}")
    InfoRow(stringResource(R.string.net_tools_loss), "${ping.lossPct.format()} %", valueColor = if (ping.lossPct > 0) LocalStatusColors.current.warn else LocalStatusColors.current.ok)
    if (ping.received > 0) InfoRow(stringResource(R.string.net_tools_rtt), "${ping.minMs.format()} / ${ping.avgMs.format()} / ${ping.maxMs.format()} ms")
}

@Composable
private fun Port(port: NetPortResult) {
    val colors = LocalStatusColors.current
    Text(
        stringResource(if (port.open) R.string.net_tools_port_open else R.string.net_tools_port_closed),
        color = if (port.open) colors.ok else colors.bad,
        style = MaterialTheme.typography.titleMedium,
    )
    if (port.message.isNotEmpty()) MutedText(port.message)
}

@Composable
private fun Trace(hops: List<NetTraceHop>) {
    SectionTitle(stringResource(R.string.net_tools_hops, hops.size))
    hops.forEach { h ->
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("${h.hop}.", style = MaterialTheme.typography.labelLarge)
            Text(
                if (h.silent) stringResource(R.string.net_tools_hop_silent) else h.host,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.weight(1f),
                color = if (h.silent) LocalStatusColors.current.muted else MaterialTheme.colorScheme.onSurface,
            )
            if (!h.silent) Text("${h.avgMs.format()} ms · ${h.lossPct.format()} %", style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun Http(http: NetHttpResult) {
    val colors = LocalStatusColors.current
    InfoRow(stringResource(R.string.net_tools_http_status), if (http.status == 0) "—" else "${http.status}")
    InfoRow(stringResource(R.string.net_tools_total_time), "${http.totalMs.format()} ms")
    if (http.redirectUrl.isNotEmpty()) InfoRow(stringResource(R.string.net_tools_redirect), http.redirectUrl, mono = true)
    http.error?.takeIf { it.isNotEmpty() }?.let { Text(it, color = colors.bad, style = MaterialTheme.typography.bodySmall) }
    if (http.tls) {
        SectionTitle(stringResource(R.string.net_tools_certificate))
        InfoRow(
            stringResource(R.string.net_tools_tls_verified),
            stringResource(if (http.tlsOk) R.string.net_tools_tls_valid else R.string.net_tools_tls_invalid),
            valueColor = if (http.tlsOk) colors.ok else colors.bad,
        )
        if (http.subject.isNotEmpty()) InfoRow(stringResource(R.string.net_tools_subject), http.subject, mono = true)
        if (http.issuer.isNotEmpty()) InfoRow(stringResource(R.string.net_tools_issuer), http.issuer, mono = true)
        if (http.notAfter.isNotEmpty()) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.net_tools_expires, http.notAfter.take(10)), style = MaterialTheme.typography.bodyMedium)
                StatusPill(
                    stringResource(R.string.net_tools_days_left, http.daysLeft),
                    when {
                        http.daysLeft < 0 -> colors.bad
                        http.daysLeft < CERT_WARN_DAYS -> colors.warn
                        else -> colors.ok
                    },
                )
            }
        }
    }
}

/** The full output, folded: the escape hatch when the fields are not enough. */
@Composable
fun RawOutput(raw: String) {
    if (raw.isBlank()) return
    var open by rememberSaveable { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().expandable(open) { open = !open }.padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(stringResource(R.string.net_tools_raw), style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
        Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
    }
    if (open) {
        Text(
            raw,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
            softWrap = false,
        )
    }
}

private fun Double.format(): String = if (this == Math.floor(this)) "%.0f".format(this) else "%.1f".format(this)

/** A result as plain text, to paste in a chat or a ticket: the fields, then the raw output. */
fun netToolShareText(context: Context, hostname: String, result: NetToolResult): String = buildString {
    appendLine(context.getString(R.string.net_tools_share_title, result.tool.uppercase(), result.target, hostname))
    appendLine(context.getString(if (result.ok) R.string.net_tools_ok else R.string.net_tools_not_ok))
    result.dns?.let { d ->
        appendLine("${d.status} · ${d.server} · ${d.queryMs} ms")
        d.records.forEach { appendLine("${it.name} ${it.ttl} ${it.type} ${it.value}") }
    }
    result.ping?.let { p -> appendLine("${p.received}/${p.sent}, ${p.lossPct.format()} % loss, ${p.minMs.format()}/${p.avgMs.format()}/${p.maxMs.format()} ms") }
    result.port?.let { p -> appendLine(context.getString(if (p.open) R.string.net_tools_port_open else R.string.net_tools_port_closed) + " · " + p.message) }
    result.trace?.forEach { h -> appendLine("${h.hop}. ${h.host} ${h.avgMs.format()} ms ${h.lossPct.format()} %") }
    result.http?.let { h ->
        appendLine("HTTP ${h.status} · ${h.totalMs.format()} ms")
        if (h.tls) appendLine("${h.subject} · ${h.issuer} · ${h.notAfter} (${context.getString(R.string.net_tools_days_left, h.daysLeft)})")
    }
    if (result.raw.isNotBlank()) {
        appendLine()
        append(result.raw.trim())
    }
}.trim()
