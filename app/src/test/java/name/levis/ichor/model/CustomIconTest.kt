package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class CustomIconTest {
    private val png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

    @Test
    fun decodesAnInlinePng() {
        val icon = customIcon("data:image/png;base64,$png")
        assertTrue(icon is CustomIcon.Inline)
        assertEquals(Base64.getDecoder().decode(png).size, (icon as CustomIcon.Inline).bytes.size)
    }

    @Test
    fun acceptsAnHttpsUrl() {
        val icon = customIcon("https://git.example.org/logo.png")
        assertEquals("https://git.example.org/logo.png", (icon as CustomIcon.Url).url)
    }

    @Test
    fun keysDifferPerIcon() {
        assertNotEquals(customIcon("https://a.example/1.png")!!.key, customIcon("https://a.example/2.png")!!.key)
    }

    @Test
    fun refusesAnythingElse() {
        val big = Base64.getEncoder().encodeToString(ByteArray(CUSTOM_ICON_MAX_BYTES + 1))
        listOf(
            "",
            "grafana",
            "http://example.org/a.png",
            "https://user:pw@example.org/a.png",
            "https:///a.png",
            "file:///etc/passwd",
            "data:image/svg+xml;base64,PHN2Zy8+",
            "data:image/png;base64,***",
            "data:image/png;base64,$big",
        ).forEach { assertNull(it, customIcon(it)) }
    }

    @Test
    fun argoAppCarriesTheIconUrl() {
        val app = TalosJson.decodeFromString(ArgoApp.serializer(), """{"name":"web","iconUrl":"https://a.example/w.png"}""")
        assertEquals("https://a.example/w.png", app.iconUrl)
    }
}
