package name.levis.ichor.ui.policies

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KyvernoPolicy
import name.levis.ichor.model.PolicyFailure
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** One policy or constraint: what its mode does, whether it is ready, its rules, then the resources it fails. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PolicyDetailSheet(policy: KyvernoPolicy, onDismiss: () -> Unit, onObject: (KubeObjectRef) -> Unit) {
    val colors = LocalStatusColors.current
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(policy.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
                ActionBadge(policy.action)
            }
            MutedText(policyScopeText(policy))
            actionMeaning(policy.action)?.let { Text(it, style = MaterialTheme.typography.bodyMedium) }
            if (policy.description.isNotEmpty()) {
                Text(policy.description, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(top = 4.dp))
            }
            if (!policy.ready) {
                Row(Modifier.padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    NotReadyBadge(policy)
                    if (policy.readyReason.isNotEmpty()) Text(policy.readyReason, style = MaterialTheme.typography.bodyMedium, color = colors.bad)
                }
                if (policy.readyMessage.isNotEmpty()) MutedText(policy.readyMessage)
            }
            if (policy.category.isNotEmpty()) InfoRow(stringResource(R.string.kpol_category), policy.category)
            if (policy.severity.isNotEmpty()) InfoRow(stringResource(R.string.kpol_severity), policy.severity)
            if (policy.background) MutedText(stringResource(R.string.kpol_background))
            if (policy.auditAt > 0) MutedText(stringResource(R.string.kpol_audited, timeAgo(policy.auditAt * 1000)))

            // A Gatekeeper constraint has no rules of its own: its template's Rego is the rule.
            if (!policy.isGatekeeper) {
                SectionTitle(stringResource(R.string.kpol_rules))
                Text(rulesByKind(policy), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                policy.ruleNames.forEach { MutedText(it) }
            }
            ResultCounts(policy.fail, policy.warn, policy.pass, Modifier.padding(top = 4.dp))

            SectionTitle(stringResource(R.string.kpol_failing))
            if (policy.failing.isEmpty() && policy.fail == 0) MutedText(stringResource(R.string.kpol_no_failing))
            policy.failing.forEach { FailureRow(it, onObject) }
            if (policy.fail > policy.failing.size) {
                val shown = if (policy.failingCapped) R.string.kpol_failing_capped else R.string.kpol_failing_shown
                MutedText(stringResource(shown, policy.failing.size.toString(), policy.fail.toString()))
            }
        }
    }
}

/** "validate 2 · verifyImages 1": the rule kinds as Kyverno spells them. */
private fun rulesByKind(policy: KyvernoPolicy): String = with(policy.rules) {
    listOf("validate" to validate, "mutate" to mutate, "generate" to generate, "verifyImages" to verifyImages)
        .filter { it.second > 0 }
        .joinToString("  ·  ") { (kind, n) -> "$kind $n" }
}

/** Kind namespace/name (opening its object screen when the browser knows the kind), the rule and its message. */
@Composable
private fun FailureRow(failure: PolicyFailure, onObject: (KubeObjectRef) -> Unit) {
    val ref = failure.objectRef()
    val target = listOf(failure.namespace, failure.name).filter { it.isNotEmpty() }.joinToString("/")
    Column(
        Modifier
            .fillMaxWidth()
            .let { if (ref != null) it.clickable(role = Role.Button) { onObject(ref) } else it }
            .padding(vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            listOf(failure.kind, target).filter { it.isNotEmpty() }.joinToString(" "),
            style = MaterialTheme.typography.bodyMedium,
            fontFamily = FontFamily.Monospace,
            color = if (ref != null) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
        )
        val meta = listOfNotNull(
            failure.rule.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.kpol_rule_of, it) },
            failure.at.takeIf { it > 0 }?.let { timeAgo(it * 1000) },
        )
        if (meta.isNotEmpty()) MutedText(meta.joinToString("  ·  "))
        if (failure.message.isNotEmpty()) Text(failure.message, style = MaterialTheme.typography.bodySmall)
    }
}
