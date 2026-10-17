package name.levis.ichor.data

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.File
import java.io.FileOutputStream
import java.nio.channels.FileChannel
import java.nio.file.StandardOpenOption
import java.security.KeyStore
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/**
 * The Keystore entries a [SecureStore] uses, by alias. An interface so tests can use software
 * keys and make any call fail; [AndroidKeySlots] is the real one.
 */
internal interface KeySlots {
    /** The key under [alias], or null when there is none. */
    fun get(alias: String): SecretKey?

    /** A new AES-256-GCM key under [alias] (replacing one there), in StrongBox when [strongBox]. */
    fun generate(alias: String, strongBox: Boolean): SecretKey

    /** Drops the key under [alias]; no key there is fine. */
    fun delete(alias: String)
}

internal object AndroidKeySlots : KeySlots {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"

    private fun keyStore(): KeyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    override fun get(alias: String): SecretKey? = keyStore().getKey(alias, null) as? SecretKey

    override fun generate(alias: String, strongBox: Boolean): SecretKey {
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
        if (strongBox && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) spec.setIsStrongBoxBacked(true)
        generator.init(spec.build())
        return generator.generateKey()
    }

    override fun delete(alias: String) = keyStore().deleteEntry(alias)
}

/**
 * The steps of [SecureStore.rekeyToStrongBox], in order. Tests make one fail, or stop the run
 * there as a kill would, through the store's step hook.
 */
internal enum class RekeyStep {
    READ,
    CLEAR_LEFTOVER_KEY,
    GENERATE,
    WRITE_NEXT,
    VERIFY_NEXT,
    RENAME,
    SYNC_DIR,
    DELETE_OLD_KEY,
    RECORD_ALIAS,
    CLEAN_UP,
}

/** Writes [bytes] to [file] and waits until they are on disk. */
internal fun writeSynced(file: File, bytes: ByteArray) {
    FileOutputStream(file).use { out ->
        out.write(bytes)
        out.flush()
        out.fd.sync()
    }
}

/**
 * Flushes [dir]'s entries (a rename in it) to disk; false when the platform refuses, in which
 * case nothing that only a durable rename makes safe may follow.
 */
internal fun syncDir(dir: File): Boolean = runCatching {
    FileChannel.open(dir.toPath(), StandardOpenOption.READ).use { it.force(true) }
}.isSuccess

/** [bytes] to [file] all at once: a temporary file, synced, renamed over it. */
internal fun replaceSynced(file: File, bytes: ByteArray) {
    val tmp = File(file.parentFile, "${file.name}.tmp")
    writeSynced(tmp, bytes)
    if (!tmp.renameTo(file)) {
        tmp.delete()
        throw IllegalStateException("Could not write ${file.name}")
    }
}
