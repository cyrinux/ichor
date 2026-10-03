package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import name.levis.ichor.data.TalosJson

/**
 * The features open for funding (Google Play build), published as docs/roadmap.json on the
 * website. Each feature lists the Play in-app products that back it; what they raised is
 * read from the Play Console reports and written back into [RoadmapFeature.raised] by hand.
 */
data class Roadmap(
    /** ISO 4217 code of [RoadmapFeature.goal] and [RoadmapFeature.raised]. */
    val currency: String,
    /** Products that support the project as a whole rather than one feature. */
    val tips: List<String>,
    val features: List<RoadmapFeature>,
) {
    /** Every product to price in Play: tips first, then the fundable features' in display order. */
    val productIds: List<String> get() = (tips + features.filter { it.fundable }.flatMap { it.products }).distinct()
}

@Serializable
data class RoadmapFeature(
    val id: String,
    /** Language code ("en", "fr"…) → text; English is the fallback and required. */
    val title: Map<String, String>,
    val description: Map<String, String> = emptyMap(),
    /** Whole units of [Roadmap.currency]; 0 hides the progress bar. */
    val goal: Int = 0,
    val raised: Int = 0,
    val status: FeatureStatus = FeatureStatus.OPEN,
    val products: List<String> = emptyList(),
    /** Discussion of the feature (https only). */
    val issue: String? = null,
    /** The app version it shipped in, for [FeatureStatus.SHIPPED]. */
    val shippedIn: String? = null,
) {
    val fundable: Boolean get() = status != FeatureStatus.SHIPPED && products.isNotEmpty()

    val progress: Float get() = if (goal <= 0) 0f else (raised.toFloat() / goal).coerceIn(0f, 1f)

    /** True when one of [bought] (product ids bought on this device) backs this feature. */
    fun backedWith(bought: Set<String>): Boolean = products.any { it in bought }
}

@Serializable
enum class FeatureStatus {
    @SerialName("open") OPEN,
    @SerialName("in_progress") IN_PROGRESS,
    @SerialName("shipped") SHIPPED,
}

/** [lang]'s text, else English, else any; empty when there is none. */
fun Map<String, String>.localized(lang: String): String = get(lang) ?: get("en") ?: values.firstOrNull().orEmpty()

/**
 * The roadmap in [json], or null when it is missing or corrupt. Entries without an id or an
 * English title, malformed ones and repeated ids are dropped, as are malformed product ids
 * and non-https links. Features in progress come first and shipped ones last; an unknown
 * status reads as open.
 */
fun decodeRoadmap(json: String?): Roadmap? {
    if (json.isNullOrBlank()) return null
    val roadmap = runCatching { TalosJson.decodeFromString(RoadmapJson.serializer(), json) }.getOrNull() ?: return null
    val features = roadmap.features
        .mapNotNull { runCatching { TalosJson.decodeFromJsonElement(RoadmapFeature.serializer(), it) }.getOrNull() }
        .filter { it.id.isNotBlank() && !it.title["en"].isNullOrBlank() }
        .distinctBy { it.id } // the list's keys: a duplicate would crash it
        .map { it.copy(products = it.products.filter(::isProductId), issue = it.issue?.takeIf { url -> url.startsWith("https://") }) }
        .sortedBy { STATUS_ORDER.indexOf(it.status) }
    return Roadmap(roadmap.currency, roadmap.tips.filter(::isProductId), features)
}

/** docs/roadmap.json as written; features are decoded one by one, so a malformed entry drops only itself. */
@Serializable
private data class RoadmapJson(
    val currency: String = "EUR",
    val tips: List<String> = emptyList(),
    val features: List<JsonElement> = emptyList(),
)

private val STATUS_ORDER = listOf(FeatureStatus.IN_PROGRESS, FeatureStatus.OPEN, FeatureStatus.SHIPPED)

/** Play product ids: lowercase letters, digits, "_" and ".", starting with a letter or digit. */
private fun isProductId(id: String): Boolean = PRODUCT_ID.matches(id)

private val PRODUCT_ID = Regex("^[a-z0-9][a-z0-9_.]{0,138}$")
