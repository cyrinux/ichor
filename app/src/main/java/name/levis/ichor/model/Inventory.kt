package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/inventory.go.

/** The applications running in the cluster, from every node's Kubernetes containers. */
@Serializable
data class Inventory(
    /** Unix millis. */
    val at: Long = 0,
    /** Nodes of the context. */
    val nodes: Int = 0,
    /** Nodes whose containers are counted. */
    val answered: Int = 0,
    /** Sorted by name. */
    val apps: List<InventoryApp> = emptyList(),
) {
    /** Some nodes did not answer: the counts are a lower bound. */
    val partial: Boolean get() = answered < nodes
}

@Serializable
data class InventoryApp(
    val id: String,
    /** A proper noun: never translated. */
    val name: String,
    /** One of [APP_CATEGORIES]. */
    val category: String = OTHER_CATEGORY,
    /** Bundled icon: assets/appicons/<icon>.webp; "" when none. */
    val icon: String = "",
    /** Dashboard Icons slug to download, only when the user allowed it; "" when none. */
    val remoteIcon: String = "",
    /** Identified by the catalog; false goes to "Not recognised". */
    val known: Boolean = false,
    /** Kubernetes and Talos plumbing. */
    val system: Boolean = false,
    /** "" when unknown, e.g. pinned by digest only. */
    val version: String = "",
    /** The same repository runs with several tags. */
    val drift: Boolean = false,
    /** `:latest` or untagged. */
    val unpinned: Boolean = false,
    val namespaces: List<String> = emptyList(),
    /** Node addresses. */
    val nodes: List<String> = emptyList(),
    val containers: Int = 0,
    val running: Int = 0,
    /** Bytes. */
    val memory: Long = 0,
    val images: List<InventoryImage> = emptyList(),
    val pods: List<InventoryPod> = emptyList(),
)

@Serializable
data class InventoryImage(
    /** "" for an image known only by [digest]. */
    val repo: String = "",
    val tag: String = "",
    val digest: String = "",
    val containers: Int = 0,
)

@Serializable
data class InventoryPod(
    val namespace: String = "",
    val pod: String = "",
    /** Node address. */
    val node: String = "",
    val containers: List<InventoryContainer> = emptyList(),
)

@Serializable
data class InventoryContainer(
    val name: String = "",
    val image: String = "",
    /** CRI state, e.g. CONTAINER_RUNNING. */
    val status: String = "",
    /** Bytes. */
    val memory: Long = 0,
)

const val OTHER_CATEGORY = "other"

/** The Go catalog's categories, in the order the filter chips show them. */
val APP_CATEGORIES = listOf(
    "system", "networking", "storage", "observability", "security", "database", "messaging",
    "devops", "media", "home", "productivity", "ai", "web", OTHER_CATEGORY,
)

/** Worth a look: several versions of one image, or an image that is not pinned. */
val InventoryApp.attention: Boolean get() = drift || unpinned

val List<InventoryApp>.attentionCount: Int get() = count { it.attention }

/** A category this app knows; anything newer counts as [OTHER_CATEGORY]. */
val InventoryApp.knownCategory: String get() = category.takeIf { it in APP_CATEGORIES } ?: OTHER_CATEGORY

/** Repositories that run with more than one tag. */
val InventoryApp.driftingRepos: Set<String>
    get() = images.filter { it.repo.isNotEmpty() }
        .groupBy { it.repo }
        .filterValues { list -> list.map { it.tag }.distinct().size > 1 }
        .keys

/** How many versions run side by side: the most tags one repository has (1 without drift). */
val InventoryApp.driftVersions: Int
    get() = images.filter { it.repo.isNotEmpty() }
        .groupBy { it.repo }
        .values
        .maxOfOrNull { list -> list.map { it.tag }.distinct().size }
        ?: 1

/** The slug to download when this app has no bundled icon, if it has a valid one. */
val InventoryApp.remoteIconSlug: String?
    get() = remoteIcon.takeIf { icon.isEmpty() && isValidIconSlug(it) }

/** The Apps screen's three parts: recognised apps, Kubernetes/Talos plumbing, and the rest. */
data class AppGroups(val main: List<InventoryApp>, val system: List<InventoryApp>, val unknown: List<InventoryApp>)

fun List<InventoryApp>.groups(): AppGroups = AppGroups(
    main = filter { it.known && !it.system },
    system = filter { it.system },
    unknown = filter { !it.known && !it.system },
)

sealed interface AppFilter {
    data object All : AppFilter
    data object Attention : AppFilter
    data class Category(val id: String) : AppFilter
}

/**
 * Apps matching [query] (case-insensitive, in the name, id, namespaces or image repositories)
 * and [filter], in their original order.
 */
