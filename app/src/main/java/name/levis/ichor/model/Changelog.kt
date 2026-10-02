package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.time.LocalDate
import java.time.OffsetDateTime

// Mirrors changelog.json, generated at build time by scripts/changelog.py (bundled as an
// asset, and published with each GitHub release).

@Serializable
data class Changelog(
    @SerialName("generated_at") val generatedAt: String = "",
    /** Newest first. */
    val releases: List<ChangelogRelease> = emptyList(),
)

@Serializable
data class ChangelogRelease(
    val version: String = "",
    /** The release's versionCode. */
    @SerialName("build_number") val buildNumber: Int = 0,
    /** ISO-8601, e.g. "2026-10-01T20:21:33+02:00". */
    @SerialName("published_at") val publishedAt: String = "",
    val sections: List<ChangelogSection> = emptyList(),
)

@Serializable
data class ChangelogSection(
    val kind: String = "",
    val title: String = "",
    val items: List<String> = emptyList(),
)

/** Section kinds with a translated heading; any other kind shows the JSON title. */
enum class ChangelogKind { NEW, FIXED, FASTER, BREAKING }

val ChangelogSection.knownKind: ChangelogKind?
    get() = when (kind.trim().lowercase()) {
        "new" -> ChangelogKind.NEW
        "fixed" -> ChangelogKind.FIXED
        "faster" -> ChangelogKind.FASTER
        "breaking" -> ChangelogKind.BREAKING
        else -> null
    }

/** Sections worth showing; a release without any is a "maintenance release". */
val ChangelogRelease.visibleSections: List<ChangelogSection> get() = sections.filter { it.items.isNotEmpty() }

/** The publication day, or null when the date is missing or unreadable. */
val ChangelogRelease.publishedDate: LocalDate?
    get() = runCatching { OffsetDateTime.parse(publishedAt).toLocalDate() }.getOrNull()
        ?: runCatching { LocalDate.parse(publishedAt.take(10)) }.getOrNull()

private val ChangelogJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
}

/** The changelog in [json]; empty when it is missing or corrupt (never an error for the user). */
fun decodeChangelog(json: String?): Changelog =
    json?.let { runCatching { ChangelogJson.decodeFromString(Changelog.serializer(), it) }.getOrNull() } ?: Changelog()

/** Releases after the build [build], newest first. */
fun Changelog.releasesSince(build: Int): List<ChangelogRelease> =
    releases.filter { it.buildNumber > build }.sortedByDescending { it.buildNumber }

/**
 * The releases to show in "What's new" when the app starts as build [current] after
 * [previous] ran last (null: first launch). Null means no dialog: first install, same build,
 * downgrade, or nothing bundled about the builds in between.
 */
fun whatsNew(previous: Int?, current: Int, changelog: Changelog): List<ChangelogRelease>? {
    if (previous == null || current <= previous) return null
    return changelog.releasesSince(previous).filter { it.buildNumber <= current }.ifEmpty { null }
}
