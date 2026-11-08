package name.levis.ichor.widget

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterStatusTest {

    private val now = 1_800_000_000_000L

    private fun counts(total: Int = 3, ready: Int = 3, notReady: Int = 0, unreachable: Int = 0, etcdChecked: Boolean = true, alarms: Int = 0) =
        NodeCounts(total, ready, notReady, unreachable, etcdChecked, alarms)

    @Test
    fun staleAfterOneHour() {
        assertFalse(isStaleAt(now - WIDGET_STALE_AFTER_MS, now))
        assertTrue(isStaleAt(now - WIDGET_STALE_AFTER_MS - 1, now))
        assertTrue(isStaleAt(null, now))
    }

    @Test
    fun problemsComeFirstThenEtcd() {
        assertEquals(listOf(StatusItem.AllReady, StatusItem.Etcd(0)), statusItems(counts()))
        assertEquals(
            listOf(StatusItem.NotReady(1), StatusItem.Unreachable(1)),
            statusItems(counts(ready = 1, notReady = 1, unreachable = 1, alarms = 2)),
        )
        assertEquals(listOf(StatusItem.AllReady), statusItems(counts(etcdChecked = false)))
    }

    @Test
    fun aTunnelDownIsBadAndNeverAllReady() {
        val down = counts().copy(kubespanChecked = true, tunnelsDown = 2)
        assertEquals(listOf(StatusItem.TunnelsDown(2), StatusItem.Etcd(0)), statusItems(down))
        assertEquals(
            listOf(StatusItem.Unreachable(1), StatusItem.TunnelsDown(2)),
            statusItems(counts(ready = 2, unreachable = 1).copy(kubespanChecked = true, tunnelsDown = 2)),
        )
        assertEquals(Tone.BAD, toneOf(StatusItem.TunnelsDown(1)))
        assertEquals(Tone.BAD, toneOf(down, now, now))
        // Not read this time: says nothing.
        assertEquals(StatusItem.AllReady, statusItems(counts().copy(tunnelsDown = 2)).first())
    }

    @Test
    fun toneIsTheWorstItemEvenBeyondTheTwoShown() {
        assertEquals(Tone.OK, toneOf(counts(), now, now))
        assertEquals(Tone.WARN, toneOf(counts(ready = 2, notReady = 1), now, now))
        // Not ready and unreachable fill the two slots; the etcd alarm still makes it bad.
        assertEquals(Tone.BAD, toneOf(counts(ready = 1, notReady = 1, unreachable = 1, alarms = 1), now, now))
        assertEquals(Tone.BAD, toneOf(counts(alarms = 1), now, now))
        assertEquals(Tone.UNKNOWN, toneOf(counts(), now - WIDGET_STALE_AFTER_MS - 1, now))
        assertEquals(Tone.UNKNOWN, toneOf(null, null, now))
        assertEquals(Tone.UNKNOWN, toneOf(counts(total = 0, ready = 0, etcdChecked = false), now, now))
    }

    @Test
    fun payloadRoundTripsAndToleratesGarbage() {
        val p = WearPayload(listOf(WearCluster("lab", now, counts())))
        assertEquals(p, decodeWearPayload(encodeWearPayload(p)))
        assertNull(decodeWearPayload("not json".encodeToByteArray()))
    }
}
