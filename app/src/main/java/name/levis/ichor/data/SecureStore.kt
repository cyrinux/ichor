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
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Where the key protecting the config lives, strongest first. */
enum class KeyProtection(@StringRes val label: Int) {
    STRONGBOX(R.string.settings_key_strongbox),
    TEE(R.string.settings_key_tee),
    SOFTWARE(R.string.settings_key_software),
}

/**
 * Stores small secrets in a file encrypted with AES-256-GCM. The key is generated inside
 * the Android Keystore and never leaves it: in the StrongBox secure element when the device
 * has one (Android's counterpart of the Secure Enclave), otherwise in the TEE.
 * File layout: [12-byte IV][ciphertext+tag].
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
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val inner = cipher.iv + cipher.doFinal(plaintext)
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

    fun read(): ByteArray? {
        if (!file.exists()) return null
        val payload = unseal(file.readBytes())
        require(payload.size > IV_SIZE) { "Stored config is corrupted" }
        // Never a new key here: it could not decrypt the file, and would replace the one that can.
        val key = checkNotNull(existingKey()) { "The key of the stored config is missing" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            key,
            GCMParameterSpec(TAG_BITS, payload, 0, IV_SIZE),
        )
        return cipher.doFinal(payload, IV_SIZE, payload.size - IV_SIZE)
    }

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
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_SIZE = 12
        const val TAG_BITS = 128
    }
}

fun hasStrongBox(pm: PackageManager): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && pm.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
