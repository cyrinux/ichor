package name.levis.ichor.ui.talosvms

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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.TalosVm
import name.levis.ichor.model.TalosVmDisk
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.vms.VmStateTag
import name.levis.ichor.util.formatBytes

/**
 * One Talos VM on [hostname]: its state and the state it should be in, its stage and error,
 * CPUs and memory, addresses and interfaces, and its disks with their pool, size and phase.
 * Read-only for now: the power and console actions go in a row under the details.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TalosVmSheet(vm: TalosVm, hostname: String, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(vm.name, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f, fill = false))
                VmStateTag(vm.stateLabel, vm.vmState)
            }
            MutedText(hostname)
            if (vm.error.isNotBlank()) Text(vm.error, style = MaterialTheme.typography.bodyMedium, color = LocalStatusColors.current.bad)
            Details(vm)
            Network(vm)
            if (vm.disks.isNotEmpty()) {
                SectionTitle(stringResource(R.string.vm_disks), Modifier.padding(top = 8.dp))
                vm.disks.forEach { DiskRow(it) }
            }
        }
    }
}

@Composable
private fun Details(vm: TalosVm) {
    InfoRow(stringResource(R.string.talos_vms_state), vm.stateLabel, mono = true)
    if (vm.wantedPowerState.isNotEmpty()) InfoRow(stringResource(R.string.talos_vms_wanted_state), vm.wantedPowerState, mono = true)
    if (vm.stage.isNotEmpty()) {
        InfoRow(stringResource(R.string.talos_vms_stage), vm.stage, mono = true, valueColor = if (vm.stage == "error") LocalStatusColors.current.bad else Color.Unspecified)
    }
    talosVmResources(vm)?.let { InfoRow(stringResource(R.string.vm_resources), it) }
    if (vm.profile.isNotEmpty()) InfoRow(stringResource(R.string.talos_vms_profile), vm.profile, mono = true)
}

@Composable
private fun Network(vm: TalosVm) {
    if (vm.addresses.isNotEmpty()) InfoRow(stringResource(R.string.vm_addresses), vm.addresses.joinToString(", "), mono = true)
    if (vm.interfaces.isEmpty()) return
    SectionTitle(stringResource(R.string.talos_vms_interfaces), Modifier.padding(top = 8.dp))
    vm.interfaces.forEach { nic ->
        InfoRow(nic.name.ifEmpty { "-" }, nic.mac, mono = true)
        if (nic.addresses.isNotEmpty()) MutedText(nic.addresses.joinToString(", "))
    }
}

/** "root  vm-pool · 40.0 GiB · ready", its volume and format under it, its error in red. */
@Composable
private fun DiskRow(disk: TalosVmDisk) {
    val colors = LocalStatusColors.current
    val summary = listOfNotNull(
        disk.pool.ifEmpty { null },
        disk.sizeBytes.takeIf { it > 0 }?.let { formatBytes(it) },
        disk.phase.ifEmpty { null },
    ).joinToString(" · ")
    val bad = disk.error.isNotBlank() || disk.phase == "notReady"
    InfoRow(disk.name, summary, mono = true, valueColor = if (bad) colors.bad else Color.Unspecified)
    val detail = listOfNotNull(
        disk.volume.ifEmpty { null },
        disk.format.ifEmpty { null },
        if (disk.readOnly) stringResource(R.string.talos_vms_disk_read_only) else null,
    )
    if (detail.isNotEmpty()) MutedText(detail.joinToString(" · "))
    if (disk.error.isNotBlank()) Text(disk.error, style = MaterialTheme.typography.bodySmall, color = colors.bad)
}
