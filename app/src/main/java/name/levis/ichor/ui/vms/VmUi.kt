package name.levis.ichor.ui.vms

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import name.levis.ichor.R
import name.levis.ichor.model.VirtualMachine
import name.levis.ichor.model.VmProgress
import name.levis.ichor.model.VmState
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors

/** The badge colour of a VM's state: failing red, on its way amber, running green, stopped muted. */
@Composable
fun VmState.color(): Color {
    val colors = LocalStatusColors.current
    return when (this) {
        VmState.FAILING -> colors.bad
        VmState.CHANGING -> colors.warn
        VmState.RUNNING -> colors.ok
        VmState.STOPPED -> colors.muted
    }
}

/** KubeVirt's status as written, coloured by state, read out with its meaning. */
@Composable
fun VmStatusBadge(vm: VirtualMachine, modifier: Modifier = Modifier) {
    val description = stringResource(R.string.vm_status_description, vm.status.ifEmpty { "-" })
    Tag(vm.status.ifEmpty { "-" }, vm.vmState.color(), modifier.semantics { contentDescription = description })
}

/** "Importing win11-root 62.00%" or "Migrating worker-2 → worker-3", null when nothing is in flight. */
@Composable
fun vmProgressText(progress: VmProgress?): String? = when (progress) {
    is VmProgress.Importing -> stringResource(R.string.vm_importing, progress.disk, progress.progress.ifEmpty { "…" })
    is VmProgress.Migrating -> stringResource(R.string.vm_migrating, progress.from.ifEmpty { "?" }, progress.to.ifEmpty { "?" })
    null -> null
}
