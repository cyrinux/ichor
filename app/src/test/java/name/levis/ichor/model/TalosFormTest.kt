package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosFormTest {

    private val direct = TalosForm(
        name = " homelab ",
        endpoints = "10.0.0.1\n 10.0.0.2 , cp.example.com\n\n",
        nodes = "",
        ca = "-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----\n",
        crt = "Y3J0",
        key = "a2V5",
    )

    private val omni = TalosForm(
        mode = TalosFormMode.OMNI,
        name = "acme-prod",
        omniUrl = " https://acme.omni.example.com ",
        cluster = "prod",
        identity = "ops@example.com",
    )

    private fun json(form: TalosForm): JsonObject = Json.parseToJsonElement(form.toJson()).jsonObject

    private fun JsonObject.text(key: String) = getValue(key).jsonPrimitive.content

    private fun JsonObject.list(key: String) = getValue(key).jsonArray.map { it.jsonPrimitive.content }

    @Test
    fun directSplitsTheAddressesAndTrims() {
        val out = json(direct)
        assertEquals("direct", out.text("mode"))
        assertEquals("homelab", out.text("name"))
        assertEquals(listOf("10.0.0.1", "10.0.0.2", "cp.example.com"), out.list("endpoints"))
        assertEquals(emptyList<String>(), out.list("nodes"))
        assertEquals("-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----", out.text("ca"))
        assertEquals("Y3J0", out.text("crt"))
        assertEquals("a2V5", out.text("key"))
        // The other mode's fields stay out.
        assertEquals(setOf("mode", "name", "endpoints", "nodes", "ca", "crt", "key"), out.keys)
    }

    @Test
    fun omniSendsOnlyItsFields() {
        val out = json(omni)
        assertEquals("omni", out.text("mode"))
        assertEquals("https://acme.omni.example.com", out.text("omniUrl"))
        assertEquals("prod", out.text("cluster"))
        assertEquals("ops@example.com", out.text("identity"))
        assertEquals(setOf("mode", "name", "omniUrl", "cluster", "identity"), out.keys)
    }

    @Test
    fun nodesAreSplitLikeEndpoints() {
        assertEquals(listOf("10.0.0.5", "10.0.0.6"), direct.copy(nodes = "10.0.0.5,10.0.0.6").nodeList)
    }

    @Test
    fun canSubmitNeedsTheModesFields() {
        assertTrue(direct.canSubmit)
        assertFalse(direct.copy(name = "  ").canSubmit)
        assertFalse(direct.copy(endpoints = " \n, ").canSubmit)
        assertFalse(direct.copy(ca = "").canSubmit)
        assertFalse(direct.copy(crt = "").canSubmit)
        assertFalse(direct.copy(key = " ").canSubmit)

        assertTrue(omni.canSubmit)
        // A service account's identity works as well as an account email.
        assertTrue(omni.copy(identity = "ci@serviceaccount.omni.sidero.dev").canSubmit)
        assertFalse(omni.copy(identity = " ").canSubmit)
        assertFalse(omni.copy(omniUrl = "").canSubmit)
        assertFalse(omni.copy(cluster = " ").canSubmit)
        // Fields of the other mode do not count.
        assertFalse(TalosForm(mode = TalosFormMode.OMNI, name = "x", endpoints = "10.0.0.1").canSubmit)
    }

    @Test
    fun toStringHidesTheKey() {
        assertFalse(direct.toString().contains("a2V5"))
    }
}
