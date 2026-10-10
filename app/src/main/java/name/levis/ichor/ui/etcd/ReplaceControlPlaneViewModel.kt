package name.levis.ichor.ui.etcd

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CpReplacePlan
import name.levis.ichor.model.membersBeforeJoin
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

/** The removal step's request (`talosctl etcd remove-member`). */
sealed interface CpRemoveState {
    data object Idle : CpRemoveState
    data object Running : CpRemoveState
    data class Failed(val message: UiText) : CpRemoveState
}

/** The last step: polling etcd for the new member. */
sealed interface CpJoinState {
    data object Idle : CpJoinState
    data class Waiting(val detail: String) : CpJoinState
    data object Joined : CpJoinState
    data class Failed(val message: UiText) : CpJoinState
}

/**
 * The guided replacement of the failed etcd member [memberId]: the plan (read from the cluster
 * on each refresh, so the screen resumes), the removal, and the wait for the new member. The
 * reset of the old node goes through the node's own ResetViewModel.
 */
class ReplaceControlPlaneViewModel(
    private val talos: TalosRepository,
    private val memberId: String,
    node: String,
) : ViewModel() {
    private val _plan = MutableStateFlow<UiState<CpReplacePlan>>(UiState.Loading)
    val plan: StateFlow<UiState<CpReplacePlan>> = _plan.asStateFlow()
    private val _remove = MutableStateFlow<CpRemoveState>(CpRemoveState.Idle)
    val remove: StateFlow<CpRemoveState> = _remove.asStateFlow()
    private val _join = MutableStateFlow<CpJoinState>(CpJoinState.Idle)
    val join: StateFlow<CpJoinState> = _join.asStateFlow()

    /** The old node's address: it names the node once the member has left etcd. */
    private var node = node

    /** The voting members to wait past, from the first plan read (before the new one joins). */
    private var membersBefore: Int? = null
    private var waitJob: Job? = null

    fun refresh() {
        val previous = (_plan.value as? UiState.Loaded)?.data
        _plan.value = previous?.let { UiState.Loaded(it, refreshing = true) } ?: UiState.Loading
        viewModelScope.launch {
            val next = uiStateOf { talos.controlPlaneReplacePlan(memberId, node) }
            (next as? UiState.Loaded)?.data?.let { p ->
                if (p.member.node.isNotBlank()) node = p.member.node
                // A plan read while etcd did not answer counts no member: not a baseline.
                if (membersBefore == null && p.quorum.members > 0) membersBefore = p.membersBeforeJoin
            }
            _plan.value = next
        }
    }

    /** Removes the member; Go checks the fresh plan first and sends it through a healthy member. */
    fun removeMember() {
        if (_remove.value == CpRemoveState.Running) return
        _remove.value = CpRemoveState.Running
        viewModelScope.launch {
            _remove.value = try {
                talos.controlPlaneReplaceRemove(memberId)
                CpRemoveState.Idle
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                CpRemoveState.Failed(e.uiText())
            }
            refresh()
        }
    }

    fun failRemoval(message: UiText) {
        _remove.value = CpRemoveState.Failed(message)
    }

    fun dismissRemoval() {
        if (_remove.value != CpRemoveState.Running) _remove.value = CpRemoveState.Idle
    }

    /** Polls etcd until the new member is a healthy voter; read-only, stops with the screen. */
    fun startWait() {
        val before = membersBefore ?: return
        if (waitJob?.isActive == true) return
        _join.value = CpJoinState.Waiting("")
        waitJob = viewModelScope.launch {
            while (isActive) {
                val result = try {
                    talos.controlPlaneReplaceWait(before, WAIT_SLICE_SECONDS)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Throwable) {
                    _join.value = CpJoinState.Failed(e.uiText())
                    return@launch
                }
                if (result.joined) {
                    _join.value = CpJoinState.Joined
                    refresh()
                    return@launch
                }
                _join.value = CpJoinState.Waiting(result.detail)
            }
        }
    }

    fun stopWait() {
        waitJob?.cancel()
        _join.value = CpJoinState.Idle
    }

    private companion object {
        /** Each Go call waits this long at most, so leaving the screen stops polling soon. */
        const val WAIT_SLICE_SECONDS = 30
    }
}
