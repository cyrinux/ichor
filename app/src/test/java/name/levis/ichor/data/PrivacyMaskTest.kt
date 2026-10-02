package name.levis.ichor.data

import android.content.SharedPreferences
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

/** In-memory SharedPreferences: unit tests run without the Android framework. */
private class MemoryPrefs : SharedPreferences {
    private val values = mutableMapOf<String, Any?>()

    override fun getAll(): Map<String, *> = values.toMap()
    override fun getString(key: String, defValue: String?) = values[key] as String? ?: defValue
    @Suppress("UNCHECKED_CAST")
    override fun getStringSet(key: String, defValues: Set<String>?) = values[key] as Set<String>? ?: defValues
    override fun getInt(key: String, defValue: Int) = values[key] as Int? ?: defValue
    override fun getLong(key: String, defValue: Long) = values[key] as Long? ?: defValue
    override fun getFloat(key: String, defValue: Float) = values[key] as Float? ?: defValue
    override fun getBoolean(key: String, defValue: Boolean) = values[key] as Boolean? ?: defValue
    override fun contains(key: String) = key in values
    override fun registerOnSharedPreferenceChangeListener(l: SharedPreferences.OnSharedPreferenceChangeListener) = Unit
    override fun unregisterOnSharedPreferenceChangeListener(l: SharedPreferences.OnSharedPreferenceChangeListener) = Unit

    override fun edit(): SharedPreferences.Editor = object : SharedPreferences.Editor {
        private val pending = mutableMapOf<String, Any?>()
        private val removed = mutableSetOf<String>()
        private var clear = false

        override fun putString(key: String, value: String?) = apply { pending[key] = value }
        override fun putStringSet(key: String, values: Set<String>?) = apply { pending[key] = values }
        override fun putInt(key: String, value: Int) = apply { pending[key] = value }
        override fun putLong(key: String, value: Long) = apply { pending[key] = value }
        override fun putFloat(key: String, value: Float) = apply { pending[key] = value }
        override fun putBoolean(key: String, value: Boolean) = apply { pending[key] = value }
        override fun remove(key: String) = apply { removed += key }
        override fun clear() = apply { clear = true }
        override fun commit(): Boolean {
            if (clear) values.clear()
            removed.forEach(values::remove)
            values.putAll(pending)
            return true
        }
        override fun apply() {
            commit()
        }
    }
}
