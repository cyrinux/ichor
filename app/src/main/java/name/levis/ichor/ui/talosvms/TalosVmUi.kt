package name.levis.ichor.ui.talosvms

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.TalosFeature
import name.levis.ichor.model.TalosNodeVms
import name.levis.ichor.model.TalosVm
import name.levis.ichor.model.talosVmCounts
import name.levis.ichor.ui.components.rememberClusterFeatures
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.vms.VmCountChip
import name.levis.ichor.ui.vms.VmCountChips
import name.levis.ichor.util.formatBytes

/** "4 vCPU · 8.0 GiB", null when the spec sets neither. */
@Composable
internal fun talosVmResources(vm: TalosVm): String? = listOfNotNull(
    vm.cpus.takeIf { it > 0 }?.let { stringResource(R.string.vm_vcpus, it.toInt()) },
    vm.memoryBytes.takeIf { it > 0 }?.let { formatBytes(it) },
).takeIf { it.isNotEmpty() }?.joinToString("  ·  ")

/** The running, stopped, failing and pending chips of [vms], the KubeVirt list's shapes. */
@Composable
internal fun TalosVmCountChips(vms: List<TalosVm>, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val c = talosVmCounts(vms)
    VmCountChips(
        listOf(
            VmCountChip(c.running, R.string.vm_count_running, colors.ok),
            VmCountChip(c.stopped, R.string.vm_count_stopped, colors.muted),
            VmCountChip(c.failing, R.string.vm_count_failing, colors.bad),
            VmCountChip(c.changing, R.string.talos_vms_count_pending, colors.warn),
        ),
        modifier,
    )
}

/** "1 running · 1 stopped · 1 failing": the node screen's row under its title. */
@Composable
fun talosVmSummary(vms: List<TalosVm>): String {
    val c = talosVmCounts(vms)
    return listOfNotNull(
        c.running.takeIf { it > 0 }?.let { stringResource(R.string.vm_count_running, it) },
        c.stopped.takeIf { it > 0 }?.let { stringResource(R.string.vm_count_stopped, it) },
        c.failing.takeIf { it > 0 }?.let { stringResource(R.string.vm_count_failing, it) },
        c.changing.takeIf { it > 0 }?.let { stringResource(R.string.talos_vms_count_pending, it) },
    ).joinToString(" · ")
}

/** Whether [features] say the node's Talos has the hypervisor: known and supported, not a guess. */
private fun NodeFeatures?.hasHypervisor(): Boolean = this?.features?.get(TalosFeature.HYPERVISOR.key)?.supported == true

/**
 * [node]'s VMs once its features say its Talos has the hypervisor (1.15), read again on each
 * invalidation; null before, on an older Talos, or when the read fails.
 */
@Composable
fun rememberNodeTalosVms(node: String, features: NodeFeatures?): TalosNodeVms? {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val invalidations by talos.invalidations.collectAsStateWithLifecycle()
    val supported = features.hasHypervisor()
    val report by produceState<TalosNodeVms?>(null, node, supported, invalidations) {
        value = if (supported) runCatching { talos.nodeVirtualMachines(node) }.getOrNull() else null
    }
    return report
}

/**
 * Whether the cluster entry shows (the rule of the KubeVirt entry): once a reachable node's
 * Talos has the hypervisor and some node reports a VM. Asked only while [active], again on
 * each [key] (an overview load); false until then, and when the read fails.
 */
@Composable
fun rememberClusterHasTalosVms(active: Boolean, key: Any?): Boolean {
    val talos = (LocalContext.current.applicationContext as TalosApp).talosRepository
    val features = rememberClusterFeatures()
    val supported = active && features.any { it.hasHypervisor() }
    val found by produceState(false, supported, key) {
        value = supported && runCatching { talos.clusterVirtualMachines().hasVms }.getOrDefault(false)
    }
    return supported && found
}
