package name.levis.ichor.ui.dataservices

import android.text.format.DateUtils
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
import name.levis.ichor.model.CertIssuer
import name.levis.ichor.model.CertManagerStatus
import name.levis.ichor.model.CertReason
import name.levis.ichor.model.Certificate
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle

/** Every certificate (problems first, then the soonest expiry) with its issuer, then the issuers. */
@Composable
fun CertificatesTab(status: CertManagerStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.certificates.isEmpty()) item { EmptyText(stringResource(R.string.certmanager_empty)) }
        items(status.certificates, key = { it.label }) { cert ->
            CertificateRow(cert)
            HorizontalDivider()
        }
        if (status.issuers.isNotEmpty()) {
            item { SectionTitle(stringResource(R.string.certmanager_issuers), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(status.issuers, key = { "issuer|${it.label}" }) { issuer ->
                IssuerRow(issuer)
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun CertificateRow(cert: Certificate) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(cert.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(cert.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        // Expiry is worded by the date itself: expired and expiring have no words of their own.
        val failed = cert.failedAttempts.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.certmanager_failed_attempts, it, it) }
        val line = listOf(expiryText(cert)) + cert.reasonList.mapNotNull { reasonText(it) } + listOfNotNull(failed)
        Text(
            line.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = cert.serviceHealth.color().takeIf { cert.serviceHealth.needsAttention } ?: muted,
            modifier = Modifier.padding(start = 22.dp),
        )
        val more = cert.dnsNameCount - cert.dnsNames.size
        val names = cert.dnsNames.joinToString(", ") + if (more > 0) " +$more" else ""
        Text(
            listOf(names, cert.issuer).filter { it.isNotBlank() }.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = muted,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(start = 22.dp),
        )
        if (cert.message.isNotEmpty()) {
            Text(cert.message, style = MaterialTheme.typography.labelSmall, color = muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 22.dp))
        }
    }
}

@Composable
private fun IssuerRow(issuer: CertIssuer) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(issuer.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(issuer.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        val type = when (issuer.type) {
            "acme" -> "ACME"
            "ca" -> "CA"
            "selfSigned" -> stringResource(R.string.certmanager_type_self_signed)
            "vault" -> "Vault"
            "venafi" -> "Venafi"
            else -> null
        }
        val state = if (issuer.ready) null else stringResource(R.string.certmanager_reason_not_ready)
        Text(
            listOfNotNull(type, issuer.server.ifEmpty { null }, state).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = issuer.serviceHealth.color().takeIf { issuer.serviceHealth.needsAttention } ?: muted,
            modifier = Modifier.padding(start = 22.dp),
        )
        if (!issuer.ready && issuer.message.isNotEmpty()) {
            Text(issuer.message, style = MaterialTheme.typography.labelSmall, color = muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 22.dp))
        }
    }
}

/** "expires in 23 days", "expired 2 days ago", or not issued yet. */
@Composable
private fun expiryText(cert: Certificate): String {
    if (cert.notAfter <= 0) return stringResource(R.string.certmanager_not_issued)
    val now = System.currentTimeMillis()
    val relative = DateUtils.getRelativeTimeSpanString(cert.notAfter, now, DateUtils.MINUTE_IN_MILLIS).toString()
    return stringResource(if (cert.notAfter > now) R.string.certmanager_expires else R.string.certmanager_expired, relative)
}

@Composable
private fun reasonText(reason: CertReason): String? = when (reason) {
    CertReason.EXPIRED, CertReason.EXPIRING -> null
    CertReason.NOT_READY -> stringResource(R.string.certmanager_reason_not_ready)
    CertReason.RENEWAL_OVERDUE -> stringResource(R.string.certmanager_reason_renewal_overdue)
    CertReason.ISSUER -> stringResource(R.string.certmanager_reason_issuer)
}
