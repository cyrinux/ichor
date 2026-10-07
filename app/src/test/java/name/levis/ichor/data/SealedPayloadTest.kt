package name.levis.ichor.data

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.security.GeneralSecurityException
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/** A software key stands in for the Keystore one: the layout does not depend on where it lives. */
class SealedPayloadTest {

    private fun key(): SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()

    private val wrapping = key()
    private val config = "context: lab\ncontexts:\n".repeat(2000).encodeToByteArray()

    /** What SecureStore wrote before: [IV][ciphertext+tag] under the Keystore key itself. */
    private fun legacy(key: SecretKey, plaintext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key) }
        return cipher.iv + cipher.doFinal(plaintext)
    }

    @Test
    fun opensWhatItSeals() {
        assertArrayEquals(config, SealedPayload.open(wrapping, SealedPayload.seal(wrapping, config)))
    }

    @Test
    fun eachSealUsesAFreshDataKey() {
        assertNotEquals(SealedPayload.seal(wrapping, config).toList(), SealedPayload.seal(wrapping, config).toList())
    }

    @Test
    fun opensTheLegacyLayout() {
        assertArrayEquals(config, SealedPayload.open(wrapping, legacy(wrapping, config)))
    }

    @Test
    fun opensAnEmptyValue() {
        assertArrayEquals(ByteArray(0), SealedPayload.open(wrapping, SealedPayload.seal(wrapping, ByteArray(0))))
    }

    @Test
    fun anotherKeyCannotOpenIt() {
        val sealed = SealedPayload.seal(wrapping, config)

        assertThrows(GeneralSecurityException::class.java) { SealedPayload.open(key(), sealed) }
    }

    @Test
    fun aChangedByteIsRefused() {
        val sealed = SealedPayload.seal(wrapping, config)
        listOf(5, 40, sealed.size - 1).forEach { at ->
            val tampered = sealed.copyOf().also { it[at] = (it[at] + 1).toByte() }
            assertThrows(GeneralSecurityException::class.java) { SealedPayload.open(wrapping, tampered) }
        }
    }

    @Test
    fun aTruncatedPayloadIsRefused() {
        assertThrows(IllegalArgumentException::class.java) { SealedPayload.open(wrapping, ByteArray(8)) }
    }
}
