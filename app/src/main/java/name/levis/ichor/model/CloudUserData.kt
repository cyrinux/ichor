package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/boot_userdata.go.

/**
 * The user data of a new node on a cloud host (Hetzner, OpenStack, Scaleway): pasted when the
 * server is created, read by Talos at first boot. [userData] is a credential: Omni's join config
 * (its join token) or a new member's full config with the cluster's secrets.
 */
@Serializable
data class CloudUserData(
    /** "omni": Omni's join config; "talos": a new member's config made from [templateNode]'s. */
    val kind: String = "",
    val userData: String = "",
    val size: Int = 0,
    /** The 32 KiB most clouds accept as user data; [size] above it comes with a warning. */
    val limit: Int = 0,
    /** [userData] with its secrets hidden: what the screen shows. */
    val redactedPreview: String = "",
    val role: String = "",
    val hostname: String = "",
    val templateNode: String = "",
    val talosVersion: String = "",
    val schematic: String = "",
    val images: List<CloudImage> = emptyList(),
    val warnings: List<String> = emptyList(),
) {
    val isOmni: Boolean get() = kind == "omni"
}

/** The Image Factory disk image to create the server (or its snapshot) from on [platform]. */
@Serializable
data class CloudImage(
    val platform: String = "",
    val arch: String = "",
    val url: String = "",
)

/** The provider a cloud image is for, as people know it. */
val CloudImage.providerLabel: String
    get() = when (platform) {
        "hcloud" -> "Hetzner"
        "openstack" -> "OpenStack"
        "scaleway" -> "Scaleway"
        else -> platform
    }
