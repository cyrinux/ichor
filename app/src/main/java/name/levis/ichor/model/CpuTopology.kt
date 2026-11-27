package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/hardware_cpu.go: Talos 1.15's NUMATopology and CPUScalingStatus.

/** One NUMA node of the host: its CPUs as ranges ("0-15,32-47") and its memory. */
@Serializable
data class NumaNode(
    val id: Int = 0,
    val cpus: String = "",
    val cpuCount: Int = 0,
    val memoryBytes: Long = 0,
)

/** One cpufreq policy: the CPUs it drives, its governor and the frequency window it allows. */
@Serializable
data class CpuScalingPolicy(
    val policy: String = "",
    val cpus: String = "",
    val driver: String = "",
    val governor: String = "",
    /** "" unless the driver has energy performance preferences. */
    val energyPreference: String = "",
    val minKhz: Long = 0,
    val maxKhz: Long = 0,
    /** performance or efficiency on hybrid CPUs, "" otherwise. */
    val coreType: String = "",
    /** The CPUScalingConfig document that set the policy, "" when none did. */
    val configuredBy: String = "",
)

const val GOVERNOR_POWERSAVE = "powersave"

/** The NUMA card shows only on a machine split in more than one node (or when its read failed). */
val NodeHardware.showsNuma: Boolean get() = numa.size > 1 || errors[HardwareSection.NUMA] != null

/** The CPU scaling card: policies to show, or a read that failed. Absent before Talos 1.15 and on VMs. */
val NodeHardware.showsCpuScaling: Boolean get() = cpuScaling.isNotEmpty() || errors[HardwareSection.CPU_SCALING] != null

/** A powersave policy on a node that is throttled: the likely reason it runs slow. */
fun NodeHardware.powersaveWhileThrottled(throttled: Boolean): Boolean =
    throttled && cpuScaling.any { it.governor == GOVERNOR_POWERSAVE }
