package name.levis.ichor.data

import androidx.annotation.StringRes
import name.levis.ichor.R
import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import name.levis.ichor.security.DekHolder
import name.levis.ichor.security.SealedFile
import name.levis.ichor.security.SecurityKeyRequiredException
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import java.io.File
import java.security.KeyStore
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/** Where the key protecting the config lives, strongest first. */
enum class KeyProtection(@StringRes val label: Int) {
    STRONGBOX(R.string.settings_key_strongbox),
    TEE(R.string.settings_key_tee),
    SOFTWARE(R.string.settings_key_software),
}

/**
 * Stores secrets in a file encrypted with AES-256-GCM. The key is generated inside the Android
 * Keystore and never leaves it: in the StrongBox secure element when the device has one
 * (Android's counterpart of the Secure Enclave), otherwise in the TEE. It wraps a random data
 * key that encrypts the content ([EnvelopeFormat] v2); files written before are read too
 * (v1, the content directly under the Keystore key) and rewritten by [upgrade].
 *
 * With a security key required ([outer], see SecurityKeys.kt) that payload is sealed once more
 * with the data key only a tap of the key unwraps ([SealedFile]): the file then reads only
 * after a key was tapped this run, and [read] throws [SecurityKeyRequiredException] before.
 */
class SecureStore(
    private val file: File,
    private val keyAlias: String = "talosconfig",
    private val strongBoxAvailable: Boolean = false,
    private val outer: DekHolder? = null,
) {

    fun write(plaintext: ByteArray) {
        val inner = EnvelopeFormat.encrypt(plaintext, key())
        val holder = outer
        val payload = if (holder != null && holder.sealing()) {
            // Never written unsealed while the key is required: a missing DEK is a bug upstream.
            SealedFile.seal(inner, holder.dek() ?: throw SecurityKeyRequiredException())
        } else {
            inner
        }
        val tmp = File(file.parentFile, "${file.name}.tmp")
        tmp.writeBytes(payload)
        if (!tmp.renameTo(file)) {
            tmp.delete()
            throw IllegalStateException("Could not persist encrypted config")
        }
    }

    /**
     * Makes sure the Keystore key answers before a write that must not fail halfway (an import,
     * a restored backup). A secure element can drop its connection after days of uptime and
     * refuse every call with UNKNOWN_ERROR until its service reconnects: the key is tried with
     * a real wrap and unwrap, again after each of [delays] (ms). A new key goes to StrongBox
     * when the chip answers in time, else to the TEE; an existing StrongBox key cannot move,
     * so [KeystoreUnavailableException] says the chip does not answer.
     */
    suspend fun prepare(delays: List<Long> = CHIP_RETRY_DELAYS) {
        if (existingKey() != null) {
            try {
                retrying(delays) { probe(storedKey()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                throw KeystoreUnavailableException(e)
            }
            return
        }
        val strongBox = strongBoxAvailable && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
        strongBoxOrTee(
            strongBox = if (strongBox) ({ generate(strongBox = true).also(::probe) }) else null,
            tee = { generate(strongBox = false).also(::probe) },
            delays = delays,
            // A StrongBox key left half made would be picked up by the next write.
            discard = ::deleteKey,
        )
    }

    private fun probe(key: SecretKey) {
        EnvelopeFormat.decrypt(EnvelopeFormat.encrypt(PROBE, key), key)
    }

    fun read(): ByteArray? {
        if (!file.exists()) return null
        return EnvelopeFormat.decrypt(unseal(file.readBytes()), storedKey())
    }

    /**
     * Rewrites a file stored before the envelope format (v1) as v2, sealed as the current mode
     * asks; true when it did. A v2 file, or none, is left alone. Throws like [read].
     */
    fun upgrade(): Boolean {
        if (!file.exists()) return false
        val payload = unseal(file.readBytes())
        if (EnvelopeFormat.isEnvelope(payload)) return false
        write(EnvelopeFormat.decrypt(payload, storedKey()))
        return true
    }

    /** Never a new key here: it could not decrypt the file, and would replace the one that can. */
    private fun storedKey(): SecretKey = checkNotNull(existingKey()) { "The key of the stored config is missing" }

    /**
     * The Keystore payload of a stored file: opened with the DEK when sealed. A sealed file with
     * no DEK this run is unreadable while the key is required; a payload that only starts like a
     * sealed one (a Keystore IV can) is taken as is when opening it fails.
     */
    private fun unseal(payload: ByteArray): ByteArray {
        if (!SealedFile.isSealed(payload)) return payload
        val dek = outer?.dek()
        if (dek != null) {
            try {
                return SealedFile.open(payload, dek)
            } catch (_: javax.crypto.AEADBadTagException) {
                // Not sealed with this DEK: a Keystore IV that happens to start with the magic.
            }
        } else if (outer?.sealing() == true) {
            throw SecurityKeyRequiredException()
        }
        return payload
    }

    /** Whether the stored file is sealed with the security-key DEK (false when none is stored). */
    fun isSealed(): Boolean = file.exists() && SealedFile.isSealed(file.readBytes())

    /**
     * Writes the stored value again as the current mode asks: sealed once the key is required,
     * Keystore-only once it no longer is (the DEK must still be held then). Nothing stored: no-op.
     */
    fun reseal() {
        read()?.let { write(it) }
    }

    fun clear() {
        file.delete()
        // Drop the key too, so a later import gets a fresh one at the best available level.
        deleteKey()
    }

    private fun deleteKey() {
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }.deleteEntry(keyAlias)
    }

    /** Protection level of the existing key, or null before the first write. */
    fun protection(): KeyProtection? {
        val key = existingKey() ?: return null
        val info = javax.crypto.SecretKeyFactory.getInstance(key.algorithm, ANDROID_KEYSTORE)
            .getKeySpec(key, KeyInfo::class.java) as KeyInfo
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            when (info.securityLevel) {
                KeyProperties.SECURITY_LEVEL_STRONGBOX -> KeyProtection.STRONGBOX
                KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> KeyProtection.TEE
                else -> KeyProtection.SOFTWARE
            }
        } else {
            @Suppress("DEPRECATION")
            if (info.isInsideSecureHardware) KeyProtection.TEE else KeyProtection.SOFTWARE
        }
    }

    private fun existingKey(): SecretKey? {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        return keyStore.getKey(keyAlias, null) as? SecretKey
    }

    private fun key(): SecretKey {
        existingKey()?.let { return it }
        if (strongBoxAvailable && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            try {
                return generate(strongBox = true)
            } catch (_: StrongBoxUnavailableException) {
                // Advertised but unusable (busy or unsupported parameters): use the TEE.
            } catch (_: java.security.ProviderException) {
                // The chip refused (UNKNOWN_ERROR while its connection is down): use the TEE.
                deleteKey()
            }
        }
        return generate(strongBox = false)
    }

    private fun generate(strongBox: Boolean): SecretKey {
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        val spec = KeyGenParameterSpec.Builder(
            keyAlias,
            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
        )
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
        if (strongBox && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) spec.setIsStrongBoxBacked(true)
        generator.init(spec.build())
        return generator.generateKey()
    }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        val PROBE = "ichor".encodeToByteArray()
    }
}

