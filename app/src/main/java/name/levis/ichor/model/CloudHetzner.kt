package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_hetzner.go.

/** What a Hetzner token may do. */
@Serializable
data class HetznerTokenCheck(val readOnly: Boolean = false, val servers: Int = 0)

/** What a server can be created from; prices before tax, in [currency]. */
@Serializable
data class HetznerOptions(
    val currency: String = "",
    val serverTypes: List<HetznerServerType> = emptyList(),
    val locations: List<HetznerLocation> = emptyList(),
    val snapshots: List<HetznerSnapshot> = emptyList(),
)

@Serializable
data class HetznerServerType(
    val name: String = "",
    val cores: Int = 0,
    val memoryGB: Double = 0.0,
    val diskGB: Double = 0.0,
    val arch: String = "",
    val prices: List<HetznerPrice> = emptyList(),
) {
    fun price(location: String): HetznerPrice? = prices.firstOrNull { it.location == location }
}

@Serializable
data class HetznerPrice(val location: String = "", val hourly: String = "", val monthly: String = "")

@Serializable
data class HetznerLocation(val name: String = "", val description: String = "", val city: String = "", val country: String = "")

@Serializable
data class HetznerSnapshot(
    val id: Long = 0,
    val description: String = "",
    val created: String = "",
    val arch: String = "",
    val talos: Boolean = false,
)

/** StartHetznerCreate's specJSON. Never holds the token. */
@Serializable
data class HetznerCreateSpec(
    val serverType: String,
    val location: String,
    val snapshotId: Long,
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
)

/** The snapshots a server of [type] can start from, Talos ones first as the core lists them. */
fun HetznerOptions.snapshotsFor(type: HetznerServerType?): List<HetznerSnapshot> =
    if (type == null) emptyList() else snapshots.filter { it.arch == type.arch }
