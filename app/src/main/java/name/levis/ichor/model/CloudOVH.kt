package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_ovh.go.

/** A service account, sealed on the device; what the core gets per call. */
@Serializable
data class OVHCredential(
    val endpoint: String = "eu",
    val clientId: String = "",
    val clientSecret: String = "",
    val project: String = "",
)

/** What a service account sees and may do; [checked] is false when the right could not be told. */
@Serializable
data class OVHCheck(
    val project: String = "",
    val projects: List<String> = emptyList(),
    val canCreate: Boolean = false,
    val checked: Boolean = false,
)

/** What an instance can be created from; prices before tax, in [currency], empty without the catalog. */
@Serializable
data class OVHOptions(
    val project: String = "",
    val currency: String = "",
    val regions: List<String> = emptyList(),
    val flavors: List<OVHFlavor> = emptyList(),
    val images: List<OVHImage> = emptyList(),
)

@Serializable
data class OVHFlavor(
    val id: String = "",
    val name: String = "",
    val region: String = "",
    val vcpus: Int = 0,
    val ramGB: Int = 0,
    val diskGB: Int = 0,
    val hourly: String = "",
)

@Serializable
data class OVHImage(
    val id: String = "",
    val name: String = "",
    val region: String = "",
    val private: Boolean = false,
    val talos: Boolean = false,
)

/** StartOVHCreate's specJSON. Never holds the account. */
@Serializable
data class OVHCreateSpec(
    val region: String,
    val flavorId: String,
    val imageId: String,
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
)

/** The flavors of [region], smallest first as the core lists them. */
fun OVHOptions.flavorsIn(region: String): List<OVHFlavor> = flavors.filter { it.region == region }

/** The images of [region], Talos ones first as the core lists them. */
fun OVHOptions.imagesIn(region: String): List<OVHImage> = images.filter { it.region == region }
