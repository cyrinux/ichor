package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeConfigDataTest {

    private val json = """
        {"kind":"Secret","type":"kubernetes.io/tls",
         "keys":[{"key":"tls.crt","size":1200,"hint":"pem","cert":{"subject":"CN=shop.example.com","issuer":"CN=ca.example.com",
                  "notBefore":1000,"notAfter":2000000,"dnsNames":["shop.example.com"],"count":2}},
                 {"key":"token","size":16,"hint":"text"}],
         "registries":[],"usedBy":[{"pod":"web-1","via":["env","volume"]}],"future":"ignored"}
    """.trimIndent()

    @Test
    fun decodesTheGoData() {
        val d = TalosJson.decodeFromString(KubeConfigData.serializer(), json)
        assertEquals("kubernetes.io/tls", d.type)
        assertEquals(2, d.keys.size)
        assertEquals(2, d.keys[0].cert?.count)
        assertNull(d.keys[1].cert)
        assertFalse(d.keys[1].revealed)
        assertEquals(listOf("env", "volume"), d.usedBy.single().via)
        assertFalse(d.usedByUnknown)
    }

    @Test
    fun revealedKeyReplacesItsRow() {
        val d = TalosJson.decodeFromString(KubeConfigData.serializer(), json)
        val shown = ConfigKey("token", 16, ConfigKey.HINT_TEXT, revealed = true, value = "not-a-real-token")
        val rows = d.withRevealed(shown)
        assertEquals("not-a-real-token", rows[1].value)
        assertTrue(rows[1].revealed)
        assertFalse(rows[0].revealed)
        assertEquals(d.keys, d.withRevealed(null))
    }

    @Test
    fun certificateToneByExpiry() {
        val day = 86_400L
        val cert = ConfigCert(notAfter = 100 * day)
        assertEquals(CellTone.OK, cert.tone(10 * day))
        assertEquals(CellTone.WARN, cert.tone(90 * day))
        assertEquals(CellTone.BAD, cert.tone(100 * day))
        assertEquals(-1, cert.daysLeft(100 * day + 1))
    }

    @Test
    fun onlySecretsAndConfigMapsHaveData() {
        val secret = KubeObjectRef("", "v1", "secrets", "Secret", "shop", "db", editable = true)
        val configMap = secret.copy(resource = "configmaps", kind = "ConfigMap")
        assertTrue(secret.hasConfigData)
        assertEquals("Secret", secret.configDataKind)
        assertTrue(configMap.hasConfigData)
        assertEquals("ConfigMap", configMap.configDataKind)
        assertFalse(KubeObjectRef.pod("shop", "web-1").hasConfigData)
        assertFalse(secret.copy(group = "example.com").hasConfigData)
    }
}
