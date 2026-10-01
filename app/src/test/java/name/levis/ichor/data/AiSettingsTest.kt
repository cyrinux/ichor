package name.levis.ichor.data

import kotlinx.serialization.builtins.ListSerializer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AiSettingsTest {

    @Test
    fun offByDefaultAndAnonymized() {
        val settings = AiSettings()
        assertFalse(settings.enabled)
        assertTrue(settings.anonymize)
        assertEquals("anthropic", settings.provider)
        assertEquals("", settings.model)
        assertEquals("", settings.baseUrl)
    }

    @Test
    fun modelAndServerAreKeptPerProvider() {
        val claude = AiSettings().withModel(" claude-haiku-4-5 ")
        val openai = claude.copy(provider = "openai").withModel("gpt-x").withBaseUrl(" http://192.0.2.9:11434/v1 ")

        assertEquals("gpt-x", openai.model)
        assertEquals("http://192.0.2.9:11434/v1", openai.baseUrl)

        val back = openai.copy(provider = "anthropic")
        assertEquals("claude-haiku-4-5", back.model)
        assertEquals("", back.baseUrl)
    }

    @Test
    fun askingNeedsAKeyOrAServer() {
        assertFalse(canAsk(apiKey = "", baseUrl = " "))
        assertTrue(canAsk(apiKey = "sk-test", baseUrl = ""))
        assertTrue(canAsk(apiKey = "", baseUrl = "http://192.0.2.9:11434/v1"))
    }

    @Test
    fun healthNoteQuotesTheFailure() {
        assertEquals(
            "The cluster health check failed: not healthy after 1m0s: waiting for kubelet",
            healthCheckNote(" not healthy after 1m0s: waiting for kubelet\n"),
        )
    }

    @Test
    fun providersDecodeFromTheGoCore() {
        val json = """[{"id":"anthropic","name":"Anthropic (Claude)","defaultModel":"claude-opus-5-5","keyUrl":"https://console.anthropic.com/settings/keys","future":1}]"""
        val providers = TalosJson.decodeFromString(ListSerializer(AiProvider.serializer()), json)
        assertEquals(listOf(AiProvider("anthropic", "Anthropic (Claude)", "claude-opus-5-5", "https://console.anthropic.com/settings/keys")), providers)
    }
}
