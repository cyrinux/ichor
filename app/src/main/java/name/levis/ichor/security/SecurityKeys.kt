package name.levis.ichor.security

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.security.KeyFactory
import java.security.MessageDigest
import java.security.PublicKey
import java.security.SecureRandom
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/*
 * Security keys (a YubiKey or any FIDO2 key, tapped over NFC or plugged in) as a way to open
 * the app lock, with an optional hardened mode. Nothing here touches Android or the key itself:
 * the records, the key wrapping and the signature check are plain JVM code, tested as such.
 * SecurityKeyClient talks to the key; SecureStore applies [SealedFile] to the credential files.
 *
 * How it works: each key holds a FIDO2 credential made for the relying party [SECURITY_KEY_RP_ID]
 * (not a website; the id only names the app). Opening the lock asks the key for an assertion over
 * a fresh random challenge, with user presence only (a touch, no FIDO PIN), and checks its
 * signature with the public key recorded at enrolment. In the "required" mode the same assertion
 * also carries the hmac-secret (WebAuthn prf) output for a fixed salt: a 32-byte secret only that
 * key can compute, stable for the credential. It wraps a random data-encryption key (DEK) that
 * seals the credential files on top of the Keystore: without a key tapped they cannot be read.
 */

/** The relying party id the credentials are made for (the app's domain, nothing is served there). */
const val SECURITY_KEY_RP_ID = "ichor.levis.name"

/** The most keys that can be enrolled: the one worn, and a spare. */
const val SECURITY_KEY_MAX = 2

/** What the enrolled keys are for. */
enum class SecurityKeyMode {
    /** An alternative to the fingerprint / device PIN: either one opens the lock. */
    UNLOCK,

    /**
     * The credential files are sealed with a key only a tap can unwrap: fingerprint / PIN alone
     * no longer opens the app, and the stored configs are unreadable without one of the keys.
     */
    REQUIRED,
}

/** One enrolled key. Public data only (the credential id and public key; no secret). */
@Serializable
data class EnrolledKey(
    /** The credential id the key gave at enrolment, base64url. */
    val credentialId: String,
    /** Its ES256 public key, X.509 SubjectPublicKeyInfo, base64. */
    val publicKey: String,
    /** How it is listed in Settings. */
    val label: String,
    /** Enrolment time, epoch milliseconds. */
    val enrolledAt: Long,
    /**
     * In [SecurityKeyMode.REQUIRED]: the DEK wrapped with this key's hmac-secret output
     * (see [DekWrap]), base64. Null in [SecurityKeyMode.UNLOCK] or before the key was tapped
     * for it.
     */
    val wrappedDek: String? = null,
    /**
     * Whether the hmac-secret output wrapping the DEK was made with user verification on the key
     * (a key set to always verify): the authenticator derives a different output then, so the
     * same state is needed to unwrap. Unlocking with the other state is refused with a clear
     * message instead of a silent failure.
     */
    val uv: Boolean = false,
) {
    val credentialIdBytes: ByteArray get() = Base64.getUrlDecoder().decode(credentialId)
    val publicKeyBytes: ByteArray get() = Base64.getDecoder().decode(publicKey)
}

/** The enrolled keys and their mode; the record the app lock keeps (no secret in it). */
@Serializable
data class SecurityKeyEnrolment(
    val keys: List<EnrolledKey> = emptyList(),
    /** The hmac-secret salt, 32 random bytes made at the first enrolment, base64. */
    val salt: String,
    val mode: SecurityKeyMode = SecurityKeyMode.UNLOCK,
) {
    val saltBytes: ByteArray get() = Base64.getDecoder().decode(salt)

    /** Whether a tap is needed to read the stored configs (and fingerprint / PIN alone is refused). */
    val required: Boolean get() = mode == SecurityKeyMode.REQUIRED && keys.isNotEmpty()

    fun key(credentialId: ByteArray): EnrolledKey? = keys.firstOrNull { it.credentialIdBytes.contentEquals(credentialId) }

    fun withKey(key: EnrolledKey): SecurityKeyEnrolment = copy(keys = keys.filter { it.credentialId != key.credentialId } + key)

    fun without(credentialId: String): SecurityKeyEnrolment = copy(keys = keys.filter { it.credentialId != credentialId })

    fun encode(): String = JSON.encodeToString(this)

    companion object {
        private val JSON = Json { ignoreUnknownKeys = true }

        fun decode(json: String?): SecurityKeyEnrolment? =
            json?.takeIf { it.isNotBlank() }?.let { runCatching { JSON.decodeFromString<SecurityKeyEnrolment>(it) }.getOrNull() }

        /** A new record, with no key yet, for the first enrolment. */
        fun create(random: SecureRandom = SecureRandom()): SecurityKeyEnrolment =
            SecurityKeyEnrolment(salt = Base64.getEncoder().encodeToString(ByteArray(32).also(random::nextBytes)))
    }
}

