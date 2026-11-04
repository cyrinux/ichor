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
    fun awsKeysAreKeptPerAccountApartFromTheOtherProviders() {
        val store = CloudProviderStore(MemoryValue())
        val keys = """{"accessKeyId":"AKIAEXAMPLE","secretAccessKey":"secret","region":"eu-west-1"}"""
        store.save(CloudProviderStore.AWS, "000000000000", keys)
        store.save(CloudProviderStore.OVH, "scratch", "{}")

        assertEquals(listOf("000000000000"), store.projects(CloudProviderStore.AWS))
        assertEquals(keys, store.token(CloudProviderStore.AWS, "000000000000"))
        assertEquals(listOf("scratch"), store.projects(CloudProviderStore.OVH))
    }

    @Test
    fun azurePrincipalsAreKeptPerSubscriptionApartFromTheOtherProviders() {
        val store = CloudProviderStore(MemoryValue())
        val principal = """{"tenantId":"t","clientId":"c","clientSecret":"secret","subscriptionId":"00000000-0000-0000-0000-000000000000"}"""
        store.save(CloudProviderStore.AZURE, "00000000-0000-0000-0000-000000000000", principal)
        store.save(CloudProviderStore.AWS, "000000000000", "{}")

        assertEquals(listOf("00000000-0000-0000-0000-000000000000"), store.projects(CloudProviderStore.AZURE))
        assertEquals(principal, store.token(CloudProviderStore.AZURE, "00000000-0000-0000-0000-000000000000"))
        assertEquals(listOf("000000000000"), store.projects(CloudProviderStore.AWS))
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
