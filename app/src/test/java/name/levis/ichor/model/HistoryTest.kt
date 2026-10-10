package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HistoryTest {

    @Test
    fun segmentsAreFractionsOfTheWindowWithGapsLeftOut() {
        val span = HistoryNodeSpan(
            "10.0.0.21",
            intervals = listOf(
                HistoryState(HISTORY_READY, 0, 400),
                // 400..600: a gap.
                HistoryState(HISTORY_NOT_READY, 600, 700),
                HistoryState(HISTORY_READY, 700, 2_000),
            ),
        )
        val segments = uptimeSegments(span, from = 0, to = 1_000)
        assertEquals(
            listOf(
                UptimeSegment(HISTORY_READY, 0f, 0.4f),
                UptimeSegment(HISTORY_NOT_READY, 0.6f, 0.7f),
                UptimeSegment(HISTORY_READY, 0.7f, 1f),
            ),
            segments,
        )
        assertTrue(uptimeSegments(span, from = 1_000, to = 1_000).isEmpty())
    }

    @Test
    fun queriesDecodeAsTheCoreWritesThem() {
        val json = """
            {"from":1780000000000,"to":1780086400000,"records":96,
             "nodes":[{"node":"10.0.0.21","hostname":"worker-1","uptimePercent":97.92,
                       "intervals":[{"state":"ready","from":1780000000000,"to":1780040000000}]}],
             "alerts":[{"key":"data:longhorn|pvc-data","track":"data","openedAt":1780010000000}],
             "volumes":[{"key":"10.0.0.11|EPHEMERAL","name":"EPHEMERAL","node":"10.0.0.11","series":[[1780000000000,61.9]]}],
             "memory":[{"node":"10.0.0.11","series":[[1780000000000,41.3]]}],
             "gaps":[]}
        """.trimIndent()
        val query = TalosJson.decodeFromString(HistoryQuery.serializer(), json)
        assertEquals(97.92, query.nodes.single().uptimePercent!!, 0.0001)
        assertEquals(null, query.alerts.single().closedAt)
        assertEquals(61.9, query.volumes.single().series.single()[1], 0.0001)
        assertEquals(41.3, query.memory.single().series.single()[1], 0.0001)
    }
}
