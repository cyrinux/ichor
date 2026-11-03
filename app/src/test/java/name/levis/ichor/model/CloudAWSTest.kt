package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudAWSTest {
    // The same fixture as ios/IchorCore/Tests/IchorCoreTests/CloudAWSTests.swift.
    private val options = TalosJson.decodeFromString(
        AWSOptions.serializer(),
        """
        {"region":"eu-west-1","regions":["eu-central-1","eu-west-1"],
         "types":[{"name":"t4g.small","vcpus":2,"memoryGB":2,"arch":"arm64","burstable":true},
                  {"name":"t3.small","vcpus":2,"memoryGB":2,"arch":"amd64","burstable":true},
                  {"name":"m6i.large","vcpus":2,"memoryGB":8,"arch":"amd64","burstable":false}],
         "subnets":[{"id":"subnet-a","vpcId":"vpc-a","vpcDefault":true,"cidr":"172.31.0.0/20","az":"eu-west-1a","public":true,"name":""},
                    {"id":"subnet-b","vpcId":"vpc-b","vpcDefault":false,"cidr":"10.0.0.0/24","az":"eu-west-1b","public":false,"name":"private"}],
         "securityGroups":[{"id":"sg-a","vpcId":"vpc-a","name":"default","description":"default VPC security group"},
                           {"id":"sg-b","vpcId":"vpc-b","name":"nodes","description":""}],
         "images":[{"id":"ami-custom","arch":"amd64","source":"custom","name":"my talos","talosVersion":"","schematic":""},
                   {"id":"ami-amd","arch":"amd64","source":"official","name":"Talos v1.12.0 (amd64)","talosVersion":"v1.12.0","schematic":""},
                   {"id":"ami-arm","arch":"arm64","source":"official","name":"Talos v1.12.0 (arm64)","talosVersion":"v1.12.0","schematic":""}],
         "warnings":["a warning"]}
        """,
    )

    @Test
    fun theArchCouplesTypesAndImages() {
        assertEquals(listOf("amd64", "arm64"), options.arches())
        assertEquals(listOf("t3.small", "m6i.large"), options.typesFor("amd64").map { it.name })
        assertEquals(listOf("ami-amd", "ami-custom"), options.imagesFor("amd64").map { it.id })
        assertEquals(listOf("ami-arm"), options.imagesFor("arm64").map { it.id })
        assertTrue(options.imagesFor("arm64").single().official)
    }

    @Test
    fun securityGroupsFollowTheSubnetsVPC() {
        assertEquals(listOf("sg-a"), options.securityGroupsIn("vpc-a").map { it.id })
        assertTrue(options.securityGroupsIn("vpc-z").isEmpty())
        assertTrue(options.subnets.first().public)
        assertEquals(listOf("a warning"), options.warnings)
    }

    @Test
    fun nullListsDecodeEmpty() {
        val empty = TalosJson.decodeFromString(AWSOptions.serializer(), """{"region":"eu-west-1","types":null,"images":null,"warnings":null}""")
        assertTrue(empty.types.isEmpty() && empty.images.isEmpty() && empty.warnings.isEmpty())
    }

    @Test
    fun theCheckAndThePriceDecode() {
        val check = TalosJson.decodeFromString(AWSCheck.serializer(), """{"account":"000000000000","arn":"arn","region":"eu-west-1","canCreate":false,"checked":false}""")
        assertFalse(check.checked)
        assertEquals("000000000000", check.account)
        assertEquals("0.0208", TalosJson.decodeFromString(AWSPrice.serializer(), """{"hourly":"0.0208","currency":"USD"}""").hourly)
        assertEquals("", TalosJson.decodeFromString(AWSPrice.serializer(), """{"hourly":""}""").hourly)
    }

    @Test
    fun theSpecUsesTheCoresNames() {
        val spec = AWSCreateSpec(
            region = "eu-west-1", instanceType = "t3.small", imageId = "ami-amd", subnetId = "subnet-a",
            securityGroupIds = listOf("sg-a"), associatePublicIp = true, hostname = "node-c",
        )
        val json = TalosJson.encodeToString(AWSCreateSpec.serializer(), spec)
        listOf("\"instanceType\"", "\"imageId\"", "\"subnetId\"", "\"securityGroupIds\"", "\"associatePublicIp\":true").forEach {
            assertTrue(json, json.contains(it))
        }
    }

    @Test
    fun aRunWithAnInstanceHasTheCloudTimeline() {
        val spec = AWSCreateSpec(region = "eu-west-1", instanceType = "t3.small", imageId = "ami-amd", subnetId = "subnet-a", hostname = "node-c")
        val plain = JoinRequest(address = "", template = "", role = "", hostname = "node-c", disk = "", staticAddress = "", aws = spec)
        assertEquals(JoinPhase.cloudRun, plain.phases)
        assertEquals(JoinPhase.cloudOmniRun, plain.copy(omni = true).phases)
    }
}