fun List<InventoryApp>.filtered(query: String, filter: AppFilter): List<InventoryApp> {
    val q = query.trim()
    return filter { app ->
        val inFilter = when (filter) {
            AppFilter.All -> true
            AppFilter.Attention -> app.attention
            is AppFilter.Category -> app.knownCategory == filter.id
        }
        inFilter && (q.isEmpty() || app.matches(q))
    }
}

private fun InventoryApp.matches(q: String): Boolean =
    name.contains(q, ignoreCase = true) ||
        id.contains(q, ignoreCase = true) ||
        namespaces.any { it.contains(q, ignoreCase = true) } ||
        images.any { it.repo.contains(q, ignoreCase = true) }

/** Each category present with its number of apps, in [APP_CATEGORIES] order. */
fun List<InventoryApp>.categoryCounts(): List<Pair<String, Int>> {
    val counts = groupingBy { it.knownCategory }.eachCount()
    return APP_CATEGORIES.mapNotNull { id -> counts[id]?.let { id to it } }
}

/** Up to [max] non-system apps for the overview card, those with a bundled icon first. */
fun List<InventoryApp>.overviewTiles(max: Int = 6): List<InventoryApp> =
    filter { !it.system }.sortedBy { it.icon.isEmpty() }.take(max)

/** Up to two initials: of the first two words, else the first two letters. "?" when there are none. */
fun monogram(name: String): String {
    val words = name.split(' ', '-', '_', '.', '/').map { w -> w.filter { it.isLetterOrDigit() } }.filter { it.isNotEmpty() }
    val letters = when {
        words.size >= 2 -> "${words[0].first()}${words[1].first()}"
        words.size == 1 -> words[0].take(2)
        else -> "?"
    }
    return letters.uppercase()
}

/** A stable hue (0..360) for [id], so an app keeps its monogram colour (FNV-1a of its UTF-8 bytes, mod 360). */
fun monogramHue(id: String): Float {
    var hash = 0x811c9dc5.toInt()
    id.encodeToByteArray().forEach { byte ->
        hash = (hash xor (byte.toInt() and 0xff)) * 0x01000193
    }
    return (hash.toUInt() % 360u).toFloat()
}

private val ICON_SLUG = Regex("^[a-z0-9][a-z0-9-]{0,80}$")

fun isValidIconSlug(slug: String): Boolean = ICON_SLUG.matches(slug)

private const val REMOTE_ICON_BASE = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/"

/** Where a Dashboard Icons icon is downloaded from; null for an invalid slug. Only the slug is sent. */
fun remoteIconUrl(slug: String): String? = if (isValidIconSlug(slug)) "$REMOTE_ICON_BASE$slug.webp" else null

/**
 * The asset path of the bundled icon [icon], its `-night` variant in a [dark] theme when
 * [available] (the file names in assets/appicons) has it. Null for a name that is not a slug:
 * it comes from the Go core, so it is never trusted as a path.
 */
fun bundledIconAsset(icon: String, dark: Boolean, available: Set<String>): String? {
    if (!isValidIconSlug(icon)) return null
    val night = "$icon-night.webp"
    return "appicons/" + if (dark && night in available) night else "$icon.webp"
}

/** Larger than this per side is not an icon: refused before decoding (a tiny file can declare 16k×16k). */
const val MAX_ICON_SIDE = 1024

/** Tiles are at most 64dp: about this many pixels is plenty. */
const val ICON_TARGET_SIDE = 256

/**
 * The power-of-two downsampling (BitmapFactory's inSampleSize) that brings a [width]×[height]
 * image close to [ICON_TARGET_SIDE]; null when the size is unknown or over [MAX_ICON_SIDE].
 */
fun iconSampleSize(width: Int, height: Int): Int? {
    if (width <= 0 || height <= 0 || width > MAX_ICON_SIDE || height > MAX_ICON_SIDE) return null
    val side = maxOf(width, height)
    var sample = 1
    while (side / (sample * 2) >= ICON_TARGET_SIDE) sample *= 2
    return sample
}

enum class PodState { RUNNING, STARTING, STOPPED }

/** Running when every container runs; stopped as soon as one exited or is unknown. */
val InventoryPod.state: PodState
    get() = when {
        containers.all { it.status == RUNNING } -> PodState.RUNNING
        containers.all { it.status == RUNNING || it.status == CREATED } -> PodState.STARTING
        else -> PodState.STOPPED
    }

val InventoryPod.memory: Long get() = containers.sumOf { it.memory }

private const val RUNNING = "CONTAINER_RUNNING"
private const val CREATED = "CONTAINER_CREATED"
