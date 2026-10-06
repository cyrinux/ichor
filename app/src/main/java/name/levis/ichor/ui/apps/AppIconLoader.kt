package name.levis.ichor.ui.apps

import android.content.Context
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import name.levis.ichor.model.CustomIcon
import name.levis.ichor.model.MAX_ICON_SIDE
import name.levis.ichor.model.bundledIconAsset
import name.levis.ichor.model.iconSampleSize
import name.levis.ichor.model.remoteIconUrl
import name.levis.ichor.util.httpGetBounded
import name.levis.ichor.util.readBounded
import java.io.File
import java.net.URL
import java.util.concurrent.ConcurrentHashMap

/**
 * App icons: the ones bundled in assets/appicons, and, only when the user allowed it, icons
 * downloaded from jsDelivr (Dashboard Icons) by their public slug or from the URL a resource
 * names in its annotation, kept in the cache dir; inline (data: URI) icons are only decoded.
 * Decoded icons stay in memory so the grid does not decode them again while scrolling.
 */
class AppIconLoader(private val context: Context) {
    private val memory = LruCache<String, ImageBitmap>(MEMORY_ENTRIES)

    /** File names in assets/appicons, read once (to know which icons have a dark variant). */
    private val bundled: Set<String> by lazy {
        runCatching { context.assets.list("appicons")?.toSet() }.getOrNull().orEmpty()
    }

    private val remoteDir by lazy { File(context.cacheDir, REMOTE_DIR) }

    /** Slugs that failed this session (404, offline, not an image): not asked again until restart. */
    private val failed = ConcurrentHashMap.newKeySet<String>()
    private val locks = ConcurrentHashMap<String, Mutex>()

    /** An icon already decoded, to draw at once without a loading frame. */
    fun peekBundled(icon: String, dark: Boolean): ImageBitmap? =
        bundledIconAsset(icon, dark, bundled)?.let(memory::get)
    fun peekRemote(slug: String): ImageBitmap? = memory.get(remoteKey(slug))
    fun peekCustom(icon: CustomIcon): ImageBitmap? = memory.get(icon.key)

    /** The bundled icon, its `-night` variant in a dark theme when there is one; null when missing. */
    suspend fun bundled(icon: String, dark: Boolean): ImageBitmap? {
        val asset = bundledIconAsset(icon, dark, bundled) ?: return null
        memory.get(asset)?.let { return it }
        return withContext(Dispatchers.IO) {
            runCatching { context.assets.open(asset).use { readBounded(it, MAX_BYTES) } }.getOrNull()
                ?.let(::decodeBounded)
                ?.also { memory.put(asset, it) }
        }
    }

    /**
     * The Dashboard Icons icon [slug], from the disk cache or downloaded. Callers check the
     * user allowed downloads; an invalid slug is never requested. Null when unavailable.
     */
    suspend fun remote(slug: String): ImageBitmap? {
        val url = remoteIconUrl(slug) ?: return null
        val key = remoteKey(slug)
        memory.get(key)?.let { return it }
        if (slug in failed) return null
        // One download per slug even when several tiles ask for it at once.
        return locks.getOrPut(slug) { Mutex() }.withLock {
            memory.get(key) ?: withContext(DOWNLOADS) { cachedOrDownload(slug, url) }
                ?.also { memory.put(key, it) }
        }
    }

    /**
     * A resource's own icon: decoded from its bytes, or (callers check the user allowed
     * downloads) from the disk cache or its URL, cached under a hash of the URL. Null when unavailable.
     */
    suspend fun custom(icon: CustomIcon): ImageBitmap? {
        memory.get(icon.key)?.let { return it }
        return when (icon) {
            is CustomIcon.Inline -> withContext(Dispatchers.Default) { decodeBounded(icon.bytes) }
                ?.also { memory.put(icon.key, it) }
            is CustomIcon.Url -> {
                val name = icon.key.replace('/', '-')
                if (name in failed) return null
                locks.getOrPut(name) { Mutex() }.withLock {
                    memory.get(icon.key) ?: withContext(DOWNLOADS) { cachedOrDownload(name, icon.url) }
                        ?.also { memory.put(icon.key, it) }
                }
            }
        }
    }

    private fun cachedOrDownload(slug: String, url: String): ImageBitmap? {
        if (slug in failed) return null
        val file = File(remoteDir, "$slug.webp")
        if (file.isFile) {
            runCatching { file.inputStream().use { readBounded(it, MAX_BYTES) } }.getOrNull()
                ?.let(::decodeBounded)
                ?.let { return it }
            // Corrupt, truncated or not a sane icon: forget it and download again.
            file.delete()
        }
        val bytes = runCatching { download(url) }.getOrNull()
        val bitmap = bytes?.let(::decodeBounded)
        if (bytes == null || bitmap == null) {
            failed += slug
            return null
        }
        // Cached only once it decoded within bounds.
        runCatching {
            remoteDir.mkdirs()
            val partial = File(remoteDir, "$slug.webp.part")
            partial.writeBytes(bytes)
            if (!partial.renameTo(file)) partial.delete()
        }
        return bitmap
    }

    /**
     * Decodes an icon after reading only its declared size: one over [MAX_ICON_SIDE] is refused
     * (a few KB can declare 16k×16k pixels, a gigabyte to decode), the rest is downsampled.
     */
    private fun decodeBounded(bytes: ByteArray): ImageBitmap? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        val sample = iconSampleSize(bounds.outWidth, bounds.outHeight) ?: return null
        val options = BitmapFactory.Options().apply { inSampleSize = sample }
        return BitmapFactory.decodeByteArray(bytes, 0, bytes.size, options)?.asImageBitmap()
    }

    /** Only the URL: no cookies, no referrer, a fixed user agent instead of the device's. */
    // An icon is a few KB: anything past MAX_BYTES is not one.
    private fun download(url: String): ByteArray? =
        httpGetBounded(url, MAX_BYTES, TIMEOUT_MS, userAgent = "ichor", followRedirects = false, useCaches = false)

    private fun remoteKey(slug: String) = "remote/$slug"

    companion object {
        private const val REMOTE_DIR = "appicons-remote"
        private const val MEMORY_ENTRIES = 300
        private const val TIMEOUT_MS = 10_000
        private const val MAX_BYTES = 512 * 1024

        /** A few downloads at a time, so a grid of new icons does not open dozens of connections. */
        private val DOWNLOADS = Dispatchers.IO.limitedParallelism(4)
    }
}
