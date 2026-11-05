package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_azure.go, cloud_azure_options.go and cloud_azure_create.go.

/**
 * A service principal (az ad sp create-for-rbac's output plus the subscription), sealed on the
 * device; what the core gets per call. [resourceGroup] scopes Test's permission check and
 * [location] the options listed; both follow the person's picks.
 */
@Serializable
data class AzureCredential(
    val tenantId: String = "",
    val clientId: String = "",
    val clientSecret: String = "",
    val subscriptionId: String = "",
    /** "" or AzurePublicCloud, AzureChinaCloud, AzureUSGovernmentCloud. */
    val environment: String = "",
    val resourceGroup: String = "",
    val location: String = "",
)

/** Whose principal this is and whether it may create VMs; [checked] is false when that could not be told. */
@Serializable
data class AzureCheck(
    val tenant: String = "",
    val subscription: String = "",
    val subscriptionName: String = "",
    val canCreate: Boolean = false,
    val checked: Boolean = false,
)

/** What a VM can be created from in [location]; [warnings] are the core's, shown as they are. */
@Serializable
data class AzureOptions(
    val subscription: String = "",
    val location: String = "",
    val subscriptions: List<AzureSubscription> = emptyList(),
    val locations: List<AzureLocation> = emptyList(),
    val resourceGroups: List<AzureResourceGroup> = emptyList(),
    val sizes: List<AzureSize> = emptyList(),
    val networks: List<AzureNetwork> = emptyList(),
    val securityGroups: List<AzureSecurityGroup> = emptyList(),
    val images: List<AzureImage> = emptyList(),
    val warnings: List<String> = emptyList(),
)

@Serializable
data class AzureSubscription(val id: String = "", val name: String = "")

@Serializable
data class AzureLocation(val name: String = "", val displayName: String = "")

@Serializable
data class AzureResourceGroup(val name: String = "", val location: String = "")

@Serializable
data class AzureSize(
    val name: String = "",
    val vcpus: Int = 0,
    val memoryGB: Double = 0.0,
    /** amd64 or arm64. */
    val arch: String = "",
    /** The Hyper-V generations it boots: V1, V2. */
    val generations: List<String> = emptyList(),
)

@Serializable
data class AzureNetwork(
    val id: String = "",
    val name: String = "",
    val resourceGroup: String = "",
    val location: String = "",
    val subnets: List<AzureSubnet> = emptyList(),
)

@Serializable
data class AzureSubnet(
    val id: String = "",
    val name: String = "",
    val cidr: String = "",
    /** The subnet's own security group, empty when it has none. */
    val nsgId: String = "",
    /** It reaches the Internet without a public IP on the VM. */
    val natGateway: Boolean = false,
)

@Serializable
data class AzureSecurityGroup(
    val id: String = "",
    val name: String = "",
    val resourceGroup: String = "",
    /** The Internet reaches port 50000 (the Talos API). */
    val wideOpen: Boolean = false,
)

@Serializable
data class AzureImage(
    val id: String = "",
    val name: String = "",
    val resourceGroup: String = "",
    /** managed (an image) or gallery (a gallery image version). */
    val source: String = "",
    val arch: String = "",
    /** V1 or V2. */
    val generation: String = "",
    val talosVersion: String = "",
    val schematic: String = "",
)

/** The pay-as-you-go hourly price before tax; [hourly] is empty when the Retail Prices API does not answer. */
@Serializable
data class AzurePrice(val hourly: String = "", val currency: String = "")

/** StartAzureCreate's specJSON. Never holds the principal. */
@Serializable
data class AzureCreateSpec(
    val resourceGroup: String,
    val location: String,
    val size: String,
    val imageId: String,
    val subnetId: String,
    val securityGroupId: String = "",
    val associatePublicIp: Boolean = false,
    val osDiskGB: Int = 0,
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
    /** On Omni, the labels to sign into the join token (omniLabelsJson); "" for none. */
    val labelsJson: String = "",
)

/** The VM sizes that boot [image] (its architecture and Hyper-V generation), in the core's order. */
fun AzureOptions.sizesFor(image: AzureImage): List<AzureSize> =
    sizes.filter { it.arch == image.arch && image.generation in it.generations }

/** The architectures of [sizes], amd64 first. */
fun List<AzureSize>.arches(): List<String> = map { it.arch }.distinct().sortedBy { if (it == "amd64") "" else it }

/** The subnets of the virtual network [networkId]; empty for an unknown one. */
fun AzureOptions.subnetsIn(networkId: String): List<AzureSubnet> = networks.firstOrNull { it.id == networkId }?.subnets.orEmpty()

/** The resource group's location, or the listed one when the group is unknown. */
fun AzureOptions.locationOf(resourceGroup: String): String =
    resourceGroups.firstOrNull { it.name == resourceGroup }?.location?.lowercase() ?: location

/** The display name of [name] ("West Europe"), or the name itself. */
fun AzureOptions.locationLabel(name: String): String = locations.firstOrNull { it.name == name }?.displayName?.ifBlank { null } ?: name

/** The default OS disk size, the smallest a Talos image fits in, and Azure's largest. */
const val AZURE_DEFAULT_DISK_GB = 20
const val AZURE_MIN_DISK_GB = 10
const val AZURE_MAX_DISK_GB = 4095

/** A typed OS disk size in GB, or null when it is not a size Azure takes. */
fun azureDiskGB(text: String): Int? = text.trim().toIntOrNull()?.takeIf { it in AZURE_MIN_DISK_GB..AZURE_MAX_DISK_GB }
