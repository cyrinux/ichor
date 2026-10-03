package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/hardware.go.

@Serializable
data class NodeHardware(
    /** Null when unavailable. */
    val system: SystemInfo? = null,
    val processors: List<ProcessorInfo> = emptyList(),
    val memory: List<MemoryModule> = emptyList(),
    val disks: List<DiskInfo> = emptyList(),
    val extensions: List<ExtensionInfo> = emptyList(),
    /** Null when unavailable. */
    val security: SecurityInfo? = null,
    /** Section ("system", "processors", "memory", "disks", "extensions", "security") -> error. */
    val errors: Map<String, String> = emptyMap(),
)

@Serializable
data class SystemInfo(
    val manufacturer: String = "",
    val product: String = "",
    val version: String = "",
    val serial: String = "",
    val uuid: String = "",
    val sku: String = "",
    val biosVersion: String = "",
)

@Serializable
data class ProcessorInfo(
    val socket: String = "",
    val manufacturer: String = "",
    val model: String = "",
    val cores: Int = 0,
    val threads: Int = 0,
    val maxSpeedMhz: Int = 0,
    val bootSpeedMhz: Int = 0,
)

@Serializable
data class MemoryModule(
    val slot: String = "",
    val bank: String = "",
    val sizeMib: Long = 0,
    val type: String = "",
    /** MT/s. */
    val speed: Int = 0,
    val manufacturer: String = "",
    val serial: String = "",
)

@Serializable
data class DiskInfo(
    val name: String,
    val devPath: String = "",
    val model: String = "",
    val serial: String = "",
    /** Bytes. */
    val size: Long = 0,
    /** ssd, hdd, nvme, sd or unknown. */
    val type: String = "",
    val wwid: String = "",
    val busPath: String = "",
    val systemDisk: Boolean = false,
    val readonly: Boolean = false,
)

@Serializable
data class ExtensionInfo(
    val name: String,
    val version: String = "",
    val author: String = "",
    val description: String = "",
)

@Serializable
data class SecurityInfo(
    val secureBoot: Boolean = false,
    val bootedWithUki: Boolean = false,
    val ukiSigningKeyFingerprint: String = "",
    val pcrSigningKeyFingerprint: String = "",
    val selinuxState: String = "",
    val fipsState: String = "",
    val moduleSignatureEnforced: Boolean = false,
)

object HardwareSection {
    const val SYSTEM = "system"
    const val PROCESSORS = "processors"
    const val MEMORY = "memory"
    const val DISKS = "disks"
    const val EXTENSIONS = "extensions"
    const val SECURITY = "security"
}

/** Installed memory in bytes (sum of the populated modules). */
val NodeHardware.totalMemoryBytes: Long get() = memory.sumOf { it.sizeMib } * 1024 * 1024

/** Disks with the system disk first, then by name. */
val NodeHardware.sortedDisks: List<DiskInfo>
    get() = disks.sortedWith(compareByDescending<DiskInfo> { it.systemDisk }.thenBy { it.name })

/** Total cores and threads over all sockets. */
val NodeHardware.totalCores: Int get() = processors.sumOf { it.cores }
val NodeHardware.totalThreads: Int get() = processors.sumOf { it.threads }
