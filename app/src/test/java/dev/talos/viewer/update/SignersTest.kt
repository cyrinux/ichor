package dev.talos.viewer.update

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
    fun sha256OfKnownInput() {
        assertEquals("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", sha256Hex(ByteArray(0)))
    }
}
