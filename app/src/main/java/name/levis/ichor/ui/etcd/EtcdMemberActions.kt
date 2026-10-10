package name.levis.ichor.ui.etcd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.EtcdMemberPlan
import name.levis.ichor.model.EtcdMemberRef
import name.levis.ichor.model.FeatureSupport
import name.levis.ichor.model.allowed
import name.levis.ichor.model.confirmToken
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.FeatureMenuItem
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

sealed interface MemberActionState {
    data object Idle : MemberActionState

    /** Working out what removing [member] would leave. */
    data class Planning(val member: EtcdMemberRef) : MemberActionState

    /** The plan to show before confirming the removal. */
    data class Planned(val plan: EtcdMemberPlan) : MemberActionState
    data class Running(val label: UiText) : MemberActionState
    /** [changed] false: the request went through but had nothing to do. */
    data class Done(val message: UiText, val changed: Boolean = true) : MemberActionState
    data class Failed(val message: UiText) : MemberActionState
}

val MemberActionState.busy: Boolean get() = this is MemberActionState.Planning || this is MemberActionState.Running

/** `talosctl etcd forfeit-leadership` and `etcd remove-member` (os:admin), one at a time. */
class EtcdMemberActionsViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _state = MutableStateFlow<MemberActionState>(MemberActionState.Idle)
    val state: StateFlow<MemberActionState> = _state.asStateFlow()
    private var job: Job? = null

    /** Loads the removal plan of [member]; nothing changes on the cluster. */
    fun plan(member: EtcdMemberRef) {
        if (_state.value.busy) return
        _state.value = MemberActionState.Planning(member)
        job = viewModelScope.launch {
            _state.value = attempt {
                val plan = talos.etcdMemberPlan(member.id)
                // The plan may come back without the name the screen already knows.
                MemberActionState.Planned(if (plan.member.id.isEmpty()) plan.copy(member = member) else plan)
            }
        }
    }

    fun forfeitLeadership(node: String, hostname: String) {
        if (_state.value.busy) return
        _state.value = MemberActionState.Running(UiText.Res(R.string.etcd_forfeit_running, hostname))
        job = viewModelScope.launch {
            _state.value = attempt {
                // No new leader in the answer: the node was not the leader, nothing changed.
                if (talos.etcdForfeitLeadership(node).member.isEmpty()) {
                    MemberActionState.Done(UiText.Res(R.string.etcd_forfeit_not_leader, hostname), changed = false)
                } else {
                    MemberActionState.Done(UiText.Res(R.string.etcd_forfeit_done, hostname))
                }
            }
        }
    }

    /** Removes [member], asking [viaNode] (another member). */
    fun remove(viaNode: String, member: EtcdMemberRef) {
        if (_state.value is MemberActionState.Running) return
        _state.value = MemberActionState.Running(UiText.Res(R.string.etcd_remove_running, member.confirmToken))
        job = viewModelScope.launch {
            _state.value = attempt {
                talos.etcdRemoveMember(viaNode, member.id)
                MemberActionState.Done(UiText.Res(R.string.etcd_remove_done, member.confirmToken))
            }
        }
    }

    fun fail(message: UiText) {
        if (!_state.value.busy) _state.value = MemberActionState.Failed(message)
    }

    /** Closes the plan or the result; a running action cannot be dismissed. */
    fun dismiss() {
        if (_state.value is MemberActionState.Running) return
        job?.cancel()
        _state.value = MemberActionState.Idle
    }

    private suspend fun attempt(block: suspend () -> MemberActionState): MemberActionState = try {
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Throwable) {
        MemberActionState.Failed(e.uiText())
    }
}

/**
 * What a member card offers; a null action is not offered (role, or not the leader).
 * [onReplace]: the guided replacement, for a failed member only.
 */
data class MemberActions(
    val support: FeatureSupport,
    val enabled: Boolean,
    val onForfeit: (() -> Unit)?,
    val onRemove: (() -> Unit)?,
    val onReplace: (() -> Unit)? = null,
)

/** The member's overflow menu: "Replace this control plane…" (a failed member), "Remove member…". */
@Composable
fun MemberOverflow(actions: MemberActions) {
    if (actions.onRemove == null && actions.onReplace == null) return
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { open = true }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.common_more)) }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            actions.onReplace?.let { onReplace ->
                FeatureMenuItem(
                    label = stringResource(R.string.cp_replace_entry),
                    icon = Icons.Outlined.SwapHoriz,
                    support = actions.support,
                    enabled = actions.enabled,
                    onClick = {
                        open = false
                        onReplace()
                    },
                )
            }
            actions.onRemove?.let { onRemove ->
                FeatureMenuItem(
                    label = stringResource(R.string.etcd_remove_member),
                    icon = Icons.Outlined.PersonRemove,
                    support = actions.support,
                    enabled = actions.enabled,
                    onClick = {
                        open = false
                        onRemove()
                    },
                )
            }
        }
    }
}

/** Progress and result of a member action, above the member list. */
@Composable
fun MemberActionPanel(state: MemberActionState, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    when (state) {
        MemberActionState.Idle, is MemberActionState.Planning, is MemberActionState.Planned -> Unit
        is MemberActionState.Running -> Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(state.label.asString(), style = MaterialTheme.typography.titleSmall)
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        }
        is MemberActionState.Done -> Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                if (state.changed) Text(state.message.asString(), color = colors.ok) else InfoNotice(state.message.asString())
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
            }
        }
        is MemberActionState.Failed -> Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                InlineError(state.message.asString())
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_ok)) }
            }
        }
    }
}

@Composable
fun PlanningDialog(member: EtcdMemberRef, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.etcd_remove_title, member.confirmToken)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.etcd_remove_planning))
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        },
        confirmButton = {},
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/**
 * The removal plan, then the member's hostname to type. [viaNode] null: no other member
 * can take the request, which forbids the removal like a blocker.
 */
@Composable
fun RemoveMemberDialog(plan: EtcdMemberPlan, viaNode: String?, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    HostnameConfirmDialog(
        title = stringResource(R.string.etcd_remove_title, plan.member.confirmToken),
        hostname = plan.member.confirmToken,
        confirmLabel = stringResource(R.string.etcd_remove_confirm),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        emphasized = true,
        enabled = plan.allowed && viaNode != null,
    ) {
        Text(stringResource(R.string.etcd_remove_body), style = MaterialTheme.typography.bodyMedium)
        Column {
            InfoRow(stringResource(R.string.etcd_remove_members_after), plan.membersAfter.toString())
            InfoRow(stringResource(R.string.etcd_remove_healthy_after), plan.healthyAfter.toString())
            InfoRow(
                stringResource(R.string.etcd_remove_quorum),
                stringResource(if (plan.quorumAfter) R.string.etcd_remove_quorum_kept else R.string.etcd_remove_quorum_lost),
            )
        }
        plan.blockers.forEach { Text(it, color = colors.bad, style = MaterialTheme.typography.bodySmall) }
        if (viaNode == null) Text(stringResource(R.string.etcd_remove_no_peer), color = colors.bad, style = MaterialTheme.typography.bodySmall)
        plan.warnings.forEach { Text(it, color = colors.warn, style = MaterialTheme.typography.bodySmall) }
    }
}

@Composable
fun ForfeitConfirmDialog(hostname: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.etcd_forfeit_title),
        text = stringResource(R.string.etcd_forfeit_body, hostname),
        confirm = stringResource(R.string.etcd_forfeit),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}
