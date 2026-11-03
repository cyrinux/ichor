package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/cloud_aws.go, cloud_aws_options.go and cloud_aws_create.go.

/** Access keys and their region, sealed on the device; what the core gets per call. */
@Serializable
data class AWSCredential(
    val accessKeyId: String = "",
    val secretAccessKey: String = "",
    val sessionToken: String = "",
    val region: String = "",
)

/** Whose keys these are and whether they may run instances; [checked] is false when that could not be told. */
@Serializable
data class AWSCheck(
    val account: String = "",
    val arn: String = "",
    val region: String = "",
    val canCreate: Boolean = false,
    val checked: Boolean = false,
)

/** What an instance can be created from in [region]; [warnings] are the core's, shown as they are. */
@Serializable
data class AWSOptions(
    val region: String = "",
    val regions: List<String> = emptyList(),
    val types: List<AWSInstanceType> = emptyList(),
    val subnets: List<AWSSubnet> = emptyList(),
    val securityGroups: List<AWSSecurityGroup> = emptyList(),
    val images: List<AWSImage> = emptyList(),
    val warnings: List<String> = emptyList(),
)

@Serializable
data class AWSInstanceType(
    val name: String = "",
    val vcpus: Int = 0,
    val memoryGB: Double = 0.0,
    /** amd64 or arm64. */
    val arch: String = "",
    val burstable: Boolean = false,
)

@Serializable
data class AWSSubnet(
    val id: String = "",
    val vpcId: String = "",
    val vpcDefault: Boolean = false,
    val cidr: String = "",
    val az: String = "",
    /** Assigns a public IPv4 address on launch. */
    val public: Boolean = false,
    val name: String = "",
)

@Serializable
data class AWSSecurityGroup(
    val id: String = "",
    val vpcId: String = "",
    val name: String = "",
    val description: String = "",
)

@Serializable
data class AWSImage(
    val id: String = "",
    val arch: String = "",
    /** official (Sidero Labs' AMI of the cluster's version) or custom (the account's own). */
    val source: String = "",
    val name: String = "",
    val talosVersion: String = "",
    val schematic: String = "",
) {
    val official: Boolean get() = source == "official"
}

/** The on-demand hourly price before tax; [hourly] is empty when the Price List does not answer. */
@Serializable
data class AWSPrice(val hourly: String = "", val currency: String = "")

/** StartAWSCreate's specJSON. Never holds the keys. */
@Serializable
data class AWSCreateSpec(
    val region: String,
    val instanceType: String,
    val imageId: String,
    val subnetId: String,
    val securityGroupIds: List<String> = emptyList(),
    val associatePublicIp: Boolean = false,
    val keyName: String = "",
    val hostname: String,
    val role: String = "",
    val templateNode: String = "",
    val installDisk: String = "",
)

/** The architectures the instance types run, amd64 first. */
fun AWSOptions.arches(): List<String> = types.map { it.arch }.distinct().sortedBy { if (it == "amd64") "" else it }

/** The instance types of [arch], in the core's order. */
fun AWSOptions.typesFor(arch: String): List<AWSInstanceType> = types.filter { it.arch == arch }

/** The images [arch] can boot: official ones first, then the account's own by name. */
fun AWSOptions.imagesFor(arch: String): List<AWSImage> =
    images.filter { it.arch == arch }.sortedWith(compareBy<AWSImage> { !it.official }.thenBy { it.name.lowercase() })

/** The security groups of [vpcId], the subnet's VPC: EC2 refuses the others. */
fun AWSOptions.securityGroupsIn(vpcId: String): List<AWSSecurityGroup> = securityGroups.filter { it.vpcId == vpcId }
