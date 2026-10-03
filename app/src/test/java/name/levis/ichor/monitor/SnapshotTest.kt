package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EtcdAlarm
import name.levis.ichor.model.EtcdOverview
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SnapshotTest {

    private val overview = ClusterOverview(context = "lab", nodes = emptyList())

    private fun snap(etcd: EtcdOverview?) = snapshotOf(overview, etcd, certNotAfter = 0, takenAt = 0)

    @Test
    fun answeredAlarmListIsChecked() {
        val s = snap(EtcdOverview(alarms = listOf(EtcdAlarm("beef", "NOSPACE"))))
        assertTrue(s.etcdChecked)
        assertEquals(listOf("beef:NOSPACE"), s.etcdAlarms)
    }

    @Test
    fun failedAlarmListIsNotChecked() {
        assertFalse(snap(EtcdOverview(alarmsError = "permission denied")).etcdChecked)
    }

    @Test
    fun etcdErrorOrMissingIsNotChecked() {
        assertFalse(snap(EtcdOverview(error = "member list: down")).etcdChecked)
        assertFalse(snap(null).etcdChecked)
    }
}
