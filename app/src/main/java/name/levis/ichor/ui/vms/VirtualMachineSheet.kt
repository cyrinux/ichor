package name.levis.ichor.ui.vms

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.VirtualMachine
import name.levis.ichor.model.VmAction
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * One VM: its status, resources, addresses, conditions, disks and migration, then the actions
 * its state allows, each also gated by what the credentials may do. Stop and restart ask for the
 * VM's name typed; migrate names the node it leaves. [onConsole] opens its serial console.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VirtualMachineSheet(vm: VirtualMachine, busy: Boolean, actions: VmActions, onConsole: (VirtualMachine) -> Unit, onDismiss: () -> Unit) {
    var confirming by remember(vm.key) { mutableStateOf<VmAction?>(null) }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(vm.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
                VmStatusBadge(vm)
            }
            MutedText(vm.namespace)
            vmProgressText(vm.progress)?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = LocalStatusColors.current.warn) }
            Details(vm)
            ActionRow(vm, busy, onAction = { confirming = it })
            ConsoleRow(vm, onConsole = { onConsole(vm) })
        }
    }
    confirming?.let { action ->
        VmConfirm(vm, action, onDismiss = { confirming = null }, onConfirm = { force ->
            confirming = null
            if (force) actions.forceStop(vm) else actions.run(vm, action)
        })
    }
}

@Composable
private fun Details(vm: VirtualMachine) {
    val colors = LocalStatusColors.current
    InfoRow(stringResource(R.string.vm_run_strategy), vm.runStrategy.ifEmpty { "-" }, mono = true)
    if (vm.pendingChange.isNotEmpty()) InfoRow(stringResource(R.string.vm_pending_change), vm.pendingChange)
    if (vm.cpu > 0 || vm.memory.isNotEmpty()) {
        InfoRow(stringResource(R.string.vm_resources), listOfNotNull(vm.cpu.takeIf { it > 0 }?.let { stringResource(R.string.vm_vcpus, it) }, vm.memory.ifEmpty { null }).joinToString("  ·  "))
    }
    if (vm.node.isNotEmpty()) InfoRow(stringResource(R.string.vm_node), vm.node, mono = true)
    if (vm.ips.isNotEmpty()) InfoRow(stringResource(R.string.vm_addresses), vm.ips.joinToString(", "), mono = true)
    if (vm.guestOS.isNotEmpty()) InfoRow(stringResource(R.string.vm_guest_os), vm.guestOS)
    if (vm.phase.isNotEmpty()) InfoRow(stringResource(R.string.vm_phase), vm.phase)
    if (vm.launcherImage.isNotEmpty()) InfoRow(stringResource(R.string.vm_launcher), vm.launcherImage, mono = true)
    if (vm.conditions.isNotEmpty()) {
        SectionTitle(stringResource(R.string.vm_conditions), Modifier.padding(top = 8.dp))
        vm.conditions.forEach { c ->
            val bad = (c.type == "Failure" && c.status == "True") || (c.type == "Ready" && c.status == "False")
            InfoRow(c.type, listOf(c.status, c.reason).filter { it.isNotEmpty() }.joinToString(" · "), valueColor = if (bad) colors.bad else Color.Unspecified)
            if (c.message.isNotEmpty()) MutedText(c.message)
        }
    }
    if (vm.disks.isNotEmpty()) {
        SectionTitle(stringResource(R.string.vm_disks), Modifier.padding(top = 8.dp))
        vm.disks.forEach { d -> InfoRow(d.name, listOf(d.phase, d.progress).filter { it.isNotEmpty() && it != "N/A" }.joinToString(" · "), mono = true) }
    }
    vm.migration?.let { m ->
        SectionTitle(stringResource(R.string.vm_migration), Modifier.padding(top = 8.dp))
        InfoRow(m.name, m.phase, mono = true, valueColor = if (m.failed) colors.bad else Color.Unspecified)
        MutedText(stringResource(R.string.vm_migration_nodes, m.sourceNode.ifEmpty { "?" }, m.targetNode.ifEmpty { "?" }))
    }
}

