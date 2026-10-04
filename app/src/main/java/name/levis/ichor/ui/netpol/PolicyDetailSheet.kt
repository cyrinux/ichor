package name.levis.ichor.ui.netpol

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.NetPolicy
import name.levis.ichor.model.NetRule
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/** One policy: who it applies to, the pods it selects now, then what each direction lets through. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PolicyDetailSheet(policy: NetPolicy, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                KindBadge(policy)
                Text(policy.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
            }
            MutedText(listOf(policy.kind, policy.namespace.ifEmpty { stringResource(R.string.netpol_cluster_wide) }).joinToString("  ·  "))
            if (policy.description.isNotEmpty()) {
                Text(policy.description, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(top = 4.dp))
            }
            InfoRow(stringResource(R.string.netpol_applies_to), subjectText(policy), mono = true)
            if (policy.subjectNamespace.isNotEmpty()) {
                InfoRow(stringResource(R.string.netpol_namespace), policy.subjectNamespace, mono = true)
            }
            if (policy.created > 0) {
                InfoRow(
                    stringResource(R.string.netpol_created),
                    DateUtils.getRelativeTimeSpanString(policy.created, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS).toString(),
                )
            }

            if (!policy.nodes) {
                SectionTitle(stringResource(R.string.netpol_selected_pods) + " (${policy.podCount})")
                if (policy.pods.isEmpty()) MutedText(stringResource(R.string.netpol_no_pods))
                policy.pods.forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
                if (policy.podCount > policy.pods.size) MutedText(stringResource(R.string.netpol_more_pods, policy.podCount - policy.pods.size))
            }

            Direction(stringResource(R.string.netpol_ingress), policy.ingress, policy.ingressRules, ingress = true, policy = policy)
            Direction(stringResource(R.string.netpol_egress), policy.egress, policy.egressRules, ingress = false, policy = policy)
        }
    }
}

@Composable
private fun Direction(title: String, isolated: Boolean, rules: List<NetRule>, ingress: Boolean, policy: NetPolicy) {
    val colors = LocalStatusColors.current
    SectionTitle(title)
    when {
        isolated && rules.isEmpty() -> Text(stringResource(R.string.netpol_deny_all), color = colors.bad, style = MaterialTheme.typography.bodyMedium)
        isolated -> Text(stringResource(R.string.netpol_isolated), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
        rules.isEmpty() -> MutedText(stringResource(R.string.netpol_not_restricted))
        // Deny rules without isolation (Cilium): the rest still passes.
        else -> MutedText(stringResource(R.string.netpol_not_restricted_but))
    }
    Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        rules.forEach { RuleCard(it, ingress, policy) }
    }
}
