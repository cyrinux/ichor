package name.levis.ichor.data

import name.levis.ichor.security.SealedFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.GeneralSecurityException
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class EnvelopeFormatTest {

    private val random = SecureRandom()
    private val key = aesKey()

    /** A config the size of a few dozen contexts: certificates and tokens, made up. */
    private val config = buildString {
        repeat(40) { append("- name: sample-$it\n  token: ${"x".repeat(900)}\n") }
    }.encodeToByteArray()

    private fun aesKey(): SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()

    @Test
    fun envelopeRoundTrips() {
        val stored = EnvelopeFormat.encrypt(config, key)
        assertTrue(EnvelopeFormat.isEnvelope(stored))
        assertArrayEquals(config, EnvelopeFormat.decrypt(stored, key))
    }

    @Test
    fun eachWriteHasItsOwnDataKey() {
        val first = EnvelopeFormat.encrypt(config, key)
        val second = EnvelopeFormat.encrypt(config, key)
        assertNotEquals(first.toList(), second.toList())
        assertArrayEquals(EnvelopeFormat.decrypt(first, key), EnvelopeFormat.decrypt(second, key))
    }

    @Test
    fun legacyFileStillReads() {
        val legacy = EnvelopeFormat.encryptLegacy(config, key)
        assertFalse(EnvelopeFormat.isEnvelope(legacy))
        assertArrayEquals(config, EnvelopeFormat.decrypt(legacy, key))
    }

    @Test
    fun legacyThenUpgradedReadsTheSame() {
        val legacy = EnvelopeFormat.encryptLegacy(config, key)
        val upgraded = EnvelopeFormat.encrypt(EnvelopeFormat.decrypt(legacy, key), key)
        assertTrue(EnvelopeFormat.isEnvelope(upgraded))
        assertArrayEquals(config, EnvelopeFormat.decrypt(upgraded, key))
    }

    @Test
    fun legacyFileWhoseIvStartsWithTheMagicStillReads() {
        val iv = "ICH2".encodeToByteArray() + ByteArray(8).also(random::nextBytes)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key, GCMParameterSpec(128, iv)) }
        val legacy = iv + cipher.doFinal(config)
        assertTrue("looks like v2", EnvelopeFormat.isEnvelope(legacy))
        assertArrayEquals(config, EnvelopeFormat.decrypt(legacy, key))
    }

    @Test
    fun anotherKeyCannotRead() {
        val stored = EnvelopeFormat.encrypt(config, key)
        assertThrows(GeneralSecurityException::class.java) { EnvelopeFormat.decrypt(stored, aesKey()) }
        val legacy = EnvelopeFormat.encryptLegacy(config, key)
        assertThrows(GeneralSecurityException::class.java) { EnvelopeFormat.decrypt(legacy, aesKey()) }
    }

    @Test
    fun tamperedPayloadIsRejected() {
        val stored = EnvelopeFormat.encrypt(config, key)
        stored[stored.size - 1] = (stored[stored.size - 1].toInt() xor 1).toByte()
        assertThrows(GeneralSecurityException::class.java) { EnvelopeFormat.decrypt(stored, key) }
    }

    @Test
    fun securityKeySealWrapsTheEnvelope() {
        val dek = ByteArray(32).also(random::nextBytes)
        val sealed = SealedFile.seal(EnvelopeFormat.encrypt(config, key), dek)
        assertTrue(SealedFile.isSealed(sealed))
        assertArrayEquals(config, EnvelopeFormat.decrypt(SealedFile.open(sealed, dek), key))
    }

    @Test
    fun smallValuesRoundTrip() {
        for (value in listOf(ByteArray(0), "{}".encodeToByteArray())) {
            assertArrayEquals(value, EnvelopeFormat.decrypt(EnvelopeFormat.encrypt(value, key), key))
        }
    }
}
