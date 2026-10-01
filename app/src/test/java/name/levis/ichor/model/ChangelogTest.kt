package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

class ChangelogTest {

    private val json = """
        {"generated_at":"2026-10-01T18:37:03+00:00","releases":[
          {"version":"0.4.1","build_number":37,"published_at":"2026-10-01T20:21:33+02:00",
           "sections":[{"kind":"new","title":"New","items":["Widget"]},{"kind":"docs","title":"Documentation","items":["README"]}]},
          {"version":"0.4.0","build_number":36,"published_at":"2026-09-30T23:30:00-05:00","sections":[{"kind":"Fixed","title":"Fixed","items":["Crash"]}]},
          {"version":"0.3.9","build_number":35,"published_at":"","sections":null},
          {"version":"0.3.8","build_number":30,"published_at":"2026-08-01","sections":[{"kind":"faster","title":"Faster","items":[]}], "future_field": 1}
        ]}
    """.trimIndent()

    private val changelog = decodeChangelog(json)

    @Test
    fun decodes() {
        assertEquals(listOf(37, 36, 35, 30), changelog.releases.map { it.buildNumber })
        assertEquals("0.4.1", changelog.releases.first().version)
        assertEquals(listOf("Widget"), changelog.releases.first().sections.first().items)
        assertTrue(changelog.releases[2].sections.isEmpty())
    }

    @Test
    fun missingOrCorruptIsEmpty() {
        assertTrue(decodeChangelog(null).releases.isEmpty())
        assertTrue(decodeChangelog("").releases.isEmpty())
        assertTrue(decodeChangelog("{not json").releases.isEmpty())
        assertTrue(decodeChangelog("""{"releases":"oops"}""").releases.isEmpty())
        assertTrue(decodeChangelog("{}").releases.isEmpty())
    }

    @Test
    fun releasesSinceABuildNewestFirst() {
        assertEquals(listOf(37, 36), changelog.releasesSince(35).map { it.buildNumber })
        assertEquals(listOf(37, 36, 35, 30), changelog.releasesSince(0).map { it.buildNumber })
        assertTrue(changelog.releasesSince(37).isEmpty())
        val shuffled = Changelog(releases = changelog.releases.reversed())
        assertEquals(listOf(37, 36), shuffled.releasesSince(35).map { it.buildNumber })
    }

    @Test
    fun whatsNewAfterAnUpdateShowsEverySkippedRelease() {
        assertEquals(listOf(37, 36, 35), whatsNew(previous = 30, current = 37, changelog)?.map { it.buildNumber })
        assertEquals(listOf(36), whatsNew(previous = 35, current = 36, changelog)?.map { it.buildNumber })
    }

    @Test
    fun noWhatsNewOnFirstInstallSameBuildOrDowngrade() {
        assertNull(whatsNew(previous = null, current = 37, changelog))
        assertNull(whatsNew(previous = 37, current = 37, changelog))
        assertNull(whatsNew(previous = 37, current = 36, changelog))
    }

    @Test
    fun noWhatsNewWithoutNotes() {
        assertNull(whatsNew(previous = 36, current = 37, Changelog()))
        // A local build after the last bundled release: nothing to tell.
        assertNull(whatsNew(previous = 37, current = 40, changelog))
    }

    @Test
    fun sectionKinds() {
        val sections = changelog.releases.flatMap { it.sections }
        assertEquals(ChangelogKind.NEW, sections[0].knownKind)
        assertNull(sections[1].knownKind)
        assertEquals("Documentation", sections[1].title)
        assertEquals(ChangelogKind.FIXED, sections[2].knownKind)
        assertEquals(ChangelogKind.FASTER, sections[3].knownKind)
        assertEquals(ChangelogKind.BREAKING, ChangelogSection(kind = " breaking ").knownKind)
    }

    @Test
    fun releasesWithoutItemsAreMaintenanceReleases() {
        assertEquals(2, changelog.releases[0].visibleSections.size)
        assertTrue(changelog.releases[2].visibleSections.isEmpty())
        assertTrue(changelog.releases[3].visibleSections.isEmpty())
    }

    @Test
    fun publishedDates() {
        assertEquals(LocalDate.of(2026, 10, 1), changelog.releases[0].publishedDate)
        // The day where it was published, not converted to another zone.
        assertEquals(LocalDate.of(2026, 9, 30), changelog.releases[1].publishedDate)
        assertNull(changelog.releases[2].publishedDate)
        assertEquals(LocalDate.of(2026, 8, 1), changelog.releases[3].publishedDate)
    }
}
