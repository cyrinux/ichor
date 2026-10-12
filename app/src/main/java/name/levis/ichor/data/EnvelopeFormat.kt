package name.levis.ichor.data

import java.security.GeneralSecurityException
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * The bytes [SecureStore] keeps under its Keystore key.
 *
 * v2 (envelope): `[magic "ICH2"][wrapped DEK: IV(12) + AES-GCM(32-byte DEK) + tag(16)][IV(12)][AES-GCM(payload) + tag]`.
 * The Keystore key, in StrongBox when the phone has it, only wraps a random data key; the
 * payload is encrypted with that data key in software. A secure element is built for keys, not
 * bulk data: decrypting a config of 20+ contexts directly with a StrongBox key took 15–18 s on
 * a phone, the wrap of 32 bytes takes one call.
 *
 * v1 (legacy, still read): `[IV(12)][AES-GCM(payload) + tag]` directly with the Keystore key.
 *
 * Pure JVM: the wrapping key is any AES key, so tests use a software one.
 */
internal object EnvelopeFormat {
    /** A v1 file starts with a random IV that could begin the same way (1 in 4 billion): see [decrypt]. */
    private val MAGIC = "ICH2".encodeToByteArray()
    private const val IV_SIZE = 12
    private const val TAG_BITS = 128
    private const val TAG_SIZE = TAG_BITS / 8
    private const val DEK_SIZE = 32
    private const val WRAPPED_SIZE = IV_SIZE + DEK_SIZE + TAG_SIZE
    private const val HEADER_SIZE = 4 + WRAPPED_SIZE + IV_SIZE
    private const val TRANSFORMATION = "AES/GCM/NoPadding"

    /** Whether [payload] starts like a v2 file (it may still be a v1 one, see [decrypt]). */
    fun isEnvelope(payload: ByteArray): Boolean =
        payload.size >= HEADER_SIZE + TAG_SIZE && payload.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)

    /** [plaintext] as v2: a new data key per call, wrapped by [wrapKey]. */
    fun encrypt(plaintext: ByteArray, wrapKey: SecretKey, random: SecureRandom = SecureRandom()): ByteArray {
        val dek = ByteArray(DEK_SIZE).also(random::nextBytes)
        try {
            // The Keystore picks the IV of its own keys: it is read back from the cipher.
            val wrap = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, wrapKey) }
            val wrapped = wrap.iv + wrap.doFinal(dek)
            val iv = ByteArray(IV_SIZE).also(random::nextBytes)
            val body = Cipher.getInstance(TRANSFORMATION).apply {
                init(Cipher.ENCRYPT_MODE, SecretKeySpec(dek, "AES"), GCMParameterSpec(TAG_BITS, iv))
            }.doFinal(plaintext)
            return MAGIC + wrapped + iv + body
        } finally {
            dek.fill(0)
        }
    }

    /**
     * The plaintext of a v2 or v1 [payload]. A v1 file whose IV happens to start with the magic
     * fails to open as v2 and is read as v1.
     */
    fun decrypt(payload: ByteArray, wrapKey: SecretKey): ByteArray {
        if (isEnvelope(payload)) {
            try {
                return decryptEnvelope(payload, wrapKey)
            } catch (_: GeneralSecurityException) {
                // Not a v2 file after all: a v1 IV that starts with the magic.
            }
        }
        return decryptLegacy(payload, wrapKey)
    }

    private fun decryptEnvelope(payload: ByteArray, wrapKey: SecretKey): ByteArray {
        val wrappedAt = MAGIC.size
        val unwrap = Cipher.getInstance(TRANSFORMATION).apply {
            init(Cipher.DECRYPT_MODE, wrapKey, GCMParameterSpec(TAG_BITS, payload, wrappedAt, IV_SIZE))
        }
        val dek = unwrap.doFinal(payload, wrappedAt + IV_SIZE, DEK_SIZE + TAG_SIZE)
        try {
            val ivAt = wrappedAt + WRAPPED_SIZE
            val body = Cipher.getInstance(TRANSFORMATION).apply {
                init(Cipher.DECRYPT_MODE, SecretKeySpec(dek, "AES"), GCMParameterSpec(TAG_BITS, payload, ivAt, IV_SIZE))
            }
            return body.doFinal(payload, HEADER_SIZE, payload.size - HEADER_SIZE)
        } finally {
            dek.fill(0)
        }
    }

    private fun decryptLegacy(payload: ByteArray, wrapKey: SecretKey): ByteArray {
        require(payload.size > IV_SIZE) { "Stored config is corrupted" }
        val cipher = Cipher.getInstance(TRANSFORMATION).apply {
            init(Cipher.DECRYPT_MODE, wrapKey, GCMParameterSpec(TAG_BITS, payload, 0, IV_SIZE))
        }
        return cipher.doFinal(payload, IV_SIZE, payload.size - IV_SIZE)
    }

    /** v1 bytes of [plaintext], as versions before the envelope wrote them (tests, nothing else). */
    fun encryptLegacy(plaintext: ByteArray, key: SecretKey): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, key) }
        return cipher.iv + cipher.doFinal(plaintext)
    }
}
