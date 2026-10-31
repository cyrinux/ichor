package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudProviderStoreTest {
    private class MemoryValue(var value: String? = null, val failReads: Boolean = false) : SealedValue {
        override fun read(): String? = if (failReads) error("locked") else value
        override fun write(value: String) {
            this.value = value
        }
        override fun delete() {
            value = null
        }
        override fun reseal() = Unit
    }

    @Test
    fun keepsOneTokenPerProviderAndProject() {
        val file = MemoryValue()
        val store = CloudProviderStore(file)
        store.save(CloudProviderStore.HETZNER, "scratch", " token-a ")
        store.save(CloudProviderStore.HETZNER, "another", "token-b")
        store.save("other", "scratch", "token-c")

        assertEquals(listOf("another", "scratch"), store.projects(CloudProviderStore.HETZNER))
        assertEquals("token-a", CloudProviderStore(file).token(CloudProviderStore.HETZNER, "scratch"))
    }

    @Test
    fun aBlankTokenRemovesTheProjectAndTheLastOneTheFile() {
        val file = MemoryValue()
        val store = CloudProviderStore(file)
        store.save(CloudProviderStore.HETZNER, "scratch", "token-a")
        store.save(CloudProviderStore.HETZNER, "scratch", "")

        assertTrue(store.projects(CloudProviderStore.HETZNER).isEmpty())
        assertNull(file.value)
    }

    @Test
    fun anUnreadableFileIsNeverOverwritten() {
        val file = MemoryValue(value = "sealed", failReads = true)

        assertFalse(CloudProviderStore(file).save(CloudProviderStore.HETZNER, "scratch", "token-a"))
        assertEquals("sealed", file.value)
    }
}
