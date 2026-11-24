package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudScalewayTest {

    // ScalewayOptions' shape (go/ichorgo/cloud_scaleway.go), with invented ids.
    private val options = TalosJson.decodeFromString(
        ScalewayOptions.serializer(),
        """{"project":"00000000-0000-4000-8000-000000000001","zone":"fr-par-1","zones":["fr-par-1","nl-ams-1"],
           "types":[{"name":"DEV1-S","arch":"x86_64","vcpus":2,"ramGB":2,"localDiskGB":20,"hourly":"0.0099","currency":"EUR"},
                    {"name":"COPARM1-2C-8G","arch":"arm64","vcpus":2,"ramGB":8,"hourly":"0.0426","currency":"EUR","blockOnly":true},
                    {"name":"PLAY2-NANO","arch":"x86_64","vcpus":1,"ramGB":2,"hourly":""}],
           "images":[{"id":"img-ubuntu","name":"ubuntu","arch":"x86_64","sizeGB":10},
                     {"id":"img-talos","name":"talos v1.12.0 scaleway","arch":"x86_64","sizeGB":10,"talos":true}],
           "securityGroups":[{"id":"sg-web","name":"web"},{"id":"sg-default","name":"Default security group","default":true}],
           "warnings":["Plain Talos: the node's security group must let the phone reach TCP 50000"],"futureField":1}""",
    )

    @Test
    fun decodesTheCoreShape() {
        assertEquals(listOf("fr-par-1", "nl-ams-1"), options.zones)
        assertEquals(3, options.types.size)
        assertTrue(options.types[1].blockOnly)
        assertEquals(1, options.warnings.size)
        val check = TalosJson.decodeFromString(
            ScalewayCheck.serializer(),
            """{"project":"p","zone":"fr-par-1","canCreate":true,"checked":true,"account":"SCWEXAMPLEKEY"}""",
        )
        assertTrue(check.canCreate && check.checked)
    }

    @Test
    fun talosImagesComeFirst() {
        assertEquals(listOf("img-talos", "img-ubuntu"), options.imagesTalosFirst.map { it.id })
    }

    @Test
    fun typesFollowTheImageArchitecture() {
        val talos = options.images.first { it.talos }
        assertEquals(listOf("DEV1-S", "PLAY2-NANO"), options.typesFor(talos).map { it.name })
        assertEquals(listOf("COPARM1-2C-8G"), options.typesFor(talos.copy(arch = "arm64")).map { it.name })
        // No image yet: every type.
        assertEquals(3, options.typesFor(null).size)
    }

    @Test
    fun theDefaultSecurityGroupIsPreselected() {
        assertEquals("sg-default", options.defaultSecurityGroup?.id)
        assertEquals("sg-web", options.copy(securityGroups = options.securityGroups.take(1)).defaultSecurityGroup?.id)
        assertNull(options.copy(securityGroups = emptyList()).defaultSecurityGroup)
    }

    @Test
    fun theManagedGroupIsPreselected() {
        val managed = ScalewaySecurityGroup(id = "sg-nodes", name = "ichor-nodes", managed = true)
        assertEquals("sg-nodes", options.copy(securityGroups = options.securityGroups + managed).defaultSecurityGroup?.id)
        val decoded = TalosJson.decodeFromString(
            ScalewayOptions.serializer(),
            """{"securityGroups":[{"id":"sg-nodes","name":"ichor-nodes","managed":true}],"groupWarning":"Plain Talos: check port 50000"}""",
        )
        assertTrue(decoded.securityGroups[0].managed)
        assertEquals("Plain Talos: check port 50000", decoded.groupWarning)
    }

    @Test
    fun decodesTheGuidedSecurityGroup() {
        // ScalewayNetworkPrepare's shape (go/ichorgo/cloud_scaleway_network.go), with documentation addresses.
        val network = TalosJson.decodeFromString(
            ScalewayNetwork.serializer(),
            """{"securityGroupId":"sg-nodes","name":"ichor-nodes","created":false,"phoneIp":"203.0.113.7",
               "rules":[{"protocol":"all","port":0,"source":"198.51.100.10/32","description":"cp-1"},
                        {"protocol":"tcp","port":50000,"source":"203.0.113.7/32","description":"this phone"},
                        {"protocol":"tcp","port":50000,"source":"203.0.113.9/32"},
                        {"protocol":"udp","port":51820,"source":"192.0.2.0/24"}],
               "warnings":["the member worker-2 has no public IPv4"]}""",
        )
        assertEquals("sg-nodes", network.securityGroupId)
        val (member, phone, older, other) = network.rules
        assertTrue(member.isMember && !member.isPhone(network.phoneIp))
        assertTrue(phone.isPhone(network.phoneIp) && !phone.isMember)
        // An older address of the phone is a plain rule: listed, never removed.
        assertTrue(!older.isPhone(network.phoneIp) && !older.isMember)
        assertEquals("udp/51820 ← 192.0.2.0/24", other.plain)
        assertEquals(1, network.warnings.size)
        val spec = TalosJson.encodeToString(ScalewayNetworkSpec.serializer(), ScalewayNetworkSpec(zone = "nl-ams-1", role = "worker", omni = true))
        assertEquals("""{"zone":"nl-ams-1","role":"worker","omni":true}""", spec)
    }

    @Test
    fun costLine() {
        assertEquals("0.0099 EUR", options.types[0].hourlyPrice)
        assertNull(options.types[2].hourlyPrice)
        assertEquals("0.01", ScalewayType(hourly = "0.01").hourlyPrice)
    }

    @Test
    fun theSpecAndTheCredentialEncodeAsTheCoreReadsThem() {
        val spec = ScalewayCreateSpec(zone = "nl-ams-1", type = "DEV1-S", imageId = "img-talos", hostname = "worker-4")
        val json = TalosJson.encodeToString(ScalewayCreateSpec.serializer(), spec)
        assertTrue(json, json.contains("\"zone\":\"nl-ams-1\"") && json.contains("\"imageId\":\"img-talos\""))
        val credential = ScalewayCredential("SCWEXAMPLEKEY", "secret", "p", "fr-par-1").inZone("pl-waw-1")
        assertEquals("pl-waw-1", credential.zone)
        assertEquals("secret", credential.secretKey)
    }

    @Test
    fun theImageSpecUsesTheCoresNames() {
        val spec = ScalewayImageSpec(templateNode = "10.0.0.2", arch = "arm64", zone = "nl-ams-1")
        assertEquals(
            """{"templateNode":"10.0.0.2","arch":"arm64","zone":"nl-ams-1"}""",
            TalosJson.encodeToString(ScalewayImageSpec.serializer(), spec),
        )
    }

    @Test
    fun theImageResultAndItsCostDecode() {
        val result = TalosJson.decodeFromString(
            ScalewayImageResult.serializer(),
            """{"imageId":"00000000-0000-4000-8000-0000000000aa","snapshotId":"s","name":"talos-v1.12.0-scaleway-amd64-0123456789ab",
               "arch":"amd64","talosVersion":"v1.12.0","schematic":"0123","sizeGB":10}""",
        )
        val made: MadeImage = result
        assertEquals("00000000-0000-4000-8000-0000000000aa", made.imageId)
        assertEquals("talos-v1.12.0-scaleway-amd64-0123456789ab", made.name)
        val cost = TalosJson.decodeFromString(
            ScalewayImageCost.serializer(),
            """{"type":"DEV1-S","hourly":"0.0099","minutes":10,"currency":"EUR","snapshotGB":10,"note":"n"}""",
        )
        assertEquals("DEV1-S", cost.type)
        assertEquals(10, cost.snapshotGB)
        assertEquals("", TalosJson.decodeFromString(ScalewayImageCost.serializer(), """{"type":"COPARM1-2C-8G"}""").hourly)
    }

    @Test
    fun theImageArchitecturesAndTheTalosImageOfEach() {
        assertEquals("amd64", scalewayArch("x86_64"))
        assertEquals("arm64", scalewayArch("aarch64"))
        assertEquals(listOf("amd64", "arm64"), options.imageArches)
        assertTrue(options.hasTalosImage("amd64"))
        assertTrue(options.hasTalosImage("x86_64"))
        assertTrue(options.hasTalosImage(""))
        assertFalse(options.hasTalosImage("arm64"))
        assertFalse(options.copy(images = options.images.filterNot { it.talos }).hasTalosImage(""))
    }

    @Test
    fun anImageRunHasItsOwnSteps() {
        val spec = ScalewayImageSpec(arch = "amd64", zone = "fr-par-1")
        val request = JoinRequest(address = "", template = "", role = "", hostname = "Talos amd64", disk = "", staticAddress = "", scalewayImage = spec)
        assertEquals(ImageRunKind.SCALEWAY_IMAGE, request.imageKind)
        assertEquals(listOf(JoinPhase.CREATING, JoinPhase.WRITING, JoinPhase.SNAPSHOT, JoinPhase.REGISTER, JoinPhase.CLEANUP, JoinPhase.DONE), request.phases)
        assertTrue(request.imageRun)
        assertFalse(request.cloud)
    }
}
