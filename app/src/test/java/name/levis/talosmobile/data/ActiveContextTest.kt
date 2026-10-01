package name.levis.talosmobile.data

import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Test

class ActiveContextTest {

    private fun summary(vararg names: String, current: String = names.first()) =
        ConfigSummary(current = current, contexts = names.map { ContextSummary(name = it) })

    private val real = summary("admin@prod", "admin@staging", current = "admin@prod")
    private val masked = summary("admin@homelab", "admin@homelab-2", current = "admin@homelab")

    @Test
    fun savedPositionSurvivesMasking() {
        // Selected while masked: the saved name is a fake, the position still points right.
        assertEquals("admin@staging", resolveActive(real, "admin@homelab-2", 1))
        assertEquals("admin@homelab-2", resolveActive(masked, "admin@staging", 1))
    }

    @Test
    fun nameWhenNoPositionWasSaved() {
        assertEquals("admin@staging", resolveActive(real, "admin@staging", -1))
    }

    @Test
    fun fallsBackToCurrent() {
        assertEquals("admin@prod", resolveActive(real, "gone", -1))
        assertEquals("admin@prod", resolveActive(real, null, 7))
    }

    @Test
    fun contextAtMapsAcrossMaskChange() {
        assertEquals("admin@homelab-2", contextAt(masked, 1))
        assertEquals("admin@homelab", contextAt(masked, -1))
    }
}
