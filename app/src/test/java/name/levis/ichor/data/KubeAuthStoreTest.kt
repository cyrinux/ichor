package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class KubeAuthStoreTest {

    @Test
    fun savesLoadsAndDeletesByFingerprint() {
        val file = MemoryValue()
        val store = KubeAuthStore(file)

        assertEquals("", store.load("aaaa"))
        store.save("aaaa", """{"method":"oidc"}""")
        store.save("bbbb", """{"method":"eks"}""")
        assertEquals("""{"method":"oidc"}""", store.load("aaaa"))
        // A new store reads the same file back.
        assertEquals("""{"method":"eks"}""", KubeAuthStore(file).load("bbbb"))

        store.save("aaaa", "")
        assertEquals("", store.load("aaaa"))
        assertEquals(mapOf("bbbb" to """{"method":"eks"}"""), decodeAuthStates(file.value))

        store.save("bbbb", "")
        assertNull("the last state gone, so is the file", file.value)
    }

    @Test
    fun retainForgetsRemovedClusters() {
        val store = KubeAuthStore(MemoryValue())
        store.save("aaaa", "a")
        store.save("bbbb", "b")

        store.retain(listOf("bbbb", "cccc"))

        assertEquals(mapOf("bbbb" to "b"), store.all())
    }

    @Test
    fun restoreKeepsWhatThisDeviceSignedIn() {
        val store = KubeAuthStore(MemoryValue())
        store.save("aaaa", "signed in here")

        store.restore(mapOf("aaaa" to "from backup", "bbbb" to "keys", "cccc" to ""))

        assertEquals(mapOf("aaaa" to "signed in here", "bbbb" to "keys"), store.all())
    }

    @Test
    fun unreadableFileIsNeverOverwritten() {
        val file = object : SealedValue {
            var writes = 0
            override fun read(): String? = error("Keystore busy")
            override fun write(value: String) {
                writes++
            }
            override fun delete() = Unit
        }
        val store = KubeAuthStore(file)

        assertEquals("", store.load("aaaa"))
        store.save("aaaa", "state")
        store.retain(emptyList())

        assertEquals(0, file.writes)
    }

    @Test
    fun failedWriteKeepsTheStateForThisRun() {
        val file = MemoryValue().apply { failWrites = true }
        val store = KubeAuthStore(file)

        store.save("aaaa", "state")

        assertEquals("state", store.load("aaaa"))
        assertNull(file.value)
    }

    @Test
    fun clearForgetsEverything() {
        val file = MemoryValue()
        val store = KubeAuthStore(file)
        store.save("aaaa", "a")

        store.clear()

        assertEquals(emptyMap<String, String>(), store.all())
        assertNull(file.value)
    }

    @Test
    fun withAuthStateSetsAndRemoves() {
        val states = mapOf("aaaa" to "a")
        assertEquals(mapOf("aaaa" to "a", "bbbb" to "b"), withAuthState(states, "bbbb", "b"))
        assertEquals(emptyMap<String, String>(), withAuthState(states, "aaaa", " "))
        assertEquals(states, withAuthState(states, "", "x"))
        assertEquals(states, decodeAuthStates(encodeAuthStates(states)))
        assertEquals(emptyMap<String, String>(), decodeAuthStates(null))
    }
}