/** The actions in a wrapping row: enabled when the state allows and the credentials may. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ActionRow(vm: VirtualMachine, busy: Boolean, onAction: (VmAction) -> Unit) {
    // Pause and unpause share one button: the state says which one it is.
    val shown = listOf(VmAction.START, VmAction.STOP, VmAction.RESTART, if (vm.allows(VmAction.UNPAUSE)) VmAction.UNPAUSE else VmAction.PAUSE, VmAction.MIGRATE)
    val denials = shown.associateWith { rememberKubeDenial(it.kubeAction, vm.namespace) }
    SectionTitle(stringResource(R.string.vm_actions), Modifier.padding(top = 8.dp))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        shown.forEach { action ->
            IchorOutlinedButton(onClick = { onAction(action) }, enabled = !busy && vm.allows(action) && denials[action] == null) {
                Text(stringResource(action.label))
            }
        }
    }
    // Why an action the state allows is still off: the credentials.
    shown.firstNotNullOfOrNull { a -> denials[a].takeIf { vm.allows(a) } }?.let { KubeDenialNote(it) }
    if (busy) MutedText(stringResource(R.string.vm_action_running))
}

/** The serial console: only a running instance has one; the credentials need its subresource. */
@Composable
private fun ConsoleRow(vm: VirtualMachine, onConsole: () -> Unit) {
    val denial = rememberKubeDenial(KubeAction.VM_CONSOLE, vm.namespace)
    val running = vm.phase == "Running"
    SectionTitle(stringResource(R.string.vm_console_title), Modifier.padding(top = 8.dp))
    IchorOutlinedButton(onClick = onConsole, enabled = running && denial == null) { Text(stringResource(R.string.vm_console)) }
    if (!running) MutedText(stringResource(R.string.vm_console_start_first)) else KubeDenialNote(denial)
}

/**
 * Stop and restart: the VM's name typed (a stop under runStrategy Always writes Halted into
 * the spec); stop can skip the guest's grace period. Migrate names the node it leaves; start,
 * pause and unpause ask plainly. [onConfirm] tells whether a forced stop was chosen.
 */
@Composable
private fun VmConfirm(vm: VirtualMachine, action: VmAction, onDismiss: () -> Unit, onConfirm: (force: Boolean) -> Unit) {
    val denial = rememberKubeDenial(action.kubeAction, vm.namespace)
    val verb = stringResource(action.label)
    when (action) {
        VmAction.STOP, VmAction.RESTART -> {
            var force by remember { mutableStateOf(false) }
            HostnameConfirmDialog(
                title = stringResource(R.string.vm_confirm_title, verb, vm.name),
                hostname = vm.name,
                confirmLabel = verb,
                enabled = denial == null,
                emphasized = true,
                onConfirm = { onConfirm(action == VmAction.STOP && force) },
                onDismiss = onDismiss,
            ) {
                Text(stringResource(if (action == VmAction.STOP) R.string.vm_stop_text else R.string.vm_restart_text))
                if (action == VmAction.STOP && vm.runStrategy == "Always") Text(stringResource(R.string.vm_stop_halted))
                if (action == VmAction.STOP) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = force, onCheckedChange = { force = it })
                        Text(stringResource(R.string.vm_force_stop))
                    }
                }
                denial?.let { KubeDenialNote(it) }
            }
        }
        VmAction.MIGRATE -> ConfirmDialog(
            title = stringResource(R.string.vm_confirm_title, verb, vm.name),
            text = stringResource(R.string.vm_migrate_text, vm.node.ifEmpty { "?" }),
            confirm = verb,
            onConfirm = { onConfirm(false) },
            onDismiss = onDismiss,
        )
        else -> ConfirmDialog(
            title = stringResource(R.string.vm_confirm_title, verb, vm.name),
            text = stringResource(if (action == VmAction.PAUSE) R.string.vm_pause_text else R.string.vm_simple_text),
            confirm = verb,
            onConfirm = { onConfirm(false) },
            onDismiss = onDismiss,
        )
    }
}
