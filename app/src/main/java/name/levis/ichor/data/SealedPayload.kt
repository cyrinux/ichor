package name.levis.ichor.data

import java.security.GeneralSecurityException
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * What [SecureStore] writes. The Keystore key (in StrongBox, a secure element that is slow on
 * anything but a few bytes) seals only a fresh 32-byte data key; the content is sealed with
 * that data key in software, so a config of many clusters costs no more than one of a few.
 *
 * Layout: [MAGIC][12-byte IV][data key+tag] [12-byte IV][ciphertext+tag], all AES-256-GCM.
 * A file written before is [12-byte IV][ciphertext+tag] under the Keystore key itself: it
 * still opens, and the next write moves it to this layout.
 */
internal object SealedPayload {

    fun seal(wrapping: SecretKey, plaintext: ByteArray): ByteArray {
        val dataKey = ByteArray(DATA_KEY_SIZE).also(random::nextBytes)
        try {
            return MAGIC + encrypt(wrapping, dataKey) + encrypt(SecretKeySpec(dataKey, ALGORITHM), plaintext)
        } finally {
            dataKey.fill(0)
        }
    }

    fun open(wrapping: SecretKey, payload: ByteArray): ByteArray {
        require(payload.size > IV_SIZE) { "Stored config is corrupted" }
        if (!payload.startsWith(MAGIC) || payload.size < ENVELOPE_MIN) return decrypt(wrapping, payload, 0, payload.size)
        return try {
            openEnvelope(wrapping, payload)
        } catch (envelope: GeneralSecurityException) {
            // A file of the previous layout whose random IV happens to start with MAGIC.
            runCatching { decrypt(wrapping, payload, 0, payload.size) }.getOrElse { throw envelope }
        }
    }

    private fun openEnvelope(wrapping: SecretKey, payload: ByteArray): ByteArray {
        val dataKey = decrypt(wrapping, payload, MAGIC.size, WRAPPED_SIZE)
        try {
            val body = MAGIC.size + WRAPPED_SIZE
            return decrypt(SecretKeySpec(dataKey, ALGORITHM), payload, body, payload.size - body)
        } finally {
            dataKey.fill(0)
        }
    }

    /** [IV][ciphertext+tag]; the cipher picks the IV (the Keystore refuses one given to it). */
    private fun encrypt(key: SecretKey, plaintext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, key) }
        return cipher.iv + cipher.doFinal(plaintext)
    }

    private fun decrypt(key: SecretKey, payload: ByteArray, offset: Int, length: Int): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(TAG_BITS, payload, offset, IV_SIZE))
        return cipher.doFinal(payload, offset + IV_SIZE, length - IV_SIZE)
    }

    private fun ByteArray.startsWith(prefix: ByteArray): Boolean =
        size >= prefix.size && prefix.indices.all { this[it] == prefix[it] }

    private val random = SecureRandom()
    private val MAGIC = byteArrayOf(0x49, 0x43, 0x48, 0x32) // "ICH2"
    private const val ALGORITHM = "AES"
    private const val TRANSFORMATION = "AES/GCM/NoPadding"
    private const val DATA_KEY_SIZE = 32
    private const val IV_SIZE = 12
    private const val TAG_BITS = 128
    private const val WRAPPED_SIZE = IV_SIZE + DATA_KEY_SIZE + TAG_BITS / 8
    private val ENVELOPE_MIN = MAGIC.size + WRAPPED_SIZE + IV_SIZE + TAG_BITS / 8
}
