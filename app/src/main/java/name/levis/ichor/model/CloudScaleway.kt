package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_scaleway.go, cloud_scaleway_create.go, cloud_scaleway_image.go and cloud_scaleway_network.go.

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

/**
 * What an Instance can be created from in [zone]; prices before tax. [warnings] are the core's,
 * worded in English; [groupWarning] too, shown unless the picked group is the managed one.
 */
@Serializable
data class ScalewayOptions(
    val project: String = "",
    val zone: String = "",
    val zones: List<String> = emptyList(),
    val types: List<ScalewayType> = emptyList(),
    val images: List<ScalewayImage> = emptyList(),
    val securityGroups: List<ScalewaySecurityGroup> = emptyList(),
    val groupWarning: String = "",
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

/** A security group; [managed] when the app made it (the guided ichor-nodes). */
@Serializable
data class ScalewaySecurityGroup(
    val id: String = "",
    val name: String = "",
    val default: Boolean = false,
    val managed: Boolean = false,
)

/** ScalewayNetworkPrepare's specJSON: the zone, the role and whether the cluster is Omni (no port 50000 then). */
@Serializable
data class ScalewayNetworkSpec(val zone: String, val role: String, val omni: Boolean)

/**
 * The guided security group ichor-nodes, [created] by this call or reused, with its inbound
 * rules as they are now. [phoneIp] is the address port 50000 was opened to, "" on Omni or when
 * it could not be learned (a warning says so). [warnings] are the core's, worded in English.
 */
@Serializable
data class ScalewayNetwork(
    val securityGroupId: String = "",
    val name: String = "",
    val created: Boolean = false,
    val phoneIp: String = "",
    val rules: List<ScalewayNetworkRule> = emptyList(),
    val warnings: List<String> = emptyList(),
)

/**
 * One inbound rule: [protocol] all or tcp (udp, icmp on one added by hand), [port] 0 for every
 * port, [source] a CIDR; [description] the member's hostname, "this phone" or "".
 */
@Serializable
data class ScalewayNetworkRule(
    val protocol: String = "",
    val port: Int = 0,
    val source: String = "",
    val description: String = "",
) {
    /** Port 50000 opened to [phoneIp] by this call or an earlier one from the same address. */
    fun isPhone(phoneIp: String): Boolean = phoneIp.isNotBlank() && protocol == "tcp" && port == 50000 && source == "$phoneIp/32"

    /** Every protocol from one member, named by its hostname. */
    val isMember: Boolean get() = protocol == "all" && port == 0 && description.isNotBlank() && description != PHONE

    /** A rule the app did not word: "tcp/443 ← 198.51.100.0/24". */
    val plain: String get() = "$protocol${if (port == 0) "" else "/$port"} ← $source"

    private companion object {
        const val PHONE = "this phone"
    }
}

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

/** StartScalewayImage's specJSON. The image's URL comes from the template, never from here. */
@Serializable
data class ScalewayImageSpec(
    val templateNode: String = "",
    /** amd64 or arm64 (the core also reads x86_64). */
    val arch: String,
    val zone: String,
    /** "" for the core's cheapest type of the architecture that boots the helper. */
    val type: String = "",
    /** "" for talos-<version>-scaleway-<arch>-<schematic12>. */
    val name: String = "",
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

/** The managed group (ichor-nodes), else the project's default one, else the first; null when the zone lists none. */
val ScalewayOptions.defaultSecurityGroup: ScalewaySecurityGroup?
    get() = securityGroups.firstOrNull { it.managed } ?: securityGroups.firstOrNull { it.default } ?: securityGroups.firstOrNull()

/** "amd64" or "arm64" for Scaleway's "x86_64" / "arm64" (and the image run's own names). */
fun scalewayArch(arch: String): String = when (val a = arch.trim().lowercase()) {
    "x86_64", "x86-64", "amd64" -> "amd64"
    "arm64", "aarch64" -> "arm64"
    else -> a
}

/** The architectures an image can be made for: those of the zone's types, amd64 first. */
val ScalewayOptions.imageArches: List<String>
    get() = types.map { scalewayArch(it.arch) }.filter { it.isNotBlank() }.distinct().sortedBy { it != "amd64" }

/** A Talos image of [arch] is listed ([arch] "" for any architecture). */
fun ScalewayOptions.hasTalosImage(arch: String): Boolean =
    images.any { it.talos && (arch.isBlank() || scalewayArch(it.arch) == scalewayArch(arch)) }

/** "0.0099 EUR": the hourly price before tax; null when the core has none. */
val ScalewayType.hourlyPrice: String?
    get() = hourly.takeIf { it.isNotBlank() }?.let { if (currency.isBlank()) it else "$it $currency" }
