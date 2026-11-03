package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OllamaPullTest {
    @Test
    fun fractionIsNullBeforeTheFirstLayer() {
        assertNull(OllamaPullProgress("pulling manifest").fraction)
    }

    @Test
    fun fractionIsTheShareDoneClamped() {
        assertEquals(0.25f, OllamaPullProgress("pulling aa", 250, 1000).fraction!!, 0.0001f)
        assertEquals(1f, OllamaPullProgress("success", 1200, 1000).fraction!!, 0.0001f)
    }

    @Test
    fun modelNamesAsTheCoreAcceptsThem() {
        listOf("qwen2.5:0.5b", "llama3.2", "hf.co/org/model:q4_k_m").forEach { assertTrue(it, isOllamaModelName(it)) }
        listOf("", "a b", "m:t:x", "../etc", "m;rm", "a".repeat(129)).forEach { assertFalse(it, isOllamaModelName(it)) }
    }

    @Test
    fun aPullBelongsToItsServer() {
        val pull = OllamaPullState("ai", "ollama", "qwen2.5:0.5b")
        assertTrue(pull.isFor("ai", "ollama"))
        assertFalse(pull.isFor("lab", "ollama"))
    }
}
