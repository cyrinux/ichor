package name.levis.ichor.ui.loadbalancing

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
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
import name.levis.ichor.model.LbAdvertisement
import name.levis.ichor.model.LbBgpPeer
import name.levis.ichor.model.LbCause
import name.levis.ichor.model.LbConfigError
import name.levis.ichor.model.LbKubeVip
import name.levis.ichor.model.LbPool
import name.levis.ichor.model.LbService
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.kubebrowser.color

/** One sentence on why a Service waits for an address. */
fun lbCauseText(cause: LbCause): Int = when (cause) {
    LbCause.NO_CONTROLLER -> R.string.lb_cause_noController
    LbCause.CONFIG_ERROR -> R.string.lb_cause_configError
    LbCause.CONTROLLER_DOWN -> R.string.lb_cause_controllerDown
    LbCause.NO_POOL -> R.string.lb_cause_noPool
    LbCause.POOL_NOT_MATCHED -> R.string.lb_cause_poolNotMatched
    LbCause.POOL_EXHAUSTED -> R.string.lb_cause_poolExhausted
    LbCause.POOL_NOT_ADVERTISED -> R.string.lb_cause_poolNotAdvertised
    LbCause.UNKNOWN -> R.string.lb_cause_unknown
}

@Composable
internal fun WaitingCard(svc: LbService, modifier: Modifier, onClick: () -> Unit) {
    Card(modifier.fillMaxWidth().clickable(onClick = onClick)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.lb_waiting, svc.key), style = MaterialTheme.typography.titleSmall, color = svc.level.color())
            Text(stringResource(lbCauseText(LbCause.of(svc.cause))), style = MaterialTheme.typography.bodyMedium)
            val age = ageSince(svc.since, System.currentTimeMillis())
            if (age.isNotEmpty()) MutedText(stringResource(R.string.lb_waiting_since, age))
        }
    }
}

@Composable
internal fun ConfigErrorCard(e: LbConfigError, modifier: Modifier) {
    Card(modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            val who = if (e.node.isNotEmpty()) "${e.component} · ${e.node}" else e.component
            Text(stringResource(R.string.lb_config_error, who), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.error)
            if (e.message.isNotEmpty()) MutedText(e.message)
        }
    }
}

@Composable
private fun Mono(text: String, modifier: Modifier = Modifier) {
    Text(text, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = modifier)
}

@Composable
internal fun PoolRow(pool: LbPool, modifier: Modifier) {
    Column(modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Mono(pool.name, Modifier.weight(1f))
            Text(pool.controller, style = MaterialTheme.typography.labelMedium)
            Text(
                stringResource(R.string.lb_pool_free, pool.available.toString(), pool.total.toString()),
                style = MaterialTheme.typography.labelMedium,
                color = pool.level.color(),
            )
        }
        LinearProgressIndicator(progress = { pool.used }, modifier = Modifier.fillMaxWidth())
        MutedText(pool.addresses.joinToString(", "), maxLines = 2, overflow = TextOverflow.Ellipsis)
        val notes = buildList {
            if (pool.controller == "metallb") {
                add(
                    if (pool.advertisedBy.isEmpty()) stringResource(R.string.lb_pool_not_advertised)
                    else stringResource(R.string.lb_pool_advertised_by, pool.advertisedBy.joinToString(", ")),
                )
            }
            if (!pool.autoAssign) add(stringResource(R.string.lb_pool_manual))
            if (pool.namespaces.isNotEmpty()) add(stringResource(R.string.lb_pool_namespaces, pool.namespaces.joinToString(", ")))
        }
        if (notes.isNotEmpty()) MutedText(notes.joinToString("  ·  "))
    }
}

@Composable
internal fun AdvertisementRow(ad: LbAdvertisement, modifier: Modifier) {
    Column(modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Mono(ad.name, Modifier.weight(1f))
            Text(if (ad.kind == "bgp") "BGP" else "L2", style = MaterialTheme.typography.labelMedium)
        }
        MutedText(
            if (ad.allPools) stringResource(R.string.lb_adv_all_pools) else stringResource(R.string.lb_adv_pools, ad.pools.joinToString(", ")),
        )
        val via = ad.interfaces.ifEmpty { ad.peers }
        if (via.isNotEmpty()) MutedText(stringResource(R.string.lb_adv_via, via.joinToString(", ")))
    }
}

@Composable
internal fun PeerRow(peer: LbBgpPeer, modifier: Modifier) {
    Column(modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Mono(peer.name, Modifier.weight(1f))
            Text(peer.peerAddress.ifEmpty { peer.`interface` }, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
        }
        val asn = peer.peerASN.takeIf { it > 0 }?.toString() ?: peer.dynamicASN
        MutedText(stringResource(R.string.lb_peer_asn, asn, peer.myASN.toString()))
        when {
            !peer.sessionsKnown -> MutedText(stringResource(R.string.lb_peer_sessions_unknown))
            peer.sessions.isEmpty() -> MutedText(stringResource(R.string.lb_peer_no_session))
            else -> peer.sessions.forEach { s ->
                Text(
                    stringResource(if (s.up) R.string.lb_peer_session_up else R.string.lb_peer_session_down, s.node),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (s.up) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

@Composable
internal fun KubeVipRows(vip: LbKubeVip, modifier: Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (vip.ranges.isEmpty()) MutedText(stringResource(R.string.lb_kubevip_no_range))
        vip.ranges.forEach { r ->
            val scope = if (r.scope == "global") stringResource(R.string.lb_kubevip_global) else r.scope
            MutedText("$scope: ${r.cidrs.joinToString(", ")}")
        }
        val now = System.currentTimeMillis()
        vip.leases.forEach { l ->
            val age = ageSince(l.renewed, now)
            MutedText(
                if (age.isEmpty()) stringResource(R.string.lb_kubevip_lease, l.name, l.holder)
                else stringResource(R.string.lb_kubevip_lease_renewed, l.name, l.holder, age),
            )
        }
    }
}

@Composable
internal fun ServiceRow(svc: LbService, modifier: Modifier, onClick: () -> Unit) {
    Column(modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Mono(svc.key, Modifier.weight(1f))
            Text(
                if (svc.pending) stringResource(R.string.kube_services_no_address) else svc.addresses.joinToString(", "),
                style = MaterialTheme.typography.labelMedium,
                fontFamily = FontFamily.Monospace,
                color = svc.level.color(),
            )
        }
        val notes = listOfNotNull(
            svc.pool.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.lb_service_pool, it) },
            svc.announcedBy.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.lb_service_announced_by, it) },
        )
        if (notes.isNotEmpty()) MutedText(notes.joinToString("  ·  "))
    }
}