/** How long to wait before each new try of the secure element, in ms: its service reconnects in seconds. */
private val CHIP_RETRY_DELAYS = listOf(250L, 1000L, 3000L)

/**
 * A new key from [strongBox], tried again after each of [delays] (ms), else from [tee]; null
 * [strongBox] (no chip) goes to [tee] at once. [discard] runs before the fallback, to drop what
 * a failed try may have left. A failure of [tee] is thrown with the StrongBox one suppressed.
 */
internal suspend fun <K> strongBoxOrTee(
    strongBox: (() -> K)?,
    tee: () -> K,
    delays: List<Long>,
    pause: suspend (Long) -> Unit = { delay(it) },
    discard: () -> Unit = {},
): K {
    if (strongBox == null) return tee()
    val chipFailure = try {
        return retrying(delays, pause, retryOn = { it !is StrongBoxUnavailableException }) { strongBox() }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e
    }
    runCatching(discard)
    return try {
        tee()
    } catch (e: Exception) {
        e.addSuppressed(chipFailure)
        throw e
    }
}

/** The Keystore key of the stored config does not answer, even after a few tries. */
class KeystoreUnavailableException(cause: Exception) : LocalizedException(UiText.Res(R.string.keystore_unavailable)) {
    init {
        initCause(cause)
    }
}

fun hasStrongBox(pm: PackageManager): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && pm.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
