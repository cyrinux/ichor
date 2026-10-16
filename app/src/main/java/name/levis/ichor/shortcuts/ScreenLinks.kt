package name.levis.ichor.shortcuts

import java.net.URI
import java.net.URLDecoder
import java.security.MessageDigest
import name.levis.ichor.model.ShareTarget

private const val SCREEN_PREFIX = "screen-"
private const val WEB_LINK = "https://cyrinux.github.io/ichor/open/#"
private const val APP_LINK = "ichor://open?"
private const val CLUSTER_PARAM = "c"

/**
 * The ichor://open?… form of the https share link [link] (same query, from its fragment), so a
 * home screen shortcut opens the app directly: Android 12+ sends the https form to the browser.
 * Null for anything that is not an https share link.
 */
fun appShareLink(link: String): String? =
    link.takeIf { it.startsWith(WEB_LINK) }?.removePrefix(WEB_LINK)?.takeIf { it.isNotEmpty() }?.let { APP_LINK + it }

/** The id of the home screen shortcut opening [link]: the same for the same screen of the same cluster. */
fun screenShortcutId(link: String): String {
    val digest = MessageDigest.getInstance("SHA-1").digest(link.toByteArray(Charsets.UTF_8))
    return SCREEN_PREFIX + digest.joinToString("") { "%02x".format(it) }
}

fun isScreenShortcut(id: String): Boolean = id.startsWith(SCREEN_PREFIX)

/** The cluster id an ichor://open link names (its `c` parameter); null when it names none. */
fun clusterOfAppLink(link: String): String? {
    val query = runCatching { URI(link) }.getOrNull()?.takeIf { it.scheme == "ichor" && it.host == "open" }?.rawQuery ?: return null
    return query.split('&')
        .map { it.substringBefore('=') to it.substringAfter('=', "") }
        .firstOrNull { it.first == CLUSTER_PARAM }
        ?.second
        ?.let { runCatching { URLDecoder.decode(it, Charsets.UTF_8.name()) }.getOrNull() }
        ?.takeIf { it.isNotBlank() }
}

/** A shortcut pinned on the home screen: its [id] and the link its intent opens, if any. */
data class PinnedShortcut(val id: String, val link: String?)

/**
 * Whether the pinned [shortcut] still opens something: a cluster shortcut among [known] or
 * [current], or a screen shortcut whose cluster is among [clusters] (the cluster ids on the phone).
 */
fun keepsPinned(shortcut: PinnedShortcut, current: Set<String>, known: Set<String>, clusters: Set<String>): Boolean =
    if (isScreenShortcut(shortcut.id)) {
        shortcut.link?.let(::clusterOfAppLink)?.let { it in clusters } == true
    } else {
        shortcut.id in known || shortcut.id in current
    }

/**
 * The letter drawn on a screen shortcut's icon, by the kind of screen ([ShareTarget.target]);
 * the screen's own [label] gives it for the others.
 */
fun screenGlyph(target: String, label: String): String = when (target) {
    ShareTarget.NODE, ShareTarget.STORAGE, ShareTarget.HARDWARE -> "N"
    ShareTarget.WORKLOAD, ShareTarget.WORKLOADS -> "W"
    ShareTarget.POD -> "P"
    ShareTarget.CRONJOB -> "C"
    ShareTarget.ARGO_APP, ShareTarget.ARGO_CD -> "A"
    ShareTarget.FLUX_APP, ShareTarget.FLUX -> "F"
    ShareTarget.ETCD -> "E"
    ShareTarget.HEALTH -> "H"
    ShareTarget.ALERTS -> "!"
    else -> shortcutInitial(label)
}
