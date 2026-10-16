package name.levis.ichor.ui.policies

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.Flow
import name.levis.ichor.R
import name.levis.ichor.model.KYVERNO_GROUP
import name.levis.ichor.model.KyvernoPolicy
import name.levis.ichor.model.POLICY_ACTION_AUDIT
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.components.rememberKubeCanDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/** What a switch did: [error] null when the policy is now in [action] mode. */
data class PolicySwitchResult(val name: String, val action: String, val error: UiText?)

@Composable
fun PolicySwitchToasts(results: Flow<PolicySwitchResult>) = ResultToasts(results) { context, r ->
    val mode = context.getString(if (r.action == POLICY_ACTION_AUDIT) R.string.kpol_audit else R.string.kpol_enforce)
    val message = r.error?.resolve(context)?.let { context.getString(R.string.kpol_switch_failed, r.name, it) }
        ?: context.getString(R.string.kpol_switched, r.name, mode)
    message to (r.error != null)
}

private fun switchLabel(action: String): Int =
    if (action == POLICY_ACTION_AUDIT) R.string.kpol_switch_audit else R.string.kpol_switch_enforce

/**
 * The Audit/Enforce switch of a Kyverno policy, disabled with the reason when the credentials
 * may not patch it. Audit weakens enforcement: the policy's name is typed to confirm it.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PolicySwitchButtons(policy: KyvernoPolicy, busy: Boolean, onSetAction: (String) -> Unit) {
    val targets = policy.switchTargets
    if (targets.isEmpty()) return
    val denial = rememberKubeCanDenial("patch", KYVERNO_GROUP, policy.kyvernoResource, policy.namespace, policy.name)
    var confirming by rememberSaveable(policy.key) { mutableStateOf<String?>(null) }

    Column(Modifier.padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            targets.forEach { target ->
                OutlinedButton(onClick = { confirming = target }, enabled = !busy && denial == null) {
                    Text(stringResource(switchLabel(target)))
                }
            }
        }
        KubeDenialNote(denial)
    }

    confirming?.let { target ->
        PolicySwitchConfirm(policy, target, onDismiss = { confirming = null }, onConfirm = {
            confirming = null
            onSetAction(target)
        })
    }
}

@Composable
private fun PolicySwitchConfirm(policy: KyvernoPolicy, target: String, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val title = stringResource(switchLabel(target))
    val gitops = policy.gitopsTool?.let { stringResource(R.string.kpol_switch_gitops, it) }
    if (target == POLICY_ACTION_AUDIT) {
        HostnameConfirmDialog(title = title, hostname = policy.name, confirmLabel = title, onConfirm = onConfirm, onDismiss = onDismiss, emphasized = true) {
            Text(stringResource(R.string.kpol_switch_audit_body))
            gitops?.let { Text(it, color = LocalStatusColors.current.warn) }
        }
    } else {
        ConfirmDialog(
            title = title,
            text = listOfNotNull(stringResource(R.string.kpol_switch_enforce_body), gitops).joinToString("\n\n"),
            confirm = title,
            onConfirm = onConfirm,
            onDismiss = onDismiss,
        )
    }
}
