package name.levis.ichor.ui.vms

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.VirtualMachine
import name.levis.ichor.model.VmProgress
import name.levis.ichor.model.VmState
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors

// Shared by the KubeVirt screens (ui/vms) and the Talos hypervisor's (ui/talosvms).

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

/** A VM's status as its platform writes it ([label]), coloured by [state], read out with its meaning. */
@Composable
fun VmStateTag(label: String, state: VmState, modifier: Modifier = Modifier) {
    val text = label.ifEmpty { "-" }
    val description = stringResource(R.string.vm_status_description, text)
    Tag(text, state.color(), modifier.semantics { contentDescription = description })
}

/** KubeVirt's status as written, coloured by state, read out with its meaning. */
@Composable
fun VmStatusBadge(vm: VirtualMachine, modifier: Modifier = Modifier) = VmStateTag(vm.status, vm.vmState, modifier)

/** One count chip: how many, its "%1$d running" label, its colour. */
data class VmCountChip(val count: Int, @StringRes val label: Int, val color: Color)

/** The count chips of a VM list; only the non-zero ones, nothing when all are zero. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun VmCountChips(chips: List<VmCountChip>, modifier: Modifier = Modifier) {
    val shown = chips.filter { it.count > 0 }
    if (shown.isEmpty()) return
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        shown.forEach { Tag(stringResource(it.label, it.count), it.color) }
    }
}

/** "Importing win11-root 62.00%" or "Migrating worker-2 → worker-3", null when nothing is in flight. */
@Composable
fun vmProgressText(progress: VmProgress?): String? = when (progress) {
    is VmProgress.Importing -> stringResource(R.string.vm_importing, progress.disk, progress.progress.ifEmpty { "…" })
    is VmProgress.Migrating -> stringResource(R.string.vm_migrating, progress.from.ifEmpty { "?" }, progress.to.ifEmpty { "?" })
    null -> null
}
