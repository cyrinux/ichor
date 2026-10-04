package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LonghornActionsTest {
    private val attached = LonghornVolume(name = "pvc-1", namespace = "longhorn-system", state = "attached", replicasDesired = 3)

    @Test
    fun volumeActionsNeedItAttached() {
        assertEquals(listOf(LonghornAction.BACKUP, LonghornAction.TRIM, LonghornAction.REPLICAS), attached.actions)
        assertEquals(listOf(LonghornAction.REPLICAS), attached.copy(state = "detached").actions)
        // No second backup while one runs.
        assertEquals(listOf(LonghornAction.TRIM, LonghornAction.REPLICAS), attached.copy(backingUp = true).actions)
    }

    @Test
    fun nodeActionsFollowItsSpec() {
        val node = LonghornNode(name = "worker-1", allowScheduling = true)
        assertEquals(listOf(LonghornAction.SCHEDULING_OFF, LonghornAction.EVICT), node.actions)
        assertEquals(
            listOf(LonghornAction.SCHEDULING_ON, LonghornAction.CANCEL_EVICTION),
            node.copy(allowScheduling = false, evictionRequested = true).actions,
        )
    }

    @Test
    fun replicaChoicesCoverTheNodesAndTheCurrentCount() {
        val twoNodes = LonghornStatus(nodes = listOf(LonghornNode("a"), LonghornNode("b")))
        assertEquals(1..3, twoNodes.replicaChoices(attached))
        assertEquals(1..2, twoNodes.replicaChoices(attached.copy(replicasDesired = 1)))
        assertEquals(1..1, LonghornStatus().replicaChoices(attached.copy(replicasDesired = 0)))
        val many = LonghornStatus(nodes = (1..30).map { LonghornNode("n$it") })
        assertEquals(1..LONGHORN_MAX_REPLICAS, many.replicaChoices(attached))
    }

    @Test
    fun decodesProgressAndNodeState() {
        val json = """{"version":"v1beta2","volumes":[{"name":"pvc-1","state":"attached","rebuilding":1,"rebuildProgress":42,
            "backingUp":true,"backupProgress":63,"scheduleError":"insufficient storage","tooManySnapshots":true}],
            "nodes":[{"name":"w1","namespace":"storage","allowScheduling":false,"evictionRequested":true,"replicas":4}]}"""
        val s = TalosJson.decodeFromString(LonghornStatus.serializer(), json)
        val v = s.volumes.single()
        assertEquals(42, v.rebuildProgress)
        assertTrue(v.backingUp && v.tooManySnapshots)
        assertEquals(63, v.backupProgress)
        assertFalse(v.restoring)
        assertEquals("insufficient storage", v.scheduleError)
        val n = s.nodes.single()
        assertEquals("storage", n.namespace)
        assertTrue(n.evictionRequested && !n.allowScheduling)
        assertEquals(4, n.replicas)
    }
}
