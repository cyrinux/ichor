package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive

// Mirrors go/ichorgo/cloud_gcp.go, cloud_gcp_options.go, cloud_gcp_create.go and cloud_gcp_network.go.

/** A service account key, its project and zone, sealed on the device; what the core gets per call. */
@Serializable
data class GCPCredential(
    val serviceAccountJson: String = "",
    val project: String = "",
    val zone: String = "",
)

/** Whose key this is and what of the printed role it lacks; [checked] is false when that could not be told. */
@Serializable
data class GCPCheck(
    val account: String = "",
    val project: String = "",
    val canCreate: Boolean = false,
    val checked: Boolean = false,
    val missing: List<String> = emptyList(),
)

/** What a VM can be created from in [zone] of [project]; [warnings] are the core's, shown as they are. */
@Serializable
data class GCPOptions(
    val project: String = "",
    val zone: String = "",
    val projects: List<GCPProject> = emptyList(),
    val zones: List<GCPZone> = emptyList(),
    val machineTypes: List<GCPMachineType> = emptyList(),
    val networks: List<GCPNetwork> = emptyList(),
    val images: List<GCPImage> = emptyList(),
    val warnings: List<String> = emptyList(),
)

@Serializable
data class GCPProject(val id: String = "", val name: String = "")

@Serializable
data class GCPZone(val name: String = "", val region: String = "")

@Serializable
data class GCPMachineType(
    val name: String = "",
    val vcpus: Int = 0,
    val memoryGB: Double = 0.0,
    /** amd64 or arm64. */
    val arch: String = "",
)

@Serializable
data class GCPNetwork(
    val name: String = "",
    val selfLink: String = "",
    val autoSubnets: Boolean = false,
    /** Only the zone's region's. */
    val subnets: List<GCPSubnet> = emptyList(),
    /** A firewall rule lets the world reach a node's port 50000 (or every port): warned about, not refused. */
    val wideOpen: Boolean = false,
    /** The guided network's ichor-nodes-internal rule is on it. */
    val managed: Boolean = false,
)

/** GCPNetworkPrepare's specJSON: the role and whether the cluster is Omni (no port 50000 then). */
@Serializable
data class GCPNetworkSpec(val project: String, val zone: String, val role: String, val omni: Boolean)

/**
 * The guided network: the default network and its subnet of the zone's region, the firewall rules
 * [created] or [updated] by this call, and the rules aimed at the ichor-node tag as they are now.
 * [phoneIp] is the address port 50000 is open to, "" on Omni or when it could not be learned.
 */
@Serializable
data class GCPNetworkResult(
    val network: String = "",
    val networkLink: String = "",
    val subnetwork: String = "",
    val subnetworkLink: String = "",
    val phoneIp: String = "",
    val created: List<String> = emptyList(),
    val updated: List<String> = emptyList(),
    val rules: List<GCPNetworkRule> = emptyList(),
    val warnings: List<String> = emptyList(),
)

/** One source of a rule: [protocol] all or tcp, [ports] "" for every port, [source] a CIDR or "tag:<tag>". */
@Serializable
data class GCPNetworkRule(
    val name: String = "",
    val protocol: String = "",
    val ports: String = "",
    val source: String = "",
    val self: Boolean = false,
    val description: String = "",
) {
    /** Port 50000 open to [phoneIp]. */
    fun isPhone(phoneIp: String): Boolean = phoneIp.isNotBlank() && protocol == "tcp" && ports == "50000" && source == "$phoneIp/32"

    /** A rule the app did not word: "tcp/50000 ← 198.51.100.4/32 · Ichor phone 2026-10-01". */
    val plain: String
        get() = listOf("$protocol${if (ports.isBlank()) "" else "/$ports"} ← $source", description).filter { it.isNotBlank() }.joinToString(" · ")
}

@Serializable
data class GCPSubnet(
    val name: String = "",
    val selfLink: String = "",
    val region: String = "",
    val cidr: String = "",
)

@Serializable
data class GCPImage(
    val name: String = "",
    val selfLink: String = "",
    val arch: String = "",
    val sizeGB: Long = 0,
    /** The ichor-talos-version label, "" for an image of your own without it. */
    val talosVersion: String = "",
    val schematic: String = "",
    val uefi: Boolean = false,
)

/** The on-demand hourly price in USD before tax; [hourly] is empty when the Billing Catalog does not answer. */
@Serializable
data class GCPPrice(val hourly: String = "", val currency: String = "")

