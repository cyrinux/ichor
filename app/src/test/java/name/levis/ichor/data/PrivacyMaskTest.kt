package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PrivacyMaskTest {

    @Test
    fun wordsAreTrimmedAndDeduplicated() {
        assertEquals("", PrivacyMask(true, "").words)
        assertEquals("", PrivacyMask(true, " , ,").words)
        assertEquals("acme,prod.example.com", PrivacyMask(true, " acme ,prod.example.com,, acme").words)
    }

    @Test
    fun offByDefault() {
        val mask = UiPreferences(MemoryPrefs()).privacyMask.value
        assertFalse(mask.enabled)
        assertEquals("", mask.extraWords)
    }

    @Test
    fun persistsAcrossInstances() {
        val prefs = MemoryPrefs()
        UiPreferences(prefs).setPrivacyMask(PrivacyMask(enabled = true, extraWords = "acme, bank"))
        val reloaded = UiPreferences(prefs).privacyMask.value
        assertTrue(reloaded.enabled)
        assertEquals("acme, bank", reloaded.extraWords)
    }

    @Test
    fun publishesChanges() {
        val ui = UiPreferences(MemoryPrefs())
        ui.setPrivacyMask(PrivacyMask(enabled = true))
        assertTrue(ui.privacyMask.value.enabled)
        ui.setPrivacyMask(PrivacyMask(enabled = false, extraWords = "x"))
        assertEquals(PrivacyMask(false, "x"), ui.privacyMask.value)
    }
}
