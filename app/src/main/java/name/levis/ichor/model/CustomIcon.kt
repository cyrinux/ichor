package name.levis.ichor.model

import java.security.MessageDigest
import java.util.Base64

/**
 * An icon a resource names for itself in its `ichor.levis.name/icon` annotation (see
 * go/ichorgo/customicon.go): inline bytes, never fetched, or an https URL, fetched only when the
 * user allowed icon downloads. [key] names it in caches.
 */
sealed interface CustomIcon {
    val key: String

    class Inline(val bytes: ByteArray, override val key: String) : CustomIcon
    data class Url(val url: String, override val key: String) : CustomIcon
}

private val DATA_URI = Regex("^data:image/(png|jpeg|webp|gif);base64,")

/** Larger than this decoded is not an icon (the Go core caps it the same). */
const val CUSTOM_ICON_MAX_BYTES = 64 * 1024

private const val CUSTOM_ICON_MAX_URL = 2048

/**
 * [iconUrl] as a [CustomIcon]; null when empty or not one of the two forms. It comes from the
 * cluster through the Go core: checked again here before it is decoded or requested.
 */
fun customIcon(iconUrl: String): CustomIcon? {
    if (iconUrl.isEmpty()) return null
    DATA_URI.find(iconUrl)?.let { header ->
        val bytes = runCatching { Base64.getDecoder().decode(iconUrl.substring(header.range.last + 1)) }.getOrNull()
        return bytes?.takeIf { it.isNotEmpty() && it.size <= CUSTOM_ICON_MAX_BYTES }
            ?.let { CustomIcon.Inline(it, "inline/" + sha256(iconUrl)) }
    }
    if (iconUrl.length > CUSTOM_ICON_MAX_URL || !iconUrl.startsWith("https://")) return null
    val uri = runCatching { java.net.URI(iconUrl) }.getOrNull() ?: return null
    if (uri.host.isNullOrEmpty() || uri.rawUserInfo != null) return null
    return CustomIcon.Url(iconUrl, "url/" + sha256(iconUrl))
}

private fun sha256(s: String): String =
    MessageDigest.getInstance("SHA-256").digest(s.encodeToByteArray()).joinToString("") { "%02x".format(it) }
