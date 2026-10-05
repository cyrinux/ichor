package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AcmeChallenge
import name.levis.ichor.model.AcmeOrder
import name.levis.ichor.model.CertCondition
import name.levis.ichor.model.CertEvent
import name.levis.ichor.model.CertRequestDetail
import name.levis.ichor.model.acmeFailed
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * Why a certificate is in its state: its conditions, its latest requests with their ACME orders
 * and challenges, the events of that chain and the controller log lines naming it, with a
 * forced renewal when it is not being issued.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CertificateDetailsSheet(state: CertDetailsState, renewing: Boolean, onReload: () -> Unit, onRenew: () -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            item { Header(state, onReload) }
            if (!state.cert.issuing) item {
                OutlinedButton(onClick = onRenew, enabled = !renewing) { Text(stringResource(R.string.certmanager_renew)) }
            }
            if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            state.error?.let { error ->
                item {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        InlineError(error.asString())
                        if (state.details == null) OutlinedButton(onClick = onReload) { Text(stringResource(R.string.common_retry)) }
                    }
                }
            }
            val details = state.details ?: return@LazyColumn
            if (details.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, details.error)) }

            item { Section(R.string.certmanager_details_conditions) }
            items(details.conditions) { ConditionLine(it) }

            if (details.requests.isNotEmpty()) item { Section(R.string.certmanager_details_requests) }
            items(details.requests, key = { "req|${it.name}" }) { RequestBlock(it) }

            item { Section(R.string.certmanager_details_events) }
            if (details.events.isEmpty()) item { EmptyText(stringResource(R.string.certmanager_details_no_events)) }
            items(details.events) { EventLine(it) }

            item { Section(R.string.certmanager_details_log) }
            if (details.log.isEmpty()) {
                item { EmptyText(stringResource(R.string.certmanager_details_no_log)) }
            } else {
                item {
                    SelectionContainer {
                        Text(
                            details.log.joinToString("\n"),
                            style = MaterialTheme.typography.labelSmall,
                            fontFamily = FontFamily.Monospace,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun Header(state: CertDetailsState, onReload: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(state.cert.label, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
            Text(state.cert.issuer, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        IconButton(onClick = onReload, enabled = !state.loading) { Icon(Icons.Outlined.Refresh, stringResource(R.string.data_services_refresh)) }
    }
}

@Composable
private fun Section(title: Int) {
    SectionTitle(stringResource(title), Modifier.padding(top = 8.dp))
}

/** "Ready: False · Failed · 2 hours ago", then the message. Ready (or Approved) not True is a warning. */
@Composable
private fun ConditionLine(c: CertCondition, indent: Int = 0) {
    val colors = LocalStatusColors.current
    val color = when {
        c.type == "Denied" || c.type == "InvalidRequest" -> if (c.isTrue) colors.bad else null
        (c.type == "Ready" || c.type == "Approved") && !c.isTrue -> colors.warn
        else -> null
    }
    Detail(
        title = listOf("${c.type}: ${c.status}", c.reason, timeAgo(c.time)).filter { it.isNotEmpty() }.joinToString(" · "),
        message = c.message,
        color = color,
        indent = indent,
    )
}

@Composable
private fun RequestBlock(r: CertRequestDetail) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            listOf(r.name, timeAgo(r.created)).filter { it.isNotEmpty() }.joinToString(" · "),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
        )
        r.conditions.forEach { ConditionLine(it, indent = 12) }
        r.orders.forEach { OrderBlock(it) }
    }
}

@Composable
private fun OrderBlock(o: AcmeOrder) {
    val colors = LocalStatusColors.current
    Detail(
        title = stringResource(R.string.certmanager_details_order, o.state.ifEmpty { "…" }),
        message = o.reason,
        color = if (acmeFailed(o.state)) colors.bad else null,
        indent = 12,
    )
    o.challenges.forEach { ChallengeLine(it) }
}

/** "HTTP-01 shop.example.com · pending", then why it is not valid yet. */
@Composable
private fun ChallengeLine(c: AcmeChallenge) {
    val colors = LocalStatusColors.current
    val name = (if (c.wildcard) "*." else "") + c.dnsName
    Detail(
        title = listOf(c.type, name).filter { it.isNotEmpty() }.joinToString(" ") + " · " + c.state.ifEmpty { "…" },
        message = c.reason,
        color = when {
            acmeFailed(c.state) -> colors.bad
            c.state != "valid" && c.reason.isNotEmpty() -> colors.warn
            else -> null
        },
        indent = 24,
    )
}

/** "Warning · PresentError · Challenge/x · ×37 · 4 minutes ago", then the message. */
@Composable
private fun EventLine(e: CertEvent) {
    val title = listOf(e.reason, e.`object`, if (e.count > 1) "×${e.count}" else "", timeAgo(e.time)).filter { it.isNotEmpty() }.joinToString(" · ")
    Detail(title = title, message = e.message, color = if (e.warning) LocalStatusColors.current.warn else null)
}

@Composable
private fun Detail(title: String, message: String, color: Color?, indent: Int = 0) {
    Column(Modifier.padding(start = indent.dp), verticalArrangement = Arrangement.spacedBy(1.dp)) {
        Text(title, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.Medium, color = color ?: MaterialTheme.colorScheme.onSurface)
        if (message.isNotEmpty()) {
            SelectionContainer {
                Text(message, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
