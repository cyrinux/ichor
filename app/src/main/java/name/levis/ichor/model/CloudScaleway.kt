package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_scaleway.go, cloud_scaleway_create.go and cloud_scaleway_image.go.

/** An API key, sealed on the device; what the core gets per call. [zone] "" is fr-par-1. */
@Serializable
data class ScalewayCredential(
    val accessKey: String = "",
    val secretKey: String = "",
    val project: String = "",
    val zone: String = "",
)

/** What an API key may do; [checked] is false when the create right could not be told. [account] is the access key id. */
@Serializable
data class ScalewayCheck(
    val project: String = "",
    val zone: String = "",
    val canCreate: Boolean = false,
    val checked: Boolean = false,
    val account: String = "",
)

/** What an Instance can be created from in [zone]; prices before tax. [warnings] are the core's, worded in English. */
@Serializable
data class ScalewayOptions(
    val project: String = "",
    val zone: String = "",
    val zones: List<String> = emptyList(),
    val types: List<ScalewayType> = emptyList(),
    val images: List<ScalewayImage> = emptyList(),
    val securityGroups: List<ScalewaySecurityGroup> = emptyList(),
    val warnings: List<String> = emptyList(),
)

/** A server type; [blockOnly] when it has no local storage (the data volume is Block Storage). */
@Serializable
data class ScalewayType(
    val name: String = "",
    val arch: String = "",
    val vcpus: Int = 0,
    val ramGB: Int = 0,
    val localDiskGB: Int = 0,
    val hourly: String = "",
    val currency: String = "",
    val blockOnly: Boolean = false,
)

@Serializable
data class ScalewayImage(
    val id: String = "",
    val name: String = "",
    val arch: String = "",
    val sizeGB: Int = 0,
    val talos: Boolean = false,
)

@Serializable
data class ScalewaySecurityGroup(
    val id: String = "",
    val name: String = "",
    val default: Boolean = false,
)

/** StartScalewayCreate's specJSON. Never holds the key. */
@Serializable
data class ScalewayCreateSpec(
    val zone: String,
    val type: String,
    val imageId: String,
    /** "" for the project's default security group. */
    val securityGroupId: String = "",
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
    /** On Omni, the labels to sign into the join token (omniLabelsJson); "" for none. */
    val labelsJson: String = "",
)

/** What a finished Talos image run at Scaleway reports. */
@Serializable
data class ScalewayImageResult(
    override val imageId: String = "",
    val snapshotId: String = "",
    override val name: String = "",
    val arch: String = "",
    val talosVersion: String = "",
    val schematic: String = "",
    val sizeGB: Int = 0,
) : MadeImage

/** The cost line of an image run: the helper's [type] and [hourly] price before tax; [note] is the core's. */
@Serializable
data class ScalewayImageCost(
    val type: String = "",
    val hourly: String = "",
    val minutes: Int = 0,
    val currency: String = "",
    val snapshotGB: Int = 0,
    val note: String = "",
)

/** The same key in another zone: options are read per zone. */
fun ScalewayCredential.inZone(zone: String): ScalewayCredential = copy(zone = zone)

/** The images, Talos ones first (in the core's order otherwise). */
val ScalewayOptions.imagesTalosFirst: List<ScalewayImage> get() = images.sortedBy { !it.talos }

/** The types an [image] can boot: those of its architecture; all of them before an image is picked. */
fun ScalewayOptions.typesFor(image: ScalewayImage?): List<ScalewayType> =
    if (image == null || image.arch.isBlank()) types else types.filter { it.arch == image.arch }

/** The project's default security group, else the first one; null when the zone lists none. */
val ScalewayOptions.defaultSecurityGroup: ScalewaySecurityGroup?
    get() = securityGroups.firstOrNull { it.default } ?: securityGroups.firstOrNull()

/** "0.0099 EUR": the hourly price before tax; null when the core has none. */
val ScalewayType.hourlyPrice: String?
    get() = hourly.takeIf { it.isNotBlank() }?.let { if (currency.isBlank()) it else "$it $currency" }