/** StartGCPCreate's specJSON. Never holds the key. */
@Serializable
data class GCPCreateSpec(
    val project: String,
    val zone: String,
    val machineType: String,
    /** The image's selfLink. */
    val image: String,
    /** The subnet's selfLink. */
    val subnetwork: String,
    // No defaults: TalosJson leaves default values out, and the core reads a missing one as false / 0.
    val associatePublicIp: Boolean,
    val diskGB: Int,
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
    /** On Omni, the labels to sign into the join token (omniLabelsJson); "" for none. */
    val labelsJson: String = "",
)

/** StartGCPImage's specJSON. The image's URL comes from the template, never from here. */
@Serializable
data class GCPImageSpec(
    val templateNode: String = "",
    val arch: String,
    val project: String,
    val zone: String,
    /** The subnet's selfLink. */
    val subnetwork: String,
    // No default: TalosJson leaves default values out, and the core reads a missing one as false.
    val associatePublicIp: Boolean,
    /** "" for the core's default (e2-small or t2a-standard-1). */
    val machineType: String = "",
    /** "" for talos-<version>-gcp-<arch>-<schematic12>. */
    val name: String = "",
)

/** What a finished image run reports; [selfLink] picks it in the image list. */
@Serializable
data class GCPImageResult(
    val image: String = "",
    val selfLink: String = "",
    val arch: String = "",
    val talosVersion: String = "",
    val schematic: String = "",
    val sizeGB: Int = 0,
) : MadeImage {
    override val imageId: String get() = selfLink.ifBlank { image }
    override val name: String get() = image
}

/**
 * The cost line of an image run: the helper's [machineType] and its [hourly] price, the data
 * disk's and the image's prices per GB-month, before tax in [currency]; a price the Billing
 * Catalog does not give is "".
 */
@Serializable
data class GCPImageEstimate(
    val machineType: String = "",
    val hourly: String = "",
    val currency: String = "",
    val diskMonthlyPerGB: String = "",
    val imageMonthlyPerGB: String = "",
    val minutes: Int = 0,
)

const val GCP_DEFAULT_DISK_GB = 20
const val GCP_MIN_DISK_GB = 10
const val GCP_MAX_DISK_GB = 65536

/** The architectures the machine types run, amd64 first. */
fun GCPOptions.arches(): List<String> = machineTypes.map { it.arch }.distinct().sortedBy { if (it == "amd64") "" else it }

/** The machine types of [arch], in the core's order (vCPUs, then memory). */
fun GCPOptions.typesFor(arch: String): List<GCPMachineType> = machineTypes.filter { it.arch == arch }

/** The images [type] can boot: its arch, and UEFI on Arm; labelled ones first, as the core sorts them. */
fun GCPOptions.imagesFor(type: GCPMachineType): List<GCPImage> =
    images.filter { it.arch == type.arch && (type.arch != "arm64" || it.uefi) }

/** The networks with a subnet in the zone's region, the only ones a VM there can use. */
fun GCPOptions.usableNetworks(): List<GCPNetwork> = networks.filter { it.subnets.isNotEmpty() }

/** The core's warnings about a route out without a public address, shown under the switch. */
fun GCPOptions.natWarnings(): List<String> = warnings.filter { it.contains("Cloud NAT") }

/** The other warnings, shown at the top. */
fun GCPOptions.otherWarnings(): List<String> = warnings.filterNot { it.contains("Cloud NAT") }

/** Compute Engine's instance name rule (RFC 1035), which the host name must follow: the VM is named after it. */
fun gcpValidName(name: String): Boolean = Regex("^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$").matches(name)

/** The first https link of a core message: where to enable an API. */
fun gcpActivationLink(message: String): String? = Regex("""https://\S+""").find(message)?.value?.trimEnd('.', ',', ')')

/** The two fields of a pasted service account key the sheet shows; never the private key. */
data class GCPKeyIdentity(val clientEmail: String, val projectId: String)

/** [text] read as a service account key; null when it is not one. */
fun gcpKeyIdentity(text: String): GCPKeyIdentity? {
    val json = runCatching { Json.parseToJsonElement(text.trim()) as? JsonObject }.getOrNull() ?: return null
    fun field(name: String) = runCatching { json[name]?.jsonPrimitive?.contentOrNull }.getOrNull().orEmpty()
    if (field("type") != "service_account" || field("client_email").isBlank() || field("private_key").isBlank()) return null
    return GCPKeyIdentity(field("client_email"), field("project_id"))
}
