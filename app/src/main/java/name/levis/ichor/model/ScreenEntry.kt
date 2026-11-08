package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** A screen of the active cluster as the Go core ranks it (SearchScreens): titles already localised. */
@Serializable
data class ScreenEntry(
    val id: String,
    val title: String,
    val subtitle: String = "",
    val keywords: List<String> = emptyList(),
    val section: String = "",
)

@Serializable
data class ScreenEntries(val screens: List<ScreenEntry>)

/** One ranked screen; [id] names the catalog entry it opens. */
@Serializable
data class ScreenMatch(
    val id: String,
    val title: String,
    val subtitle: String = "",
    val section: String = "",
    val score: Double = 0.0,
)

@Serializable
data class ScreenMatches(val results: List<ScreenMatch> = emptyList(), val total: Int = 0)
