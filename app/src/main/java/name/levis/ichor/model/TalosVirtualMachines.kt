package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/talos_vms.go (NodeVirtualMachines, ClusterVirtualMachines): the virtual
// machines the Talos hypervisor runs (Talos 1.15), read through the Talos API (os:reader).

/** One node's VMs, their disks and the node's content libraries. */
@Serializable
data class TalosNodeVms(
    /** False on a node without the hypervisor resources (Talos older than 1.15), with [reason]. */
    val supported: Boolean = false,
    val reason: String = "",
    val vms: List<TalosVm> = emptyList(),
    val libraries: List<TalosContentLibrary> = emptyList(),
    /** Section (vms, specs, disks, libraries) to its error, for the sections that failed. */
    val errors: Map<String, String> = emptyMap(),
    /** The VMs that should run and do not, as Go judged them (the background alert's). */
    val problems: List<TalosVmProblem> = emptyList(),
) {
    /** Whether the node has something to show: the entries are hidden otherwise. */
    val hasVms: Boolean get() = supported && vms.isNotEmpty()
}

@Serializable
data class TalosVm(
    val name: String = "",
    /** Observed: unknown, running or stopped. */
    val powerState: String = "",
    /** The spec's intent, "" without a spec. */
    val wantedPowerState: String = "",
    /** unknown, pending, ready or error. */
    val stage: String = "",
    val error: String = "",
    val running: Boolean = false,
    val cpus: Long = 0,
    val memoryBytes: Long = 0,
    /** "" is the builtin host profile. */
    val profile: String = "",
    /** CIDRs. */
    val addresses: List<String> = emptyList(),
    val interfaces: List<TalosVmInterface> = emptyList(),
    val disks: List<TalosVmDisk> = emptyList(),
) {
    /**
     * How the list groups it, with the KubeVirt badge colours: failing when it or a disk reports
     * an error, changing while pending or not yet in the wanted state, else running or stopped.
     */
    val vmState: VmState
        get() = when {
            stage == STAGE_ERROR || error.isNotBlank() || disks.any { it.error.isNotBlank() } -> VmState.FAILING
            stage == STAGE_PENDING || (wantedPowerState.isNotEmpty() && wantedPowerState != powerState) -> VmState.CHANGING
            running -> VmState.RUNNING
            else -> VmState.STOPPED
        }

    /** The observed state as the node writes it, "-" when unknown. */
    val stateLabel: String get() = powerState.ifEmpty { "-" }

    /** The first address without its prefix length, for the row. */
    val firstAddress: String? get() = addresses.firstOrNull()?.substringBefore('/')

    private companion object {
        const val STAGE_ERROR = "error"
        const val STAGE_PENDING = "pending"
    }
}

@Serializable
data class TalosVmInterface(val name: String = "", val mac: String = "", val addresses: List<String> = emptyList())

@Serializable
data class TalosVmDisk(
    val name: String = "",
    val pool: String = "",
    val volume: String = "",
    val format: String = "",
    val sizeBytes: Long = 0,
    val readOnly: Boolean = false,
    /** unknown, notReady, ready or observationUnavailable. */
    val phase: String = "",
    val error: String = "",
)

@Serializable
data class TalosContentLibrary(val name: String = "", val path: String = "", val phase: String = "", val error: String = "")

/** One node of [TalosClusterVms]: its report, or [error] when it could not be read. */
@Serializable
data class TalosNodeVmsEntry(
    val node: String = "",
    val hostname: String = "",
    val supported: Boolean = false,
    val reason: String = "",
    val vms: List<TalosVm> = emptyList(),
    val libraries: List<TalosContentLibrary> = emptyList(),
    val errors: Map<String, String> = emptyMap(),
    val error: String = "",
    val problems: List<TalosVmProblem> = emptyList(),
) {
    val report: TalosNodeVms get() = TalosNodeVms(supported, reason, vms, libraries, errors, problems)
}

/** Every node's VMs, read in parallel; a node that does not answer carries an error. */
@Serializable
data class TalosClusterVms(val context: String = "", val nodes: List<TalosNodeVmsEntry> = emptyList()) {
    /** The rule of the entries: shown once a node reports the hypervisor and at least one VM. */
    val hasVms: Boolean get() = nodes.any { it.report.hasVms }

    /**
     * The nodes the list shows, by hostname: those with VMs, those that failed, those whose Talos
     * cannot tell (a 1.15 node next to older ones); a supported node without VMs is left out.
     */
    val shown: List<TalosNodeVmsEntry>
        get() = nodes.filter { it.vms.isNotEmpty() || it.error.isNotEmpty() || !it.supported }
            .sortedBy { it.hostname.ifEmpty { it.node } }

    val all: List<TalosVm> get() = nodes.flatMap { it.vms }

    /** Every node's problems, with the node they are on. */
    val problems: List<Pair<TalosNodeVmsEntry, TalosVmProblem>> get() = nodes.flatMap { n -> n.problems.map { n to it } }

    /** Something is on its way: worth polling. */
    val busy: Boolean get() = all.any { it.vmState == VmState.CHANGING }

    companion object {
        /** One node's report as a cluster of one, so the list and its sheet serve both. */
        fun ofNode(node: String, hostname: String, report: TalosNodeVms) = TalosClusterVms(
            nodes = listOf(
                TalosNodeVmsEntry(
                    node, hostname, report.supported, report.reason, report.vms, report.libraries, report.errors,
                    problems = report.problems,
                ),
            ),
        )
    }
}

/** Running, stopped, failing and changing counts of [vms], in that order. */
data class TalosVmCounts(val running: Int, val stopped: Int, val failing: Int, val changing: Int)

fun talosVmCounts(vms: List<TalosVm>): TalosVmCounts {
    val states = vms.groupingBy { it.vmState }.eachCount()
    return TalosVmCounts(
        running = states[VmState.RUNNING] ?: 0,
        stopped = states[VmState.STOPPED] ?: 0,
        failing = states[VmState.FAILING] ?: 0,
        changing = states[VmState.CHANGING] ?: 0,
    )
}
