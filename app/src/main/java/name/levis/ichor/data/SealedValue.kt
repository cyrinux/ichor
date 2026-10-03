package name.levis.ichor.data

import java.io.File

/** One small text value kept encrypted on disk; [read] is null when there is none or it cannot be decrypted. */
interface SealedValue {
    fun read(): String?
    fun write(value: String)
    fun delete()
}

/** [SealedValue] in a [SecureStore] file: AES-GCM with a Keystore key that never leaves the device. */
class KeystoreValue(private val file: File, private val keyAlias: String) : SealedValue {
    // A key lost with a restored or reset Keystore makes the file unreadable: as good as none.
    override fun read(): String? = runCatching { SecureStore(file, keyAlias).read()?.decodeToString() }.getOrNull()
    override fun write(value: String) = SecureStore(file, keyAlias).write(value.encodeToByteArray())
    override fun delete() {
        file.delete()
    }
}
