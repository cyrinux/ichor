package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudAzureTest {
    // The same fixture as ios/IchorCore/Tests/IchorCoreTests/CloudAzureTests.swift.
    private val options = TalosJson.decodeFromString(
        AzureOptions.serializer(),
        """
        {"subscription":"00000000-0000-0000-0000-000000000000","location":"westeurope",
         "subscriptions":[{"id":"00000000-0000-0000-0000-000000000000","name":"Scratch"}],
         "locations":[{"name":"northeurope","displayName":"North Europe"},{"name":"westeurope","displayName":"West Europe"}],
         "resourceGroups":[{"name":"talos","location":"westeurope"},{"name":"north","location":"NorthEurope"}],
         "sizes":[{"name":"Standard_A1_v2","vcpus":1,"memoryGB":2,"arch":"amd64","generations":["V1"]},
                  {"name":"Standard_B2s","vcpus":2,"memoryGB":4,"arch":"amd64","generations":["V1","V2"]},
                  {"name":"Standard_D2ps_v5","vcpus":2,"memoryGB":8,"arch":"arm64","generations":["V2"]},
                  {"name":"Standard_E2ps_v5","vcpus":2,"memoryGB":16,"arch":"arm64","generations":["V2"]}],
         "networks":[{"id":"vnet-a","name":"talos-vnet","resourceGroup":"talos","location":"westeurope",
                      "subnets":[{"id":"vnet-a/subnets/nodes","name":"nodes","cidr":"10.0.0.0/24","nsgId":"nsg-a","natGateway":false},
                                 {"id":"vnet-a/subnets/egress","name":"egress","cidr":"10.0.1.0/24","nsgId":"","natGateway":true}]}],
         "securityGroups":[{"id":"nsg-a","name":"talos-nodes","resourceGroup":"talos","wideOpen":true}],
         "images":[{"id":"gallery-arm","name":"talos/talos-arm64/1.12.0","resourceGroup":"talos","source":"gallery","arch":"arm64",
                    "generation":"V2","talosVersion":"v1.12.0","schematic":"sample-schematic"},
                   {"id":"image-amd","name":"talos-by-hand","resourceGroup":"talos","source":"managed","arch":"amd64",
                    "generation":"V1","talosVersion":"","schematic":""}],
         "warnings":["a warning"]}
        """,
    )

    @Test
    fun theImageChoosesTheSizesThatBootIt() {
        val (gallery, managed) = options.images
        assertEquals(listOf("Standard_D2ps_v5", "Standard_E2ps_v5"), options.sizesFor(gallery).map { it.name })
        assertEquals(listOf("Standard_A1_v2", "Standard_B2s"), options.sizesFor(managed).map { it.name })
        assertEquals(listOf("amd64", "arm64"), options.sizes.reversed().arches())
        assertEquals("gallery", gallery.source)
    }

    @Test
    fun theNetworkChoosesItsSubnets() {
        val subnets = options.subnetsIn("vnet-a")
        assertEquals(listOf("nodes", "egress"), subnets.map { it.name })
        assertTrue(subnets[1].natGateway)
        assertEquals("", subnets[1].nsgId)
        assertTrue(options.subnetsIn("vnet-z").isEmpty())
        assertTrue(options.securityGroups.single().wideOpen)
    }

    @Test
    fun theResourceGroupGivesTheLocation() {
        assertEquals("northeurope", options.locationOf("north"))
        assertEquals("westeurope", options.locationOf("unknown"))
        assertEquals("West Europe", options.locationLabel("westeurope"))
        assertEquals("eastus", options.locationLabel("eastus"))
        assertEquals(listOf("a warning"), options.warnings)
    }

    @Test
    fun theDiskSizeIsOneAzureTakes() {
        assertEquals(20, azureDiskGB(" 20 "))
        assertEquals(4095, azureDiskGB("4095"))
        assertNull(azureDiskGB("4096"))
        assertNull(azureDiskGB("5"))
        assertNull(azureDiskGB(""))
    }

    @Test
    fun nullListsDecodeEmpty() {
        val empty = TalosJson.decodeFromString(AzureOptions.serializer(), """{"location":"westeurope","sizes":null,"images":null,"warnings":null}""")
        assertTrue(empty.sizes.isEmpty() && empty.images.isEmpty() && empty.warnings.isEmpty())
    }

    @Test
    fun theCheckAndThePriceDecode() {
        val check = TalosJson.decodeFromString(
            AzureCheck.serializer(),
            """{"tenant":"t","subscription":"00000000-0000-0000-0000-000000000000","subscriptionName":"Scratch","canCreate":false,"checked":false}""",
        )
        assertFalse(check.checked)
        assertEquals("Scratch", check.subscriptionName)
        assertEquals("0.0480", TalosJson.decodeFromString(AzurePrice.serializer(), """{"hourly":"0.0480","currency":"USD"}""").hourly)
        assertEquals("", TalosJson.decodeFromString(AzurePrice.serializer(), """{"hourly":""}""").hourly)
    }

    @Test
    fun theCredentialAndTheSpecUseTheCoresNames() {
        val credential = TalosJson.encodeToString(
            AzureCredential.serializer(),
            AzureCredential("t", "c", "s", "sub", "AzureChinaCloud", "talos", "chinaeast2"),
        )
        listOf("\"tenantId\"", "\"clientId\"", "\"clientSecret\"", "\"subscriptionId\"", "\"environment\"", "\"resourceGroup\"", "\"location\"").forEach {
            assertTrue(credential, credential.contains(it))
        }
        val spec = AzureCreateSpec(
            resourceGroup = "talos", location = "westeurope", size = "Standard_B2s", imageId = "image-amd", subnetId = "vnet-a/subnets/nodes",
            securityGroupId = "nsg-a", associatePublicIp = true, osDiskGB = 20, hostname = "node-c",
        )
        val json = TalosJson.encodeToString(AzureCreateSpec.serializer(), spec)
        listOf("\"resourceGroup\"", "\"size\"", "\"imageId\"", "\"subnetId\"", "\"securityGroupId\"", "\"associatePublicIp\":true", "\"osDiskGB\":20").forEach {
            assertTrue(json, json.contains(it))
        }
    }

    @Test
    fun aRunWithAVMHasTheCloudTimeline() {
        val spec = AzureCreateSpec(resourceGroup = "talos", location = "westeurope", size = "Standard_B2s", imageId = "image-amd", subnetId = "s", hostname = "node-c")
        val plain = JoinRequest(address = "", template = "", role = "", hostname = "node-c", disk = "", staticAddress = "", azure = spec)
        assertTrue(plain.cloud)
        assertEquals(JoinPhase.cloudRun, plain.phases)
        assertEquals(JoinPhase.cloudOmniRun, plain.copy(omni = true).phases)
    }

    @Test
    fun theImageSpecUsesTheCoresNames() {
        val spec = AzureImageSpec(
            templateNode = "10.0.0.2", arch = "arm64", resourceGroup = "talos", location = "westeurope",
            subnetId = "vnet-a/subnets/nodes", securityGroupId = "nsg-a",
        )
        val json = TalosJson.encodeToString(AzureImageSpec.serializer(), spec)
        listOf(
            "\"templateNode\":\"10.0.0.2\"", "\"arch\":\"arm64\"", "\"resourceGroup\":\"talos\"", "\"location\":\"westeurope\"",
            "\"subnetId\":\"vnet-a/subnets/nodes\"", "\"securityGroupId\":\"nsg-a\"",
        ).forEach { assertTrue(json, json.contains(it)) }
    }

    @Test
    fun theImageResultAndItsEstimateDecode() {
        val result = TalosJson.decodeFromString(
            AzureImageResult.serializer(),
            """{"imageId":"image-new","name":"talos-v1.12.0-azure-amd64-0123456789ab","arch":"amd64","talosVersion":"v1.12.0",
               "schematic":"0123","generation":"V1","sizeGB":8}""",
        )
        assertEquals("image-new", result.imageId)
        assertEquals("V1", result.generation)
        assertEquals(8, result.sizeGB)
        val made: MadeImage = result
        assertEquals("talos-v1.12.0-azure-amd64-0123456789ab", made.name)
        val estimate = TalosJson.decodeFromString(
            AzureImageEstimate.serializer(),
            """{"size":"Standard_B2s","hourly":"0.0416","currency":"USD","imageNote":"billed per month","minutes":10}""",
        )
        assertEquals("Standard_B2s", estimate.size)
        assertEquals("0.0416", estimate.hourly)
        assertEquals(10, estimate.minutes)
        val unpriced = TalosJson.decodeFromString(AzureImageEstimate.serializer(), """{"size":"Standard_B2pls_v2","minutes":10}""")
        assertEquals("", unpriced.hourly)
    }

    @Test
    fun anImageRunHasItsOwnSteps() {
        val spec = AzureImageSpec(arch = "amd64", resourceGroup = "talos", location = "westeurope", subnetId = "s")
        val request = JoinRequest(address = "", template = "", role = "", hostname = "Talos amd64", disk = "", staticAddress = "", azureImage = spec)
        assertEquals(ImageRunKind.AZURE_IMAGE, request.imageKind)
        assertEquals(JoinPhase.imageRun, request.phases)
        assertTrue(request.imageRun)
        assertFalse(request.cloud)
        val steps = joinTimeline(listOf(JoinProgress(phase = "creating"), JoinProgress(phase = "snapshot")), finished = false, failed = false, phases = request.phases)
        assertEquals(listOf(JoinPhase.CREATING, JoinPhase.WRITING, JoinPhase.SNAPSHOT, JoinPhase.CLEANUP, JoinPhase.DONE), steps.map { it.phase })
        assertEquals(StepStatus.DONE, steps[1].status)
        assertEquals(StepStatus.CURRENT, steps[2].status)
        assertEquals(StepStatus.PENDING, steps[3].status)
    }

    @Test
    fun eachImageRunKnowsItsKind() {
        val plain = JoinRequest(address = "", template = "", role = "", hostname = "n", disk = "", staticAddress = "")
        assertNull(plain.imageKind)
        assertFalse(plain.imageRun)
        assertEquals(ImageRunKind.HETZNER_SNAPSHOT, plain.copy(snapshot = HetznerSnapshotSpec(arch = "amd64", location = "fsn1")).imageKind)
        assertEquals(ImageRunKind.AWS_AMI, plain.copy(awsImage = AWSImageSpec(arch = "amd64", region = "eu-west-1", subnetId = "s")).imageKind)
        assertEquals(JoinPhase.snapshotRun, ImageRunKind.HETZNER_SNAPSHOT.phases)
        assertEquals(JoinPhase.awsImageRun, ImageRunKind.AWS_AMI.phases)
    }

    // An invented Microsoft-account sign-in, as the core returns it (refresh token included).
    private val signedIn = """
        {"tenantId":"00000000-0000-0000-0000-000000000001","clientId":"04b07795-8ddb-461a-bbee-02f9e1bf7b46",
         "subscriptionId":"00000000-0000-0000-0000-000000000000","kind":"user","refreshToken":"r","account":"someone@example.org"}
    """.trimIndent()

    @Test
    fun aMicrosoftAccountSignInIsReadWithoutItsToken() {
        val credential = TalosJson.decodeFromString(AzureCredential.serializer(), signedIn)
        assertTrue(credential.microsoftAccount)
        assertEquals("someone@example.org", credential.account)
        assertFalse(AzureCredential(tenantId = "t", clientId = "c", clientSecret = "s", subscriptionId = "x").microsoftAccount)
    }

    @Test
    fun theGuidedNetworkDecodes() {
        val network = TalosJson.decodeFromString(
            AzureNetworkResult.serializer(),
            """{"networkId":"vnet","subnetId":"vnet/subnets/nodes","securityGroupId":"nsg","phoneIp":"203.0.113.7/32","created":["nsg","vnet"],"reused":[],"warnings":[]}""",
        )
        assertEquals("vnet/subnets/nodes", network.subnetId)
        assertEquals(listOf("nsg", "vnet"), network.created)
        assertEquals("", network.credential)
        val group = TalosJson.decodeFromString(AzureSecurityGroup.serializer(), """{"id":"g","wideOpen":true,"wideOpenRule":"talos-api ← *"}""")
        assertEquals("talos-api ← *", group.wideOpenRule)
        assertEquals(
            """{"resourceGroup":"talos","location":"westeurope","omni":true}""",
            TalosJson.encodeToString(AzureNetworkSpec.serializer(), AzureNetworkSpec("talos", "westeurope", omni = true)),
        )
    }

    @Test
    fun theSignInStepsDecode() {
        val start = TalosJson.decodeFromString(
            AzureSignInStart.serializer(),
            """{"verificationUri":"https://microsoft.com/devicelogin","userCode":"SAMPLE123","expiresIn":900,"credential":"{}"}""",
        )
        assertTrue(start.prompt.isDevice)
        assertEquals("https://microsoft.com/devicelogin", start.prompt.url)
        assertEquals("SAMPLE123", start.prompt.userCode)
        val finish = TalosJson.decodeFromString(
            AzureSignInFinish.serializer(),
            """{"credential":"{}","account":"someone@example.org","subscriptions":[{"id":"00000000-0000-0000-0000-000000000000","name":"","tenantId":"t"}]}""",
        )
        assertEquals("00000000-0000-0000-0000-000000000000", finish.subscriptions.single().label)
        val check = TalosJson.decodeFromString(AzureCheck.serializer(), """{"subscription":"x","account":"someone@example.org","credential":"{}"}""")
        assertEquals("{}", check.credential)
        assertEquals("", TalosJson.decodeFromString(AzureOptions.serializer(), "{}").credential)
    }
}
