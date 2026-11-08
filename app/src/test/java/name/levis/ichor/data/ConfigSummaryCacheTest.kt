package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.security.SecurityKeyRequiredException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class ConfigSummaryCacheTest {

    private val summary = ConfigSummary(
        current = "lab",
        contexts = listOf(
            ContextSummary(name = "lab", fingerprint = "fp1", endpoints = listOf("10.0.0.2"), certNotAfter = 1_900_000_000),
            ContextSummary(name = "edge", fingerprint = "fp2"),
        ),
    )
    private val key = SummaryKey(talosDigest = "aaa", kubeDigest = "", maskGeneration = 0)

    @Test
    fun hitReturnsTheStoredSummary() {
        val sealed = MemoryValue()
        ConfigSummaryCache(sealed).store(key, summary)
        // A new instance stands for the next launch: the record comes from the sealed file.
        assertEquals(summary, ConfigSummaryCache(sealed).lookup(key))
    }

    @Test
    fun missWhenATextChanged() {
        val cache = ConfigSummaryCache(MemoryValue())
        cache.store(key, summary)
        assertNull(cache.lookup(key.copy(talosDigest = "bbb")))
        assertNull(cache.lookup(key.copy(kubeDigest = "ccc")))
    }

    @Test
    fun missWhenTheMaskGenerationChanged() {
        val cache = ConfigSummaryCache(MemoryValue())
        cache.store(key, summary)
        assertNull(cache.lookup(key.copy(maskGeneration = 42)))
        cache.store(key.copy(maskGeneration = 42), summary)
        assertNull(cache.lookup(key))
        assertNotNull(cache.lookup(key.copy(maskGeneration = 42)))
    }

    @Test
    fun aRecordThatDoesNotDecodeIsDeleted() {
        val sealed = MemoryValue("not json")
        assertNull(ConfigSummaryCache(sealed).lookup(key))
        assertNull(sealed.value)
    }

    @Test
    fun aSummaryThatDoesNotDecodeIsDeleted() {
        val sealed = MemoryValue(
            TalosJson.encodeToString(CachedSummary.serializer(), CachedSummary("aaa", "", 0, "{broken")),
        )
        assertNull(ConfigSummaryCache(sealed).lookup(key))
        assertNull(sealed.value)
    }

    @Test
    fun aFileThatDoesNotDecryptIsDeletedNotThrown() {
        val sealed = FailingValue(IllegalStateException("bad tag"))
        assertNull(ConfigSummaryCache(sealed).lookup(key))
        assertEquals(1, sealed.deletes)
    }

    @Test
    fun aRecordSealedWithAnUntappedKeyIsKept() {
        val sealed = FailingValue(SecurityKeyRequiredException())
        assertNull(ConfigSummaryCache(sealed).lookup(key))
        assertEquals(0, sealed.deletes)
    }

    @Test
    fun aFailedWriteLeavesNoStaleRecord() {
        val sealed = MemoryValue()
        val cache = ConfigSummaryCache(sealed)
        cache.store(key, summary)
        sealed.failWrites = true
        cache.store(key.copy(talosDigest = "bbb"), summary)
        assertNull(sealed.value)
        assertNull(cache.lookup(key))
    }

    @Test
    fun clearDeletesTheRecord() {
        val sealed = MemoryValue()
        val cache = ConfigSummaryCache(sealed)
        cache.store(key, summary)
        cache.clear()
        assertNull(sealed.value)
        assertNull(cache.lookup(key))
    }

    /** A sealed file whose read throws [failure]. */
    private class FailingValue(private val failure: Exception) : SealedValue {
        var deletes = 0

        override fun read(): String? = throw failure
        override fun write(value: String) = Unit
        override fun delete() {
            deletes++
        }
    }
}
