package name.levis.ichor.update

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SignersTest {

    @Test
    fun onlyIdenticalNonEmptySignerSetsMatch() {
        assertTrue(sameSigners(setOf("a"), setOf("a")))
        assertFalse(sameSigners(setOf("a"), setOf("b")))
        assertFalse("extra signer", sameSigners(setOf("a"), setOf("a", "b")))
        assertFalse("unsigned APK", sameSigners(setOf("a"), emptySet()))
        assertFalse("nothing to compare against", sameSigners(emptySet(), emptySet()))
    }

    @Test
    fun aKeyRotatedFromTheInstalledOneMatches() {
        assertTrue("lineage carries the installed key", sameSigners(setOf("old"), setOf("new"), setOf("old", "new")))
        assertFalse("lineage of another key", sameSigners(setOf("old"), setOf("new"), setOf("other", "new")))
        assertFalse("no lineage", sameSigners(setOf("old"), setOf("new"), emptySet()))
        assertFalse("several installed signers need the exact set", sameSigners(setOf("a", "b"), setOf("c"), setOf("a", "c")))
        assertFalse("nothing installed", sameSigners(emptySet(), setOf("new"), setOf("new")))
    }

    @Test
    fun sha256OfKnownInput() {
        assertEquals("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", sha256Hex(ByteArray(0)))
    }
}
