package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PanelSuggestionTest {
    @Test fun decodesWhatGoSends() {
        val raw = """{"id":"","title":"CPU","query":"sum(rate(node_cpu_seconds_total[5m]))","unit":"cores","legend":"{{instance}}","verified":true,"empty":false,"attempts":2,"notice":"","future":1}"""
        val s = TalosJson.decodeFromString(PanelSuggestion.serializer(), raw)
        assertEquals("CPU", s.title)
        assertEquals("cores", s.unit)
        assertTrue(s.verified)
        assertFalse(s.empty)
        assertEquals(2, s.attempts)

        // A panel that was not checked carries why; the rest keeps its defaults.
        val unchecked = TalosJson.decodeFromString(PanelSuggestion.serializer(), """{"title":"T","query":"up","notice":"not checked: timeout"}""")
        assertFalse(unchecked.verified)
        assertEquals(1, unchecked.attempts)
        assertEquals("not checked: timeout", unchecked.notice)
    }

    @Test fun usedPanelKeepsTheEditedId() {
        val s = PanelSuggestion(title = "T", query = "up", unit = "count", legend = "{{job}}")
        assertEquals(PromPanel("abc", "T", "up", "count", "{{job}}"), s.toPanel("abc"))
        assertEquals("", s.toPanel("").id)
    }

    @Test fun currentPanelGoesToGoWithoutItsId() {
        val json = PromPanel("abc", "T", "up", legend = "{{job}}").toGoPanelJson()
        assertFalse(json.contains("abc"))
        assertTrue(json.contains("\"query\":\"up\""))
        assertTrue(json.contains("\"unit\":\"\""))
    }
}
