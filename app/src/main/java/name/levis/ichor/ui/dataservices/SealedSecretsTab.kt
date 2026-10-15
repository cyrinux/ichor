package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.SealedSecretItem
import name.levis.ichor.model.SealedSecretsController
import name.levis.ichor.model.SealedSecretsReason
import name.levis.ichor.model.SealedSecretsStatus
import name.levis.ichor.model.SealingKey
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * Sealed Secrets: the controller, the sealing keys (newest first), then every SealedSecret
 * problems first with its scope and whether it unsealed. A row opens the SealedSecret's
 * summary. Names and status only: nothing here decodes a value.
 */
@Composable
fun SealedSecretsTab(status: SealedSecretsStatus, onObject: (KubeObjectRef) -> Unit) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        item(key = "controller") { ControllerCard(status.controller, status.reasonList) }
        if (status.keys.isNotEmpty()) {
            item(key = "keys-title") { SectionLabel(stringResource(R.string.sealedsecrets_keys)) }
            items(status.keys, key = { "k:${it.namespace}/${it.name}" }) { KeyRow(it) }
        }
        item(key = "keys-end") { HorizontalDivider() }
        if (status.items.isEmpty()) item { EmptyText(stringResource(R.string.sealedsecrets_empty)) }
        if (status.truncated) item {
            Text(
                pluralStringResource(R.plurals.sealedsecrets_truncated, status.secretCount, status.items.size, status.secretCount),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        items(status.items, key = { "s:${it.label}" }) { s ->
            SealedSecretRow(s, onObject)
            HorizontalDivider()
        }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp),
    )
}

/** The controller's replicas, then the section's own reasons (controller down, keys). */
@Composable
private fun ControllerCard(c: SealedSecretsController?, reasons: List<SealedSecretsReason>) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c?.serviceHealth ?: ServiceHealth.WARNING)
            Spacer(Modifier.size(12.dp))
            Text(
                c?.label ?: stringResource(R.string.sealedsecrets_no_controller),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace.takeIf { c != null },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            when {
                c == null -> Unit
                c.ready -> StatusPill(stringResource(R.string.sealedsecrets_ready), colors.ok)
                else -> StatusPill(stringResource(R.string.sealedsecrets_not_ready), colors.bad)
            }
        }
        Column(Modifier.padding(start = 22.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            if (c != null) InfoRow(stringResource(R.string.sealedsecrets_replicas), "${c.readyReplicas}/${c.replicas}", mono = true)
            reasons.forEach { reason ->
                Text(reasonText(reason), style = MaterialTheme.typography.labelSmall, color = if (reason.critical) colors.bad else colors.warn)
            }
        }
    }
}

@Composable
private fun KeyRow(k: SealingKey) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(start = 22.dp)) {
            Text(k.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val created = timeAgo(k.createdAt)
            if (created.isNotEmpty()) Text(stringResource(R.string.sealedsecrets_key_created, created), style = MaterialTheme.typography.labelSmall, color = muted)
        }
        when {
            k.active -> StatusPill(stringResource(R.string.sealedsecrets_key_active), colors.ok)
            k.compromised -> StatusPill(stringResource(R.string.sealedsecrets_key_compromised), colors.bad)
            k.state.isNotEmpty() -> StatusPill(k.state, muted)
        }
    }
}

@Composable
private fun SealedSecretRow(s: SealedSecretItem, onObject: (KubeObjectRef) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier.fillMaxWidth().clickable { onObject(s.objectRef) }.padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(s.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(s.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (s.scope.isNotEmpty()) StatusPill(s.scope, muted)
        }
        Text(
            listOfNotNull(
                pluralStringResource(R.plurals.sealedsecrets_item_keys, s.keyCount, s.keyCount),
                s.type.ifEmpty { null },
                stringResource(R.string.sealedsecrets_immutable).takeIf { s.immutable },
                stringResource(R.string.sealedsecrets_unsealed).takeIf { s.synced },
            ).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = muted,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(start = 22.dp),
        )
        if (!s.synced && (s.reason.isNotEmpty() || s.message.isNotEmpty())) {
            Text(
                listOf(s.reason, s.message).filter { it.isNotEmpty() }.joinToString(": "),
                style = MaterialTheme.typography.labelSmall,
                color = s.serviceHealth.color().takeIf { s.serviceHealth.needsAttention } ?: muted,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 22.dp),
            )
        }
        s.reasonList.forEach { reason ->
            Text(reasonText(reason), style = MaterialTheme.typography.labelSmall, color = s.serviceHealth.color(), modifier = Modifier.padding(start = 22.dp))
        }
    }
}

/** Reasons that stop secrets from being delivered, worded in red; the others are warnings. */
private val SealedSecretsReason.critical: Boolean
    get() = this == SealedSecretsReason.NOT_SYNCED || this == SealedSecretsReason.CONTROLLER_DOWN || this == SealedSecretsReason.NO_KEYS

@Composable
private fun reasonText(reason: SealedSecretsReason): String = when (reason) {
    SealedSecretsReason.NOT_SYNCED -> stringResource(R.string.sealedsecrets_reason_not_synced)
    SealedSecretsReason.UNMANAGED -> stringResource(R.string.sealedsecrets_reason_unmanaged)
    SealedSecretsReason.STALE -> stringResource(R.string.sealedsecrets_reason_stale)
    SealedSecretsReason.CONTROLLER_DOWN -> stringResource(R.string.sealedsecrets_reason_controller_down)
    SealedSecretsReason.NO_KEYS -> stringResource(R.string.sealedsecrets_reason_no_keys)
    SealedSecretsReason.KEY_COMPROMISED -> stringResource(R.string.sealedsecrets_reason_key_compromised)
    SealedSecretsReason.KEY_STALE -> stringResource(R.string.sealedsecrets_reason_key_stale)
}
