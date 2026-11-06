package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudGCPTest {
    // The same fixture as ios/IchorCore/Tests/IchorCoreTests/CloudGCPTests.swift.
    private val options = TalosJson.decodeFromString(
        GCPOptions.serializer(),
        """
        {"project":"example-project","zone":"europe-west1-b",
         "projects":[{"id":"example-project","name":"Example project"},{"id":"other-project","name":"other-project"}],
         "zones":[{"name":"europe-west1-b","region":"europe-west1"},{"name":"europe-west1-c","region":"europe-west1"}],
         "machineTypes":[{"name":"e2-medium","vcpus":2,"memoryGB":4,"arch":"amd64"},
                         {"name":"t2a-standard-1","vcpus":1,"memoryGB":4,"arch":"arm64"},
                         {"name":"e2-standard-2","vcpus":2,"memoryGB":8,"arch":"amd64"},
                         {"name":"t2a-standard-2","vcpus":2,"memoryGB":8,"arch":"arm64"}],
         "networks":[{"name":"default","selfLink":"https://example.test/networks/default","autoSubnets":true,
                      "subnets":[{"name":"default","selfLink":"https://example.test/subnetworks/default","region":"europe-west1","cidr":"10.132.0.0/20"}]},
                     {"name":"empty","selfLink":"https://example.test/networks/empty","autoSubnets":false,"subnets":null}],
         "images":[{"name":"talos-v1-12-0-amd64","selfLink":"https://example.test/images/talos-amd64","arch":"amd64","sizeGB":10,
                    "talosVersion":"v1.12.0","schematic":"","uefi":false},
                   {"name":"talos-v1-12-0-arm64","selfLink":"https://example.test/images/talos-arm64","arch":"arm64","sizeGB":10,
                    "talosVersion":"v1.12.0","schematic":"","uefi":true},
                   {"name":"my-own-arm","selfLink":"https://example.test/images/my-own-arm","arch":"arm64","sizeGB":10,
                    "talosVersion":"","schematic":"","uefi":false}],
         "warnings":["a warning","network default has no Cloud NAT in europe-west1: without a public address, the node needs a route out"]}
        """,
    )

    @Test
    fun theArchCouplesTypesAndImages() {
        assertEquals(listOf("amd64", "arm64"), options.arches())
        assertEquals(listOf("e2-medium", "e2-standard-2"), options.typesFor("amd64").map { it.name })
        val arm = options.typesFor("arm64").first()
        // An Arm VM boots UEFI images only: the unlabelled one without it is not offered.
        assertEquals(listOf("talos-v1-12-0-arm64"), options.imagesFor(arm).map { it.name })
        assertEquals(listOf("talos-v1-12-0-amd64"), options.imagesFor(options.typesFor("amd64").first()).map { it.name })
    }

    @Test
    fun onlyNetworksWithASubnetHereArePicked() {
        assertEquals(listOf("default"), options.usableNetworks().map { it.name })
        assertEquals("10.132.0.0/20", options.usableNetworks().single().subnets.single().cidr)
        assertTrue(options.networks.last().subnets.isEmpty())
    }

    @Test
    fun theNATWarningGoesUnderTheSwitch() {
        assertEquals(listOf("a warning"), options.otherWarnings())
        assertEquals(1, options.natWarnings().size)
    }

    @Test
    fun nullListsDecodeEmpty() {
        val empty = TalosJson.decodeFromString(GCPOptions.serializer(), """{"project":"p","zones":null,"machineTypes":null,"images":null,"warnings":null}""")
        assertTrue(empty.zones.isEmpty() && empty.machineTypes.isEmpty() && empty.images.isEmpty() && empty.warnings.isEmpty())
    }

    @Test
    fun theCheckAndThePriceDecode() {
        val check = TalosJson.decodeFromString(
            GCPCheck.serializer(),
            """{"account":"node-creator@example-project.iam.gserviceaccount.com","project":"example-project","canCreate":false,"checked":true,"missing":["compute.instances.create"]}""",
        )
        assertTrue(check.checked)
        assertFalse(check.canCreate)
        assertEquals(listOf("compute.instances.create"), check.missing)
        assertEquals("0.0335", TalosJson.decodeFromString(GCPPrice.serializer(), """{"hourly":"0.0335","currency":"USD"}""").hourly)
        assertEquals("", TalosJson.decodeFromString(GCPPrice.serializer(), """{"hourly":""}""").hourly)
    }

    @Test
    fun theSpecUsesTheCoresNamesAndKeepsItsDefaults() {
        val spec = GCPCreateSpec(
            project = "example-project", zone = "europe-west1-b", machineType = "e2-medium",
            image = "https://example.test/images/talos-amd64", subnetwork = "https://example.test/subnetworks/default",
            associatePublicIp = true, diskGB = 20, hostname = "node-c",
        )
        val json = TalosJson.encodeToString(GCPCreateSpec.serializer(), spec)
        // TalosJson drops default values: these must be there, or the core reads false and 0.
        listOf("\"machineType\"", "\"image\"", "\"subnetwork\"", "\"associatePublicIp\":true", "\"diskGB\":20").forEach {
            assertTrue(json, json.contains(it))
        }
        assertFalse(json, json.contains("serviceAccount"))
    }

    @Test
    fun theCredentialKeepsTheKeyInAnotherZone() {
        val credential = GCPCredential(serviceAccountJson = "{}", project = "example-project", zone = "europe-west1-b")
        val moved = TalosJson.decodeFromString(
            GCPCredential.serializer(),
            TalosJson.encodeToString(GCPCredential.serializer(), credential.copy(zone = "europe-west1-c")),
        )
        assertEquals("{}", moved.serviceAccountJson)
        assertEquals("europe-west1-c", moved.zone)
    }

    @Test
    fun theKeyShowsItsEmailAndProjectOnly() {
        val key = """{"type":"service_account","project_id":"example-project","private_key":"-----BEGIN PRIVATE KEY-----\nAAA\n-----END PRIVATE KEY-----\n",
            "client_email":"node-creator@example-project.iam.gserviceaccount.com"}"""
        val identity = gcpKeyIdentity(key)
        assertEquals("node-creator@example-project.iam.gserviceaccount.com", identity?.clientEmail)
        assertEquals("example-project", identity?.projectId)
        assertNull(gcpKeyIdentity("""{"type":"authorized_user","client_email":"a@b.c","private_key":"k"}"""))
        assertNull(gcpKeyIdentity("not json"))
    }

    @Test
    fun theHostNameFollowsComputeEngineNames() {
        assertTrue(gcpValidName("node-c"))
        assertFalse(gcpValidName("Node-C"))
        assertFalse(gcpValidName("1node"))
        assertFalse(gcpValidName("node-"))
        assertFalse(gcpValidName("a".repeat(64)))
    }

    @Test
    fun theActivationLinkIsFoundInTheCoresMessage() {
        val message = "the Compute Engine API is not enabled in project example-project: enable it at " +
            "https://console.developers.google.com/apis/api/compute.googleapis.com/overview?project=example-project"
        assertEquals(
            "https://console.developers.google.com/apis/api/compute.googleapis.com/overview?project=example-project",
            gcpActivationLink(message),
        )
        assertNull(gcpActivationLink("Google Cloud refused compute.instances.create"))
    }

    @Test
    fun aRunWithAVMHasTheCloudTimeline() {
        val spec = GCPCreateSpec(
            project = "example-project", zone = "europe-west1-b", machineType = "e2-medium", image = "img", subnetwork = "sub",
            associatePublicIp = true, diskGB = 20, hostname = "node-c",
        )
        val plain = JoinRequest(address = "", template = "", role = "", hostname = "node-c", disk = "", staticAddress = "", gcp = spec)
        assertTrue(plain.cloud)
        assertEquals(JoinPhase.cloudRun, plain.phases)
        assertEquals(JoinPhase.cloudOmniRun, plain.copy(omni = true).phases)
    }

    @Test
    fun theImageSpecUsesTheCoresNames() {
        val spec = GCPImageSpec(
            templateNode = "10.0.0.2", arch = "arm64", project = "example-project", zone = "europe-west1-b",
            subnetwork = "https://example.test/subnetworks/default", associatePublicIp = false,
        )
        val json = TalosJson.encodeToString(GCPImageSpec.serializer(), spec)
        listOf(
            "\"templateNode\":\"10.0.0.2\"", "\"arch\":\"arm64\"", "\"project\":\"example-project\"", "\"zone\":\"europe-west1-b\"",
            "\"subnetwork\":\"https://example.test/subnetworks/default\"", "\"associatePublicIp\":false",
        ).forEach { assertTrue(json, json.contains(it)) }
    }

    @Test
    fun theImageResultAndItsEstimateDecode() {
        val result = TalosJson.decodeFromString(
            GCPImageResult.serializer(),
            """{"image":"talos-v1-12-0-gcp-amd64-0123456789ab","selfLink":"https://example.test/global/images/talos-v1-12-0-gcp-amd64-0123456789ab",
               "arch":"amd64","talosVersion":"v1.12.0","schematic":"0123","sizeGB":12}""",
        )
        assertEquals("v1.12.0", result.talosVersion)
        assertEquals(12, result.sizeGB)
        val made: MadeImage = result
        assertEquals("https://example.test/global/images/talos-v1-12-0-gcp-amd64-0123456789ab", made.imageId)
        assertEquals("talos-v1-12-0-gcp-amd64-0123456789ab", made.name)
        assertEquals("talos-a", GCPImageResult(image = "talos-a").imageId)
        val estimate = TalosJson.decodeFromString(
            GCPImageEstimate.serializer(),
            """{"machineType":"e2-small","hourly":"0.0168","currency":"USD","diskMonthlyPerGB":"0.1000","imageMonthlyPerGB":"0.0500","minutes":10}""",
        )
        assertEquals("e2-small", estimate.machineType)
        assertEquals("0.0168", estimate.hourly)
        assertEquals("0.0500", estimate.imageMonthlyPerGB)
        assertEquals(10, estimate.minutes)
        val unpriced = TalosJson.decodeFromString(GCPImageEstimate.serializer(), """{"machineType":"t2a-standard-1","minutes":10}""")
        assertEquals("", unpriced.hourly)
        assertEquals("", unpriced.imageMonthlyPerGB)
    }

    @Test
    fun anImageRunHasItsOwnSteps() {
        val spec = GCPImageSpec(arch = "amd64", project = "example-project", zone = "europe-west1-b", subnetwork = "s", associatePublicIp = true)
        val request = JoinRequest(address = "", template = "", role = "", hostname = "Talos amd64", disk = "", staticAddress = "", gcpImage = spec)
        assertEquals(ImageRunKind.GCP_IMAGE, request.imageKind)
        assertEquals(JoinPhase.gcpImageRun, request.phases)
        assertTrue(request.imageRun)
        assertFalse(request.cloud)
        val steps = joinTimeline(listOf(JoinProgress(phase = "creating"), JoinProgress(phase = "register")), finished = false, failed = false, phases = request.phases)
        assertEquals(listOf(JoinPhase.CREATING, JoinPhase.WRITING, JoinPhase.REGISTER, JoinPhase.CLEANUP, JoinPhase.DONE), steps.map { it.phase })
        assertEquals(StepStatus.DONE, steps[1].status)
        assertEquals(StepStatus.CURRENT, steps[2].status)
        assertEquals(StepStatus.PENDING, steps[3].status)
    }
}
