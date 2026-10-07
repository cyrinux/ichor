package name.levis.ichor.data

import androidx.annotation.StringRes
import name.levis.ichor.R
import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
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
 * Stores small secrets in a file encrypted with AES-256-GCM. The key is generated inside
 * the Android Keystore and never leaves it: in the StrongBox secure element when the device
 * has one (Android's counterpart of the Secure Enclave), otherwise in the TEE. It seals a
 * fresh data key per write, which seals the content (layout in [SealedPayload]).
 */
class SecureStore(
    private val file: File,
    private val keyAlias: String = "talosconfig",
    private val strongBoxAvailable: Boolean = false,
) {

    fun write(plaintext: ByteArray) {
        val payload = SealedPayload.seal(key(), plaintext)
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
        // Never a new key here: it could not decrypt the file, and would replace the one that can.
        val key = checkNotNull(existingKey()) { "The key of the stored config is missing" }
        return SealedPayload.open(key, payload)
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
    }
}

fun hasStrongBox(pm: PackageManager): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && pm.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
