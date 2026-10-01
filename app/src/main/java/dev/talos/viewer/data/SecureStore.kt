package dev.talos.viewer.data

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Stores small secrets in a file encrypted with AES-256-GCM. The key lives in the
 * Android Keystore (hardware-backed when available) and never leaves it.
 * File layout: [12-byte IV][ciphertext+tag].
 */
class SecureStore(private val file: File, private val keyAlias: String = "talosconfig") {

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
    }

    private fun key(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keyStore.getKey(keyAlias, null) as? SecretKey)?.let { return it }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                keyAlias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_SIZE = 12
        const val TAG_BITS = 128
    }
}