/** What a tap of an enrolled key gave: which key, and its hmac-secret output when asked for. */
class SecurityKeyAssertion(val key: EnrolledKey, val secret: ByteArray?, val uv: Boolean)

/** The DEK could not be unwrapped with what the key gave; [reason] says why, for the message shown. */
class SecurityKeyUnwrapException(val reason: Reason) : Exception(reason.name) {
    enum class Reason {
        /** The key returned no hmac-secret output (no support for the extension). */
        NO_SECRET,
        /** This key was enrolled without a wrapped DEK (before the requirement was turned on). */
        NOT_WRAPPED,
        /** The key verified the user this time but not at enrolment (or the reverse): other output. */
        UV_CHANGED,
        /** The output does not unwrap the DEK: not the secret the wrap was made with. */
        WRONG_KEY,
        /** The wrapped DEK is too short to be one. */
        DAMAGED,
    }
}

/** The DEK for the files, unwrapped from what [assertion]'s key gave; see [SecurityKeyUnwrapException]. */
fun SecurityKeyEnrolment.unwrapDek(assertion: SecurityKeyAssertion): ByteArray {
    val secret = assertion.secret ?: throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.NO_SECRET)
    val wrapped = assertion.key.wrappedDek ?: throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.NOT_WRAPPED)
    if (assertion.uv != assertion.key.uv) throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.UV_CHANGED)
    return DekWrap.unwrap(Base64.getDecoder().decode(wrapped), secret)
}

/** This key with [dek] wrapped by the secret [assertion] gave (its own), for [SecurityKeyMode.REQUIRED]. */
fun EnrolledKey.wrapping(dek: ByteArray, assertion: SecurityKeyAssertion): EnrolledKey {
    val secret = assertion.secret ?: throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.NO_SECRET)
    return copy(wrappedDek = Base64.getEncoder().encodeToString(DekWrap.wrap(dek, secret)), uv = assertion.uv)
}

/** The credential files are sealed with the DEK and no key was tapped yet this run. */
class SecurityKeyRequiredException : Exception("The stored config is sealed with a security key")

/**
 * Wraps the data-encryption key with a security key's hmac-secret output: HKDF-SHA256 of the
 * output (info [INFO]) gives an AES-256 key, which encrypts the DEK with AES-GCM.
 * Layout: [12-byte nonce][ciphertext+tag].
 */
object DekWrap {
    const val DEK_SIZE = 32
    private const val NONCE_SIZE = 12
    private const val TAG_BITS = 128
    private val INFO = "ichor security-key dek v1".toByteArray()

    fun newDek(random: SecureRandom = SecureRandom()): ByteArray = ByteArray(DEK_SIZE).also(random::nextBytes)

    fun wrap(dek: ByteArray, secret: ByteArray, random: SecureRandom = SecureRandom()): ByteArray {
        require(dek.size == DEK_SIZE) { "DEK must be $DEK_SIZE bytes" }
        val nonce = ByteArray(NONCE_SIZE).also(random::nextBytes)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(wrappingKey(secret), "AES"), GCMParameterSpec(TAG_BITS, nonce))
        return nonce + cipher.doFinal(dek)
    }

    /** Throws [SecurityKeyUnwrapException] when [secret] is not the one [wrapped] was made with. */
    fun unwrap(wrapped: ByteArray, secret: ByteArray): ByteArray {
        if (wrapped.size <= NONCE_SIZE) throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.DAMAGED)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(wrappingKey(secret), "AES"), GCMParameterSpec(TAG_BITS, wrapped, 0, NONCE_SIZE))
        return try {
            cipher.doFinal(wrapped, NONCE_SIZE, wrapped.size - NONCE_SIZE)
        } catch (e: javax.crypto.AEADBadTagException) {
            throw SecurityKeyUnwrapException(SecurityKeyUnwrapException.Reason.WRONG_KEY)
        }
    }

    /** HKDF-SHA256 (RFC 5869) with an empty salt: extract then one expand block (32 bytes). */
    internal fun wrappingKey(secret: ByteArray): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(ByteArray(32), "HmacSHA256"))
        val prk = mac.doFinal(secret)
        mac.init(SecretKeySpec(prk, "HmacSHA256"))
        mac.update(INFO)
        mac.update(1)
        return mac.doFinal()
    }
}

