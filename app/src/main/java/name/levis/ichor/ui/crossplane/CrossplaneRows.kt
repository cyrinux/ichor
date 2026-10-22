package name.levis.ichor.ui.crossplane

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
import name.levis.ichor.model.CrossplaneCondition
import name.levis.ichor.model.CrossplaneDefinition
import name.levis.ichor.model.CrossplaneObject
import name.levis.ichor.model.CrossplanePackage
import name.levis.ichor.model.CrossplaneReason
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceDetails
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.util.timeAgo

/** "critical 1 · warning 2 · ok 12" pieces, in the header. */
@Composable
fun AssistCount(text: String, critical: Boolean) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        color = if (critical) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** A Provider, Configuration or Function: its package, revision and Installed / Healthy state. */
@Composable
fun PackageRow(p: CrossplanePackage, onClick: () -> Unit) {
    val line = listOfNotNull(
        p.`package`.takeIf { it.isNotEmpty() },
        p.currentRevision.takeIf { it.isNotEmpty() },
        conditionText(p.installed),
        conditionText(p.healthy),
    ).joinToString(" · ")
    ResourceRow(
        health = p.serviceHealth,
        title = "${p.kind} ${p.label}",
        modifier = Modifier.padding(horizontal = 16.dp),
        onClick = onClick,
    ) {
        ResourceDetails(
            (listOf(line.takeIf { it.isNotEmpty() }) + p.reasonList.map { reasonText(it) }).filterNotNull().joinToString(" · "),
        )
    }
}

/** An XRD: the API it defines, its scope and claim kind, its compositions and composites. */
@Composable
fun DefinitionRow(d: CrossplaneDefinition, onClick: () -> Unit) {
    val line = listOfNotNull(
        "${d.compositeGroup}/${d.compositeKind}".takeIf { d.compositeKind.isNotEmpty() },
        d.scope.takeIf { it.isNotEmpty() },
        d.claimKind.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_claim_kind, it) },
        stringResource(R.string.crossplane_compositions, d.compositions),
        stringResource(R.string.crossplane_composites_count, d.composites),
        conditionText(d.established),
    ).joinToString(" · ")
    ResourceRow(
        health = d.serviceHealth,
        title = d.label,
        modifier = Modifier.padding(horizontal = 16.dp),
        onClick = onClick,
    ) {
        ResourceDetails(
            (listOf(line.takeIf { it.isNotEmpty() }) + d.reasonList.map { reasonText(it) }).filterNotNull().joinToString(" · "),
        )
    }
}

/** A composite resource, a claim or a managed resource: Ready and Synced with their reasons. */
@Composable
fun ObjectRow(o: CrossplaneObject, showKind: Boolean, onClick: () -> Unit, actions: (@Composable () -> Unit)? = null) {
    val line = listOfNotNull(
        o.composition.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_composition, it) },
        stringResource(R.string.crossplane_resources, o.resources).takeIf { o.resources > 0 },
        o.claim.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_claim, it) },
        o.composite.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_composite, it) },
        o.providerConfig.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_provider_config, it) },
        o.externalName.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.crossplane_external_name, it) },
        conditionText(o.ready),
        conditionText(o.synced),
        o.synced.message.takeIf { it.isNotEmpty() },
    ).joinToString(" · ")
    ResourceRow(
        health = o.serviceHealth,
        title = (if (showKind) "${o.kind} " else "") + o.label,
        modifier = Modifier.padding(horizontal = 16.dp),
        trailing = { actions?.invoke() },
        onClick = onClick,
    ) {
        ResourceDetails(
            (listOf(line.takeIf { it.isNotEmpty() }) + o.reasonList.map { reasonText(it) } +
                listOfNotNull(stringResource(R.string.crossplane_paused).takeIf { o.paused })).filterNotNull().joinToString(" · "),
        )
    }
}

/** "True Available · 3 h ago", or null when the condition says nothing. */
@Composable
private fun conditionText(c: CrossplaneCondition): String? {
    if (c.status.isEmpty()) return null
    val parts = buildList {
        add(c.status)
        if (c.reason.isNotEmpty()) add(c.reason)
        if (c.lastTransition > 0) add(timeAgo(c.lastTransition))
    }

    return parts.joinToString(" ").ifEmpty { null }
}

@Composable
private fun reasonText(reason: CrossplaneReason): String = when (reason) {
    CrossplaneReason.SYNC_FAILED -> stringResource(R.string.crossplane_reason_sync_failed)
    CrossplaneReason.PROVIDER_UNHEALTHY -> stringResource(R.string.crossplane_reason_provider_unhealthy)
    CrossplaneReason.NOT_READY -> stringResource(R.string.crossplane_reason_not_ready)
    CrossplaneReason.DELETING -> stringResource(R.string.crossplane_reason_deleting)
    CrossplaneReason.PAUSED -> stringResource(R.string.crossplane_reason_paused)
    CrossplaneReason.NOT_ESTABLISHED -> stringResource(R.string.crossplane_reason_not_established)
}
