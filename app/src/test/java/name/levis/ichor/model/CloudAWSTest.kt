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
    fun theClustersImageComesFirst() {
        val default = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"
        val own = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"
        val withTemplate = options.copy(
            templateVersion = "v1.12.0",
            templateSchematic = own,
            images = listOf(
                AWSImage(id = "ami-amd", arch = "amd64", source = "official", name = "Talos v1.12.0 (amd64)", talosVersion = "v1.12.0", schematic = default),
                AWSImage(id = "ami-old", arch = "amd64", source = "custom", name = "a old", talosVersion = "v1.11.5", schematic = own),
                AWSImage(id = "ami-own", arch = "amd64", source = "custom", name = "z own", talosVersion = "v1.12.0", schematic = own),
                AWSImage(id = "ami-arm", arch = "arm64", source = "official", name = "Talos v1.12.0 (arm64)", talosVersion = "v1.12.0", schematic = default),
            ),
        )
        assertEquals(listOf("ami-own", "ami-amd", "ami-old"), withTemplate.imagesFor("amd64").map { it.id })
        assertTrue(withTemplate.hasClusterImage("amd64"))
        // An official AMI is the default schematic: not this cluster's image.
        assertFalse(withTemplate.hasClusterImage("arm64"))

        // A cluster on the default schematic: the official AMI is its image.
        val plain = withTemplate.copy(templateSchematic = default)
        assertTrue(plain.images.first().matches(plain))
        assertTrue(plain.hasClusterImage("arm64"))

        // Nothing to compare: no AMI matches, and an AMI of the account's own is enough.
        val unknown = withTemplate.copy(templateSchematic = "")
        assertFalse(unknown.images.any { it.matches(unknown) })
        assertTrue(unknown.hasClusterImage("amd64"))
        assertFalse(unknown.hasClusterImage("arm64"))
    }

    @Test
    fun theTemplateDecodes() {
        val decoded = TalosJson.decodeFromString(AWSOptions.serializer(), """{"templateVersion":"v1.12.0","templateSchematic":"abc"}""")
        assertEquals("v1.12.0", decoded.templateVersion)
        assertEquals("abc", decoded.templateSchematic)
        assertEquals("", options.templateVersion)
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

    @Test
    fun theAMISpecUsesTheCoresNames() {
        val spec = AWSImageSpec(templateNode = "10.0.0.2", arch = "arm64", region = "eu-west-1", subnetId = "subnet-a", securityGroupId = "sg-a")
        val json = TalosJson.encodeToString(AWSImageSpec.serializer(), spec)
        listOf("\"templateNode\":\"10.0.0.2\"", "\"arch\":\"arm64\"", "\"region\"", "\"subnetId\"", "\"securityGroupId\":\"sg-a\"").forEach {
            assertTrue(json, json.contains(it))
        }
    }

    @Test
    fun theAMIResultAndItsCostDecode() {
        val result = TalosJson.decodeFromString(
            AWSImageResult.serializer(),
            """{"imageId":"ami-0abc","snapshotId":"snap-0abc","name":"talos-v1.12.0-aws-amd64-0123456789ab","arch":"amd64","talosVersion":"v1.12.0","schematic":"0123","sizeGB":8}""",
        )
        assertEquals("ami-0abc", result.imageId)
        assertEquals(8, result.sizeGB)
        val cost = TalosJson.decodeFromString(
            AWSImageCost.serializer(),
            """{"instanceType":"t3.small","hourly":"0.0228","currency":"USD","snapshotMonthlyPerGB":"0.05","minutes":10}""",
        )
        assertEquals("t3.small", cost.instanceType)
        assertEquals(10, cost.minutes)
        val unpriced = TalosJson.decodeFromString(AWSImageCost.serializer(), """{"instanceType":"t4g.small","minutes":10}""")
        assertEquals("", unpriced.hourly)
    }

    @Test
    fun theHelperSubnetsListPublicOnesFirst() {
        assertEquals(listOf("subnet-a", "subnet-b"), options.helperSubnets().map { it.id })
        val reversed = options.copy(subnets = options.subnets.reversed())
        assertEquals(listOf("subnet-a", "subnet-b"), reversed.helperSubnets().map { it.id })
    }

    @Test
    fun anAMIRunHasItsOwnSteps() {
        val spec = AWSImageSpec(arch = "amd64", region = "eu-west-1", subnetId = "subnet-a")
        val request = JoinRequest(address = "", template = "", role = "", hostname = "Talos amd64", disk = "", staticAddress = "", awsImage = spec)
        assertEquals(JoinPhase.awsImageRun, request.phases)
        assertTrue(request.imageRun)
        assertFalse(request.cloud)
        val steps = joinTimeline(listOf(JoinProgress(phase = "creating"), JoinProgress(phase = "snapshot")), finished = false, failed = false, phases = request.phases)
        assertEquals(JoinPhase.awsImageRun, steps.map { it.phase })
        assertEquals(StepStatus.DONE, steps[JoinPhase.awsImageRun.indexOf(JoinPhase.WRITING)].status)
        assertEquals(StepStatus.CURRENT, steps[JoinPhase.awsImageRun.indexOf(JoinPhase.SNAPSHOT)].status)
        assertEquals(StepStatus.PENDING, steps[JoinPhase.awsImageRun.indexOf(JoinPhase.REGISTER)].status)
        assertEquals(JoinPhase.REGISTER, JoinPhase.of("register"))
    }

    // An invented Identity Center sign-in, as the core returns it (tokens included).
    private val signedIn = """
        {"region":"eu-west-1","ssoStartUrl":"https://example.awsapps.com/start","ssoRegion":"eu-west-1",
         "accountId":"123456789012","accountName":"Sandbox","roleName":"IchorNodeCreator",
         "ssoToken":{"accessToken":"a","refreshToken":"r","clientId":"c","clientSecret":"s"}}
    """.trimIndent()

    @Test
    fun anIdentityCenterSignInIsReadWithoutItsTokens() {
        val credential = TalosJson.decodeFromString(AWSCredential.serializer(), signedIn)
        assertTrue(credential.identityCenter)
        assertEquals("Sandbox · IchorNodeCreator", credential.role.label)
        assertFalse(AWSCredential(accessKeyId = "AKIAEXAMPLE", secretAccessKey = "secret", region = "eu-west-1").identityCenter)
    }

    @Test
    fun theSignInStepsDecode() {
        val start = TalosJson.decodeFromString(
            AWSSignInStart.serializer(),
            """{"verificationUri":"https://device.sso/","verificationUriComplete":"https://device.sso/?user_code=WXYZ-1234","userCode":"WXYZ-1234","expiresIn":600,"credential":"{}"}""",
        )
        assertTrue(start.prompt.isDevice)
        assertEquals("https://device.sso/?user_code=WXYZ-1234", start.prompt.url)
        assertEquals("WXYZ-1234", start.prompt.userCode)
        val finish = TalosJson.decodeFromString(
            AWSSignInFinish.serializer(),
            """{"credential":"{}","roles":[{"accountId":"123456789012","roleName":"IchorNodeCreator"}]}""",
        )
        assertEquals("IchorNodeCreator", finish.roles.single().roleName)
        assertEquals("{}", TalosJson.decodeFromString(AWSCheck.serializer(), """{"account":"123456789012","credential":"{}"}""").credential)
        assertEquals("", TalosJson.decodeFromString(AWSRenewed.serializer(), "{}").credential)
    }
}