/**
 * The outer layer of a credential file in [SecurityKeyMode.REQUIRED]: the Keystore ciphertext
 * is encrypted again with the DEK (AES-256-GCM), so the file reads only with a key tapped.
 * Layout: [MAGIC][12-byte nonce][ciphertext+tag]. A file without the magic is Keystore-only.
 */
object SealedFile {
    /**
     * "IK" + version 2. A Keystore-only file starts with a random IV that could begin the same
     * way (1 in 16 million): SecureStore falls back to reading it as such when opening fails.
     */
    val MAGIC = byteArrayOf(0x49, 0x4B, 0x02)
    private const val NONCE_SIZE = 12
    private const val TAG_BITS = 128

    fun isSealed(payload: ByteArray): Boolean =
        payload.size > MAGIC.size + NONCE_SIZE && payload.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)

    fun seal(inner: ByteArray, dek: ByteArray, random: SecureRandom = SecureRandom()): ByteArray {
        val nonce = ByteArray(NONCE_SIZE).also(random::nextBytes)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(dek, "AES"), GCMParameterSpec(TAG_BITS, nonce))
        return MAGIC + nonce + cipher.doFinal(inner)
    }

    fun open(sealed: ByteArray, dek: ByteArray): ByteArray {
        require(isSealed(sealed)) { "Not a sealed file" }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(dek, "AES"), GCMParameterSpec(TAG_BITS, sealed, MAGIC.size, NONCE_SIZE))
        val start = MAGIC.size + NONCE_SIZE
        return cipher.doFinal(sealed, start, sealed.size - start)
    }
}

/** The DEK of this process, once a key was tapped (or the mode turned on); see [SecureStore]. */
interface DekHolder {
    /** The DEK, or null when none was unwrapped this run. */
    fun dek(): ByteArray?

    /** Whether the files must be sealed ([SecurityKeyMode.REQUIRED] with a key enrolled). */
    fun sealing(): Boolean
}

/**
 * Checks a WebAuthn assertion as a relying party would: the rpIdHash, the user-presence flag and
 * the ES256 signature over authenticatorData || clientDataHash with the enrolled public key.
 */
object AssertionVerifier {
    private const val FLAG_UP = 0x01
    private const val FLAG_UV = 0x04

    class Result(val valid: Boolean, val uv: Boolean, val reason: String? = null)

    fun verify(key: EnrolledKey, authenticatorData: ByteArray, clientDataHash: ByteArray, signature: ByteArray, rpId: String = SECURITY_KEY_RP_ID): Result {
        if (authenticatorData.size < 37) return Result(false, false, "authenticator data too short")
        val rpIdHash = MessageDigest.getInstance("SHA-256").digest(rpId.toByteArray())
        if (!authenticatorData.copyOfRange(0, 32).contentEquals(rpIdHash)) return Result(false, false, "rpIdHash mismatch")
        val flags = authenticatorData[32].toInt()
        if (flags and FLAG_UP == 0) return Result(false, false, "no user presence")
        val uv = flags and FLAG_UV != 0
        val publicKey = try {
            publicKey(key.publicKeyBytes)
        } catch (e: Exception) {
            return Result(false, uv, "bad stored public key")
        }
        val verifier = Signature.getInstance("SHA256withECDSA")
        verifier.initVerify(publicKey)
        verifier.update(authenticatorData)
        verifier.update(clientDataHash)
        val valid = try {
            verifier.verify(signature)
        } catch (e: java.security.SignatureException) {
            false
        }
        return Result(valid, uv, if (valid) null else "signature mismatch")
    }

    fun publicKey(spki: ByteArray): PublicKey = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(spki))
}

/**
 * A key's FIDO PIN as CTAP2 defines it: any Unicode text (letters too, not only digits), at
 * least 4 code points and at most 63 bytes of UTF-8.
 */
object FidoPin {
    private const val MIN_CODE_POINTS = 4
    private const val MAX_UTF8_BYTES = 63

    fun valid(pin: String): Boolean =
        pin.codePointCount(0, pin.length) >= MIN_CODE_POINTS && pin.toByteArray(Charsets.UTF_8).size <= MAX_UTF8_BYTES

    /** [input] without line breaks, which a pasted PIN often ends with and none is typed with. */
    fun clean(input: String): String = input.filterNot { it == '\n' || it == '\r' }
}
