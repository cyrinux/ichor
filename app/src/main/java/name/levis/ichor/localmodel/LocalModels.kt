package name.levis.ichor.localmodel

import android.app.ActivityManager
import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.TalosJson
import name.levis.ichorgo.Ichorgo
import java.io.File
import java.io.IOException

/** A model the app can download (Go LocalModelCatalog): pinned by size and SHA-256. */
@Serializable
data class LocalModelInfo(
    val id: String,
    val name: String,
    val recommended: Boolean = false,
    val url: String = "",
    val bytes: Long = 0,
    val sha256: String = "",
    val license: String = "",
    val licenseUrl: String = "",
    val minMemoryMiB: Long = 0,
    val contextTokens: Int = 0,
)

/** An id the Go core passes for a model file the user imported. */
const val IMPORTED_PREFIX = "file:"

/** A file is a model when it starts with the GGUF magic. */
private val GGUF_MAGIC = "GGUF".toByteArray()

/**
 * The model files on the phone, in the app's no-backup storage: downloads as models/<id>.gguf
 * (models/<id>.gguf.part while they arrive), imports as models/imported/<name>.
 */
class LocalModelStore(private val context: Context) {
    val dir: File = File(context.noBackupFilesDir, "models")
    private val importedDir = File(dir, "imported")

    /** The downloadable models; empty when this version pins none. */
    val catalog: List<LocalModelInfo> by lazy {
        runCatching { TalosJson.decodeFromString(ListSerializer(LocalModelInfo.serializer()), Ichorgo.localModelCatalog()) }
            .getOrDefault(emptyList())
    }

    private val _changes = MutableStateFlow(0)

    /** Bumped when a file appears or goes, so screens read the files again. */
    val changes: StateFlow<Int> = _changes.asStateFlow()

    fun changed() {
        _changes.value++
    }

    fun info(id: String): LocalModelInfo? = catalog.firstOrNull { it.id == id }

    /** Where the model [id] lives once complete (null: an id this version does not know). */
    fun target(id: String): File? = when {
        id.startsWith(IMPORTED_PREFIX) -> safeName(id.removePrefix(IMPORTED_PREFIX))?.let { File(importedDir, it) }
        info(id) != null -> File(dir, "$id.gguf")
        else -> null
    }

    fun partial(id: String) = File(dir, "$id.gguf.part")

    /** The model's file when it is on the phone, complete. */
    fun file(id: String): File? = target(id)?.takeIf { it.isFile && it.length() > 0 }

    fun isReady(id: String) = file(id) != null

    fun imported(): List<String> =
        importedDir.listFiles().orEmpty().filter { it.isFile }.map { IMPORTED_PREFIX + it.name }.sorted()

    fun delete(id: String) {
        target(id)?.delete()
        if (!id.startsWith(IMPORTED_PREFIX)) {
            partial(id).delete()
            etag(id).delete()
        }
        changed()
    }

    fun etag(id: String) = File(dir, "$id.gguf.etag")

    /** The phone's memory in MiB, as the catalog's minimums count it. */
    fun memoryMiB(): Long {
        val info = ActivityManager.MemoryInfo()
        context.getSystemService(ActivityManager::class.java)?.getMemoryInfo(info)
        return info.totalMem / (1024 * 1024)
    }

    fun fits(model: LocalModelInfo) = memoryMiB() >= model.minMemoryMiB

    /**
     * Copies a model file the user picked into the app's storage; returns its id. Fails on a
     * file that is not a GGUF model, or when there is not enough room.
     */
    suspend fun importFile(uri: Uri): String = withContext(Dispatchers.IO) {
        val resolver = context.contentResolver
        val (display, size) = resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)
            ?.use { c -> if (c.moveToFirst()) c.getString(0).orEmpty() to c.getLong(1) else "" to 0L } ?: ("" to 0L)
        val name = safeName(display.ifEmpty { "model.gguf" }) ?: throw IOException("this file name cannot be used")
        importedDir.mkdirs()
        if (size > 0 && importedDir.usableSpace < size + size / 10) throw IOException(NO_ROOM)
        val target = File(importedDir, name)
        val tmp = File(importedDir, ".$name.part")
        try {
            resolver.openInputStream(uri)?.use { input ->
                val head = ByteArray(GGUF_MAGIC.size)
                if (input.readNBytesCompat(head) != head.size || !head.contentEquals(GGUF_MAGIC)) throw IOException(NOT_GGUF)
                tmp.outputStream().use { out ->
                    out.write(head)
                    input.copyTo(out, 256 * 1024)
                }
            } ?: throw IOException("the file could not be opened")
            if (!tmp.renameTo(target)) throw IOException("the file could not be stored")
        } finally {
            tmp.delete()
        }
        changed()
        IMPORTED_PREFIX + name
    }

    companion object {
        const val NOT_GGUF = "not a GGUF model file"
        const val NO_ROOM = "not enough free space on the phone"

        /** A file name without a path, or null when nothing usable is left. */
        fun safeName(name: String): String? =
            name.substringAfterLast('/').replace(Regex("[^A-Za-z0-9._ -]"), "_").trim().trimStart('.').takeIf { it.isNotEmpty() }
    }
}

private fun java.io.InputStream.readNBytesCompat(buf: ByteArray): Int {
    var done = 0
    while (done < buf.size) {
        val n = read(buf, done, buf.size - done)
        if (n < 0) break
        done += n
    }
    return done
}
