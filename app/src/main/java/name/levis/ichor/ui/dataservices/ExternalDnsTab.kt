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
import name.levis.ichor.model.ExternalDnsController
import name.levis.ichor.model.ExternalDnsReason
import name.levis.ichor.model.ExternalDnsRecord
import name.levis.ichor.model.ExternalDnsStatus
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * ExternalDNS: every controller with its settings (provider, policy, owner id, domain
 * filters, sources, interval), then the hostnames it is asked to publish, problems first.
 * A record's row opens its source object's summary. ExternalDNS keeps no status on records:
 * nothing here says one exists at the provider.
 */
@Composable
fun ExternalDnsTab(status: ExternalDnsStatus, onObject: (KubeObjectRef) -> Unit) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.controllers.isEmpty() && status.records.isEmpty()) item { EmptyText(stringResource(R.string.externaldns_empty)) }
        items(status.controllers, key = { "c:${it.label}" }) { ControllerRow(it) }
        if (status.controllers.isNotEmpty()) item(key = "controllers-end") { HorizontalDivider() }
        if (status.truncated) item {
            Text(
                pluralStringResource(R.plurals.externaldns_truncated, status.recordCount, status.records.size, status.recordCount),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        items(status.records, key = { "${it.dnsName}/${it.sourceKind}/${it.sourceNamespace}/${it.sourceName}" }) { r ->
            RecordRow(r, onObject)
            HorizontalDivider()
        }
    }
}

@Composable
private fun ControllerRow(c: ExternalDnsController) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(c.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            when {
                c.serviceHealth == ServiceHealth.CRITICAL -> StatusPill(stringResource(R.string.externaldns_not_ready), colors.bad)
                c.dryRun -> StatusPill(stringResource(R.string.externaldns_dry_run), colors.warn)
                c.ready -> StatusPill(stringResource(R.string.externaldns_ready), colors.ok)
            }
        }
        Column(Modifier.padding(start = 22.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            InfoRow(stringResource(R.string.externaldns_provider), listOf(c.provider, c.policy).filter { it.isNotEmpty() }.joinToString(" · "), mono = true)
            InfoRow(stringResource(R.string.externaldns_owner), c.ownerId, mono = true)
            if (c.domainFilters.isNotEmpty()) InfoRow(stringResource(R.string.externaldns_domain_filters), c.domainFilters.joinToString(", "), mono = true)
            InfoRow(stringResource(R.string.externaldns_sources), c.sources.joinToString(", "), mono = true)
            InfoRow(stringResource(R.string.externaldns_interval), stringResource(R.string.externaldns_interval_value, c.intervalSeconds))
            if (c.txtPrefix.isNotEmpty() || c.txtSuffix.isNotEmpty()) {
                InfoRow(stringResource(R.string.externaldns_txt), listOf(c.txtPrefix, c.txtSuffix).filter { it.isNotEmpty() }.joinToString(" · "), mono = true)
            }
            if (c.annotationFilter.isNotEmpty()) InfoRow(stringResource(R.string.externaldns_annotation_filter), c.annotationFilter, mono = true)
            c.reasonList.forEach { reason ->
                Text(reasonText(reason), style = MaterialTheme.typography.labelSmall, color = if (reason == ExternalDnsReason.NOT_READY) colors.bad else colors.warn)
            }
        }
    }
}

@Composable
private fun RecordRow(r: ExternalDnsRecord, onObject: (KubeObjectRef) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier.fillMaxWidth().clickable {
            onObject(KubeObjectRef(r.sourceGroup, r.sourceVersion, r.sourceResource, r.sourceKind, r.sourceNamespace, r.sourceName, editable = false))
        }.padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(r.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(r.dnsName, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            StatusPill(r.recordType, muted)
        }
        Text(
            listOfNotNull(
                r.targets.joinToString(", ").ifEmpty { null },
                stringResource(R.string.externaldns_from, r.sourceLabel),
                r.controller.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.externaldns_by, it) },
            ).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = r.serviceHealth.color().takeIf { r.serviceHealth.needsAttention } ?: muted,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(start = 22.dp),
        )
        r.reasonList.forEach { reason ->
            Text(reasonText(reason), style = MaterialTheme.typography.labelSmall, color = r.serviceHealth.color(), modifier = Modifier.padding(start = 22.dp))
        }
    }
}

@Composable
private fun reasonText(reason: ExternalDnsReason): String = when (reason) {
    ExternalDnsReason.NOT_READY -> stringResource(R.string.externaldns_reason_not_ready)
    ExternalDnsReason.DRY_RUN -> stringResource(R.string.externaldns_reason_dry_run)
    ExternalDnsReason.STALE -> stringResource(R.string.externaldns_reason_stale)
    ExternalDnsReason.NO_TARGET -> stringResource(R.string.externaldns_reason_no_target)
    ExternalDnsReason.UNMANAGED -> stringResource(R.string.externaldns_reason_unmanaged)
}
