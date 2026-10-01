package name.levis.talosmobile.data

import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Where the key protecting the config lives, strongest first. */
enum class KeyProtection(val label: String) {
    STRONGBOX("StrongBox security chip"),
    TEE("Trusted Execution Environment"),
    SOFTWARE("software keystore"),
}

/**
 * Stores small secrets in a file encrypted with AES-256-GCM. The key is generated inside
 * the Android Keystore and never leaves it: in the StrongBox secure element when the device
 * has one (Android's counterpart of the Secure Enclave), otherwise in the TEE.
 * File layout: [12-byte IV][ciphertext+tag].
 */
class SecureStore(
    private val file: File,
    private val keyAlias: String = "talosconfig",
    private val strongBoxAvailable: Boolean = false,
) {

    fun write(plaintext: ByteArray) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val payload = cipher.iv + cipher.doFinal(plaintext)
        val tmp = File(file.parentFile, "${file.name}.tmp")
        tmp.writeBytes(payload)
        if (!tmp.renameTo(file)) {
            tmp.delete()
            throw IllegalStateException("Could not persist encrypted config")
        }
    }

    fun read(): ByteArray? {
        if (!file.exists()) return null
        val payload = file.readBytes()
        require(payload.size > IV_SIZE) { "Stored config is corrupted" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            key(),
            GCMParameterSpec(TAG_BITS, payload, 0, IV_SIZE),
        )
        return cipher.doFinal(payload, IV_SIZE, payload.size - IV_SIZE)
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
