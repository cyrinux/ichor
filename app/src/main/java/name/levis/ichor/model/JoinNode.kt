package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/join_inspect.go.

/** What a node in maintenance mode (booted from the Talos ISO, not installed) says about itself. */
@Serializable
data class MaintenanceInspection(
    val address: String = "",
    val version: String = "",
    val arch: String = "",
    val platform: String = "",
    /** Null when unavailable. */
    val system: SystemInfo? = null,
    val disks: List<DiskInfo> = emptyList(),
    /** Physical links only. */
    val links: List<LinkInfo> = emptyList(),
    val addresses: List<AddressInfo> = emptyList(),
    /** False for a node already installed: it asks for a client certificate, and nothing was read. */
    val maintenance: Boolean = false,
    /** Section ("system", "disks", "links", "addresses") -> error. */
    val errors: Map<String, String> = emptyMap(),
) {
    /** The addresses of [link], as prefixes ("192.168.1.20/24"). */
    fun addressesOn(link: String): List<String> = addresses.filter { it.link == link }.map { it.address }
}

/** The address as typed, ready to send: no blanks; empty when there is nothing to inspect. */
fun joinAddress(typed: String): String = typed.trim()
