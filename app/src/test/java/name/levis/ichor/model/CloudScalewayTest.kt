package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
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
}
