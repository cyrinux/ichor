package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EtcdAlarm
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.KubeNodeInfo
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth
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

class KubeSnapshotTest {

    private val nodes = KubeNodesOverview(
        serverVersion = "v1.33.1",
        nodes = listOf(
            KubeNodeInfo(name = "ip-10-0-1-10", ready = true),
            KubeNodeInfo(name = "ip-10-0-1-11", ready = false, pressure = listOf("MemoryPressure", "DiskPressure")),
        ),
    )

    @Test
    fun nodesComeFromTheKubernetesList() {
        val s = kubeSnapshotOf(nodes, context = "eks", certNotAfter = 0, takenAt = 0)
        assertTrue(s.kube)
        assertEquals("eks", s.context)
        assertEquals(NodeState("ip-10-0-1-10", NodeHealth.READY, ""), s.nodes["ip-10-0-1-10"])
        assertEquals(NodeState("ip-10-0-1-11", NodeHealth.NOT_READY, "MemoryPressure; DiskPressure"), s.nodes["ip-10-0-1-11"])
        assertEquals(1, s.readyCount)
        assertEquals(1, s.notReadyCount)
        assertEquals(0, s.unreachableCount)
        assertFalse(s.unreachableAsAWhole)
    }

    @Test
    fun noEtcdAndTheTracksAsWatched() {
        val s = kubeSnapshotOf(nodes, context = "eks", certNotAfter = 0, takenAt = 0, gitopsWatched = true, gitopsIssues = null)
        assertFalse(s.etcdChecked)
        assertTrue(s.etcdAlarms.isEmpty())
        assertTrue(s.gitopsWatched)
        assertFalse(s.gitopsChecked)
    }

    @Test
    fun credentialsThatCannotListNodesGiveNoNode() {
        val s = kubeSnapshotOf(KubeNodesOverview(forbidden = true), context = "eks", certNotAfter = 0, takenAt = 0)
        assertTrue(s.nodes.isEmpty())
        assertFalse(s.unreachableAsAWhole)
    }
}
