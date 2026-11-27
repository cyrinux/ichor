package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * A Talos VM that should run and does not, as Go judged it (go/ichorgo/talos_vm_problems.go):
 * [kind] vmDown, vmError or vmDiskFailed; [detail] the node's own words, clipped.
 */
@Serializable
data class TalosVmProblem(
    val node: String = "",
    val vm: String = "",
    val kind: String = "",
    val detail: String = "",
)

/** One node of [TalosVmProblemReport]: its problems, or [error] when it could not be read. */
@Serializable
data class TalosVmProblemNode(
    val node: String = "",
    val hostname: String = "",
    val supported: Boolean = false,
    val problems: List<TalosVmProblem> = emptyList(),
    /** Section (vms, specs, disks, libraries) to its error, for the sections that failed. */
    val errors: Map<String, String> = emptyMap(),
    val error: String = "",
)

/** What the monitor reads of ClusterVirtualMachines: every node's problems, not its VMs. */
@Serializable
data class TalosVmProblemReport(val context: String = "", val nodes: List<TalosVmProblemNode> = emptyList())
