package name.levis.ichor.widget

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FleetTallyTest {

    private val now = 1_800_000_000_000L

    @Test
    fun toneIsTheWorstNonEmptyCount() {
        assertEquals(Tone.BAD, toneOf(FleetTally(bad = 1, warn = 1, ok = 4, total = 6, takenAt = now), now))
        assertEquals(Tone.WARN, toneOf(FleetTally(warn = 2, ok = 4, total = 6, takenAt = now), now))
        assertEquals(Tone.UNKNOWN, toneOf(FleetTally(unknown = 1, ok = 5, total = 6, takenAt = now), now))
        assertEquals(Tone.OK, toneOf(FleetTally(ok = 6, total = 6, takenAt = now), now))
    }

    @Test
    fun noFleetOrAStaleNewestCheckIsUnknown() {
        assertEquals(Tone.UNKNOWN, toneOf(null, now))
        assertEquals(Tone.UNKNOWN, toneOf(FleetTally(), now))
        assertEquals(Tone.UNKNOWN, toneOf(FleetTally(bad = 2, total = 2, takenAt = 0), now))
        assertEquals(Tone.UNKNOWN, toneOf(FleetTally(bad = 2, total = 2, takenAt = now - WIDGET_STALE_AFTER_MS - 1), now))
        assertEquals(Tone.BAD, toneOf(FleetTally(bad = 2, total = 2, takenAt = now - WIDGET_STALE_AFTER_MS), now))
    }

    @Test
    fun needAttentionCountsBadAndWarn() {
        assertEquals(3, FleetTally(bad = 1, warn = 2, unknown = 5, ok = 1, total = 9).needAttention)
    }

    @Test
    fun payloadCarriesTheFleetAndReadsAnOlderOne() {
        val p = WearPayload(emptyList(), FleetTally(bad = 1, ok = 2, total = 3, takenAt = now))
        assertEquals(p, decodeWearPayload(encodeWearPayload(p)))
        // An older phone app sends no fleet: the watch shows no fleet tally.
        assertNull(decodeWearPayload("""{"clusters":[]}""".encodeToByteArray())?.fleet)
        // An older watch app (no fleet field) still reads the newer payload: unknown keys are skipped.
        val older = Json { ignoreUnknownKeys = true }
        val bytes = encodeWearPayload(p).decodeToString()
        assertEquals(emptyList<WearCluster>(), older.decodeFromString(OlderPayload.serializer(), bytes).clusters)
    }

    @Serializable
    private data class OlderPayload(val clusters: List<WearCluster> = emptyList())
}
