package name.levis.ichor.model

import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DriftRowsTest {
    private fun decode(json: String) = TalosJson.decodeFromString(ListSerializer(DriftChange.serializer()), json)

    @Test
    fun decodesItemisedLists() {
        val change = decode(
            """[{"node":"192.0.2.21","reference":"192.0.2.20","nodeName":"demo-worker-2","referenceName":"demo-worker-1",
            "key":"extensions","before":"a","after":"b","added":["btrfs=v1.14.1"],"removed":["tools=1.0"],
            "changed":[{"name":"schematic","before":"0123abcd0123abcd0123","after":"4567ef014567ef014567"}]}]""",
        ).single()
        assertEquals("demo-worker-2", change.nodeName)
        assertEquals("demo-worker-1", change.referenceName)
        assertEquals(listOf("btrfs=v1.14.1"), change.added)
        assertEquals(listOf("tools=1.0"), change.removed)
        assertEquals(listOf(DriftPair("schematic", "0123abcd0123abcd0123", "4567ef014567ef014567")), change.changed)
        assertEquals(
            listOf(
                DriftRow(DriftRowKind.ADDED, "btrfs=v1.14.1", "btrfs=v1.14.1"),
                DriftRow(DriftRowKind.REMOVED, "tools=1.0", "tools=1.0"),
                DriftRow(DriftRowKind.CHANGED, "schematic 0123abcd… → 4567ef01…", "schematic 0123abcd0123abcd0123 → 4567ef014567ef014567"),
            ),
            change.rows(),
        )
    }

    @Test
    fun olderCoreFallsBackToBeforeAfter() {
        val change = decode("""[{"node":"192.0.2.21","reference":"192.0.2.20","key":"secureBoot","before":"true","after":"false"}]""").single()
        assertEquals("", change.nodeName)
        assertTrue(change.added.isEmpty() && change.removed.isEmpty() && change.changed.isEmpty())
        assertTrue(change.rows().isEmpty())
        assertEquals("true → false", change.beforeAfter())
        assertEquals("∅ → false", change.copy(before = "").beforeAfter())
    }

    @Test
    fun nullListsDecodeAsEmpty() {
        val change = decode("""[{"node":"n","reference":"r","key":"dns","before":"a","after":"b","added":null,"removed":null,"changed":null}]""").single()
        assertTrue(change.rows().isEmpty())
    }

    @Test
    fun shortensOnlyLongValues() {
        assertEquals("1500", shortDriftValue("1500"))
        assertEquals("0123456789abcdef", shortDriftValue("0123456789abcdef"))
        assertEquals("01234567…", shortDriftValue("0123456789abcdefX"))
        val change = DriftChange("n", "r", "extensions", "", "", added = listOf("schematic=0123abcd0123abcd0123abcd", "time.example.org.long"))
        assertEquals(listOf("schematic=0123abcd…", "time.example.org.long"), change.rows().map { it.text })
        assertEquals("schematic=0123abcd0123abcd0123abcd", change.rows().first().full)
    }
}
