package name.levis.ichor.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class OllamaForwardTest {
    @Test
    fun aServerRoundTripsThroughTheSettings() {
        val ref = OllamaServerRef("ai", "ollama")
        assertEquals("ai/ollama", ref.text)
        assertEquals(ref, OllamaServerRef.parse(" ai/ollama "))
    }

    @Test
    fun anythingElseIsNoServer() {
        listOf("", "ollama", "a/b/c", "/ollama", "ai/", "http://127.0.0.1:11434/v1").forEach { assertNull(it, OllamaServerRef.parse(it)) }
    }

    @Test
    fun theCoreAnswerDecodes() {
        val json = """{"baseUrl":"http://127.0.0.1:41234/v1","namespace":"ai","server":"ollama","pod":"ollama-7d9f-x2k4q","port":11434}"""
        val forward = Json.decodeFromString(OllamaForward.serializer(), json)
        assertEquals("http://127.0.0.1:41234/v1", forward.baseUrl)
        assertEquals("ai/ollama-7d9f-x2k4q", forward.target)
    }
}
