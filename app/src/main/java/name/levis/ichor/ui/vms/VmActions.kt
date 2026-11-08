package name.levis.ichor.ui.vms

import androidx.compose.runtime.Composable
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichor.R
import name.levis.ichor.data.VirtualMachineRepository
import name.levis.ichor.model.VirtualMachine
import name.levis.ichor.model.VmAction
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.uiText

/** Outcome of [action] on the VM [target] ("namespace/name"): the migration it started, or the error. */
data class VmActionOutcome(val action: VmAction, val force: Boolean, val target: String, val migration: String, val error: UiText?)

/**
 * Start, stop, restart, pause, unpause and migrate, run in [scope] (a ViewModel's). [onChanged]
 * runs after one went through: the list follows the VM's new status.
 */
class VmActions(
    private val scope: CoroutineScope,
    private val repository: VirtualMachineRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<VmActionOutcome>(scope)

    /** Keys ([VirtualMachine.key]) of the VMs with an action in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy
    val results: Flow<VmActionOutcome> get() = actions.results

    fun run(vm: VirtualMachine, action: VmAction) {
        actions.launch(vm.key, { repository.vmAction(vm.namespace, vm.name, action) }, {
            VmActionOutcome(action, false, vm.key, it.getOrNull()?.migration.orEmpty(), it.exceptionOrNull()?.uiText())
        }, onChanged)
    }

    /** Stops [vm] at once, without the guest's grace period (a hung guest). */
    fun forceStop(vm: VirtualMachine) {
        actions.launch(vm.key, { repository.vmStopForce(vm.namespace, vm.name, 0) }, {
            VmActionOutcome(VmAction.STOP, true, vm.key, "", it.exceptionOrNull()?.uiText())
        }, onChanged)
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun VmResultToasts(results: Flow<VmActionOutcome>) = ResultToasts(results) { context, r ->
    val verb = context.getString(r.action.label)
    val text = r.error?.resolve(context)?.let { context.getString(R.string.vm_action_failed, verb, r.target, it) }
        ?: when {
            r.action == VmAction.MIGRATE && r.migration.isNotEmpty() -> context.getString(R.string.vm_migration_started, r.target, r.migration)
            r.force -> context.getString(R.string.vm_force_stop_requested, r.target)
            else -> context.getString(R.string.vm_action_requested, verb, r.target)
        }
    text to (r.error != null)
}

/** The action's button label. */
val VmAction.label: Int
    get() = when (this) {
        VmAction.START -> R.string.vm_start
        VmAction.STOP -> R.string.vm_stop
        VmAction.RESTART -> R.string.vm_restart
        VmAction.MIGRATE -> R.string.vm_migrate
        VmAction.PAUSE -> R.string.vm_pause
        VmAction.UNPAUSE -> R.string.vm_unpause
    }
