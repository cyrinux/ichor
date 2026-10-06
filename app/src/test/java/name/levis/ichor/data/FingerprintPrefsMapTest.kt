package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class FingerprintPrefsMapTest {

    @Test
    fun loadsTheStoredValuesOfItsType() {
        val prefs = MemoryPrefs()
        prefs.edit().putString("a", "one").putInt("b", 2).putString("c", "three").apply()
        assertEquals(mapOf("a" to "one", "c" to "three"), FingerprintPrefsMap.strings(prefs).values.value)
        assertEquals(mapOf("b" to 2), FingerprintPrefsMap.ints(prefs).values.value)
    }

    @Test
    fun setWritesForgetsAndIgnoresABlankFingerprint() {
        val prefs = MemoryPrefs()
        val map = FingerprintPrefsMap.strings(prefs)
        map.set("a", "one")
        map.set("", "ignored")
        assertEquals(mapOf("a" to "one"), map.values.value)
        assertEquals("one", prefs.getString("a", null))
        map.set("a", null)
        assertEquals(emptyMap<String, String>(), map.values.value)
        assertEquals(emptyMap<String, Any?>(), prefs.all)
    }

    @Test
    fun storeReplacesTheWholeFile() {
        val prefs = MemoryPrefs()
        val map = FingerprintPrefsMap.ints(prefs)
        map.set("a", 1)
        map.store(mapOf("b" to 2))
        assertEquals(mapOf("b" to 2), map.values.value)
        assertEquals(mapOf<String, Any?>("b" to 2), prefs.all)
        // A second store of the same map leaves it alone.
        map.store(mapOf("b" to 2))
        assertEquals(mapOf("b" to 2), FingerprintPrefsMap.ints(prefs).values.value)
    }
}
