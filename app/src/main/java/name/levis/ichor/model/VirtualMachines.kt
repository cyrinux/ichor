package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_virt.go (KubeVirtualMachines) and kube_virt_actions.go (KubeVMAction).

/** The KubeVirt (and Harvester) virtual machines of the cluster, failing ones first. */
@Serializable
data class VirtualMachines(
    /** False: the cluster does not serve kubevirt.io (no error). */
    val installed: Boolean = false,
    val harvester: Boolean = false,
    val version: String = "",
    /** A list that failed; what could be read still shows. */
    val error: String = "",
    val vms: List<VirtualMachine> = emptyList(),
    val namespaces: List<String> = emptyList(),
    val counts: VmCounts = VmCounts(),
) {
    /** Something is on its way (starting, stopping, migrating, importing): worth polling. */
    val busy: Boolean get() = vms.any { it.vmState == VmState.CHANGING || it.progress != null }

    /** The VMs of [namespace], every one when null. */
    fun inNamespace(namespace: String?): List<VirtualMachine> = if (namespace == null) vms else vms.filter { it.namespace == namespace }
}

@Serializable
data class VmCounts(
    val running: Int = 0,
    val stopped: Int = 0,
    val failing: Int = 0,
    val migrating: Int = 0,
    val importing: Int = 0,
)

@Serializable
data class VirtualMachine(
    val namespace: String = "",
    val name: String = "",
    /** Always, Halted, Manual, RerunOnFailure, Once. */
    val runStrategy: String = "",
    /** KubeVirt's printable status as written (Running, Stopped, CrashLoopBackOff…). */
    val status: String = "",
    /** The Go core's grouping of [status]: failing, changing, running, stopped. */
    val state: String = "",
    val ready: Boolean = false,
    val created: Boolean = false,
    /** Start or Stop, "" when none. */
    val pendingChange: String = "",
    val conditions: List<VmCondition> = emptyList(),
    /** vCPUs, 0 when the template sets none. */
    val cpu: Int = 0,
    /** As written ("4Gi"), "" when unset. */
    val memory: String = "",
    val node: String = "",
    /** The instance's phase, "" without one. */
    val phase: String = "",
    val ips: List<String> = emptyList(),
    val guestOS: String = "",
    val launcherImage: String = "",
    /** The newest migration: in flight, or finished less than an hour ago. */
    val migration: VmMigration? = null,
    val disks: List<VmDisk> = emptyList(),
) {
    val key: String get() = "$namespace/$name"
    val firstIp: String? get() = ips.firstOrNull()
    val vmState: VmState get() = VmState.from(state)

    private val migrationInFlight: Boolean get() = migration?.let { !it.completed && !it.failed } == true

    /** A disk import or a migration in flight, the import first (the VM cannot boot without it). */
    val progress: VmProgress?
        get() = disks.firstOrNull { it.importing }?.let { VmProgress.Importing(it.name, it.progress) }
            ?: migration?.takeIf { migrationInFlight }?.let { VmProgress.Migrating(it.sourceNode, it.targetNode) }

    /**
     * Whether [action] may be asked in this state, as the Go core's vmRefusal answers; start is
     * also kept off while the instance runs (migrating, paused) or is going away (stopping).
     */
    fun allows(action: VmAction): Boolean = when (action) {
        VmAction.START -> status !in setOf("Running", "Starting", "Provisioning", "Migrating", "Paused", "Stopping")
        VmAction.STOP -> status != "Stopped" && status != "Stopping"
        VmAction.RESTART -> runStrategy != "Halted" && status == "Running"
        VmAction.MIGRATE -> !migrationInFlight && status == "Running"
        VmAction.PAUSE -> status == "Running" || status == "Migrating"
        VmAction.UNPAUSE -> status == "Paused"
    }
}

@Serializable
data class VmCondition(val type: String = "", val status: String = "", val reason: String = "", val message: String = "")

@Serializable
data class VmMigration(
    val name: String = "",
    val phase: String = "",
    val sourceNode: String = "",
    val targetNode: String = "",
    /** Unix ms. */
    val startedAt: Long = 0,
    val completed: Boolean = false,
    val failed: Boolean = false,
)

/** One DataVolume a VM boots from, with its import, clone or upload progress. */
@Serializable
data class VmDisk(
    val name: String = "",
    val phase: String = "",
    /** "45.00%", "N/A" or "". */
    val progress: String = "",
    val importing: Boolean = false,
)

/** What KubeVMAction answers: the migration a migrate started, "" otherwise. */
@Serializable
data class VmActionResult(val action: String = "", val namespace: String = "", val name: String = "", val migration: String = "")

/** How the list groups a VM's status (its badge colour). */
enum class VmState(val wire: String) {
    FAILING("failing"),
    CHANGING("changing"),
    RUNNING("running"),
    STOPPED("stopped"),
    ;

    companion object {
        fun from(wire: String): VmState = entries.firstOrNull { it.wire == wire } ?: CHANGING
    }
}

/** What a VM row says is in progress. */
sealed interface VmProgress {
    data class Importing(val disk: String, val progress: String) : VmProgress
    data class Migrating(val from: String, val to: String) : VmProgress
}

/** The actions KubeVMAction runs, with their wire name and their can-I check. */
enum class VmAction(val wire: String, val kubeAction: KubeAction) {
    START("start", KubeAction.VM_START),
    STOP("stop", KubeAction.VM_STOP),
    RESTART("restart", KubeAction.VM_RESTART),
    MIGRATE("migrate", KubeAction.VM_MIGRATE),
    PAUSE("pause", KubeAction.VM_PAUSE),
    UNPAUSE("unpause", KubeAction.VM_UNPAUSE),
}
