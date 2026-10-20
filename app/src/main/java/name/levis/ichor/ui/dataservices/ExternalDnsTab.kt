package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.DnsCheckEntry
import name.levis.ichor.model.DnsCheckStatus
import name.levis.ichor.model.ExternalDnsController
import name.levis.ichor.model.ExternalDnsReason
import name.levis.ichor.model.ExternalDnsRecord
import name.levis.ichor.model.ExternalDnsStatus
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.cardGap
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * ExternalDNS: every controller with its settings (provider, policy, owner id, domain
 * filters, sources, interval), then the hostnames it is asked to publish, problems first.
 * A record's row opens its source object's summary. ExternalDNS keeps no status on records:
 * nothing here says one exists at the provider, so "Check names" resolves them from the
 * phone and compares the answers with the targets.
 */
@Composable
fun ExternalDnsTab(status: ExternalDnsStatus, onObject: (KubeObjectRef) -> Unit, actions: ExternalDnsActions) {
    val check by actions.state.collectAsStateWithLifecycle()
    val verdicts = check?.byName.orEmpty()
    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.controllers.isEmpty() && status.records.isEmpty()) item { EmptyText(stringResource(R.string.externaldns_empty)) }
        // The check leads the tab: below the controller cards it was scrolled out of sight.
        if (status.records.isNotEmpty()) item(key = "dns-check") { DnsCheckHeader(status, check, actions) }
        cardRows(status.controllers, key = { "c:${it.label}" }) { ControllerRow(it) }
        if (status.controllers.isNotEmpty()) cardGap("controllers-end")
        if (status.truncated) item {
            Text(
                pluralStringResource(R.plurals.externaldns_truncated, status.recordCount, status.records.size, status.recordCount),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp),
            )
        }
        cardRows(status.records, key = { "${it.dnsName}/${it.sourceKind}/${it.sourceNamespace}/${it.sourceName}" }) { r ->
            RecordRow(r, onObject, verdicts[r.dnsName])
        }
    }
}

/**
 * The "Check names" button, whether a check ran, what it does, and the last check's summary.
 * The names go to the phone's DNS resolver: that is the feature, and the hint says so. Names
 * with no target to compare with are counted apart: they are neither pointing here nor away.
 */
@Composable
private fun DnsCheckHeader(status: ExternalDnsStatus, check: DnsCheckState?, actions: ExternalDnsActions) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = { actions.check(status) }, enabled = check?.running != true) {
                Text(stringResource(R.string.externaldns_check_names))
            }
            if (check?.running == true) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
        }
        when {
            check?.running == true -> MutedText(stringResource(R.string.externaldns_check_running))
            check == null -> MutedText(stringResource(R.string.externaldns_check_not_run))
        }
        MutedText(stringResource(R.string.externaldns_check_hint))
        check?.error?.let { InlineError(it.asString()) }
        check?.result?.let { result ->
            val pointing = result.results.count { it.verdict == DnsCheckStatus.MATCH }
            val unverified = result.results.count { it.verdict == DnsCheckStatus.UNVERIFIED }
            Text(
                pluralStringResource(R.plurals.externaldns_check_summary, result.results.size, pointing, result.results.size),
                style = MaterialTheme.typography.bodyMedium,
                color = when {
                    result.results.any { it.verdict == DnsCheckStatus.MISSING } -> colors.bad
                    pointing == result.results.size -> colors.ok
                    else -> colors.warn
                },
            )
            if (unverified > 0) MutedText(stringResource(R.string.externaldns_check_unverified_count, unverified))
            if (result.results.any { it.verdict == DnsCheckStatus.MISMATCH }) {
                MutedText(stringResource(R.string.externaldns_check_mismatch_hint))
            }
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
private fun RecordRow(r: ExternalDnsRecord, onObject: (KubeObjectRef) -> Unit, verdict: DnsCheckEntry?) {
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
            Tag(r.recordType, mono = true)
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
        verdict?.let { VerdictRow(it) }
    }
}

/** What the last check found for one record, and who owns it when that is not this cluster. */
@Composable
private fun VerdictRow(entry: DnsCheckEntry) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.padding(start = 22.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            when (entry.verdict) {
                DnsCheckStatus.MATCH -> StatusPill(stringResource(R.string.externaldns_check_match), colors.ok)
                DnsCheckStatus.MISMATCH -> StatusPill(stringResource(R.string.externaldns_check_mismatch), colors.warn)
                DnsCheckStatus.MISSING -> StatusPill(stringResource(R.string.externaldns_check_missing), colors.bad)
                DnsCheckStatus.UNVERIFIED -> StatusPill(stringResource(R.string.externaldns_check_unverified), muted)
                DnsCheckStatus.ERROR -> StatusPill(stringResource(R.string.externaldns_check_error), muted)
                null -> {}
            }
            if (entry.ownerMatch == false) StatusPill(stringResource(R.string.externaldns_check_owner_other, entry.owner), colors.warn)
        }
        if (entry.probed.isNotEmpty()) {
            MutedText(stringResource(R.string.externaldns_check_probed, entry.probed), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (entry.answers.isNotEmpty() && entry.verdict != DnsCheckStatus.ERROR) {
            MutedText(entry.answers.joinToString(", "), maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (entry.verdict == DnsCheckStatus.ERROR && entry.message.isNotEmpty()) {
            MutedText(entry.message, maxLines = 2, overflow = TextOverflow.Ellipsis)
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
