package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MaintenanceTest {

    private val plan = MaintenancePlan(node = "192.0.2.10", hostname = "w-1")

    @Test
    fun decodesPlan() {
        val p = TalosJson.decodeFromString(
            MaintenancePlan.serializer(),
            """{"node":"192.0.2.10","hostname":"cp-1","kubeNode":"cp-1","controlPlane":true,"cordoned":true,
               "pods":[{"namespace":"db","name":"pg-1","owner":"Cluster/pg","kind":"evict","emptyDir":false,"pdb":"pg","pdbAllowed":0},
                       {"namespace":"kube-system","name":"cilium-x","owner":"DaemonSet/cilium","kind":"daemonset","emptyDir":false,"pdb":"","pdbAllowed":-1}],
               "blockers":null,"warnings":["w"],"acknowledge":["etcd keeps quorum"],"future":1}""",
        )
        assertTrue(p.controlPlane)
        assertTrue(p.cordoned)
        assertEquals("cp-1", p.kubeNode)
        assertEquals(2, p.pods.size)
        assertTrue(p.pods[0].pdbBlocks)
        assertFalse(p.pods[1].pdbBlocks)
        assertTrue(p.blockers.isEmpty()) // null from Go
        assertEquals(listOf("etcd keeps quorum"), p.acknowledge)
    }

    @Test
    fun decodesProgress() {
        val e = TalosJson.decodeFromString(
            MaintenanceProgress.serializer(),
            """{"phase":"drain","message":"1 of 2 pods evicted","at":42,
               "pods":[{"namespace":"db","name":"pg-1","kind":"evict","pdb":"pg","pdbAllowed":0,"state":"blocked","reason":"PDB pg allows 0 disruptions"}]}""",
        )
        assertEquals(MaintenancePhase.DRAIN, MaintenancePhase.of(e.phase))
        assertEquals(DrainPod.STATE_BLOCKED, e.pods.single().state)
        assertTrue(TalosJson.decodeFromString(MaintenanceProgress.serializer(), """{"phase":"cordon"}""").pods.isEmpty())
    }

    @Test
    fun groupsPods() {
        val groups = plan.copy(
            pods = listOf(
                DrainPod("a", "evicted", kind = DrainPod.KIND_EVICT),
                DrainPod("a", "bare", kind = DrainPod.KIND_BARE),
                DrainPod("a", "ds", kind = DrainPod.KIND_DAEMONSET),
                DrainPod("a", "static", kind = DrainPod.KIND_STATIC),
                DrainPod("a", "newer", kind = "something-new"),
            ),
        ).drainGroups()
        assertEquals(listOf("evicted"), groups.evict.map { it.name })
        assertEquals(listOf("bare"), groups.bare.map { it.name })
        assertEquals(listOf("ds", "static", "newer"), groups.leftAlone.map { it.name })
    }

    @Test
    fun blockersOnlyStopRebootAndShutdown() {
        val blocked = plan.copy(blockers = listOf("etcd would lose quorum"))
        assertFalse(maintenanceCanStart(blocked, MaintenanceAction.REBOOT, 0, otherRunning = false))
        assertFalse(maintenanceCanStart(blocked, MaintenanceAction.SHUTDOWN, 0, otherRunning = false))
        assertTrue(maintenanceCanStart(blocked, MaintenanceAction.NONE, 0, otherRunning = false))
        assertFalse(maintenanceCanStart(plan, MaintenanceAction.NONE, 0, otherRunning = true))
    }

    @Test
    fun acknowledgmentsMustAllBeTicked() {
        val ack = plan.copy(acknowledge = listOf("a", "b"))
        assertFalse(maintenanceCanStart(ack, MaintenanceAction.REBOOT, 1, otherRunning = false))
        assertTrue(maintenanceCanStart(ack, MaintenanceAction.REBOOT, 2, otherRunning = false))
        assertTrue(maintenanceCanStart(ack, MaintenanceAction.NONE, 0, otherRunning = false))
        assertTrue(maintenanceAcknowledged(ack, MaintenanceAction.REBOOT, 2))
        assertFalse(maintenanceAcknowledged(ack, MaintenanceAction.REBOOT, 1))
        // Nothing to acknowledge, or a drain alone: the core needs no acknowledgment.
        assertFalse(maintenanceAcknowledged(plan, MaintenanceAction.REBOOT, 0))
        assertFalse(maintenanceAcknowledged(ack, MaintenanceAction.NONE, 2))
    }

    @Test
    fun timelineFollowsTheAction() {
        assertEquals(5, maintenancePhases(MaintenanceAction.REBOOT).size)
        assertEquals(listOf(MaintenancePhase.CORDON, MaintenancePhase.DRAIN, MaintenancePhase.SHUTDOWN), maintenancePhases(MaintenanceAction.SHUTDOWN))

        val events = listOf(
            MaintenanceProgress("cordon", "cordoning w-1", at = 1),
            MaintenanceProgress("drain", "evicting 2 pods", at = 2),
            MaintenanceProgress("drain", "1 of 2 pods evicted", at = 3),
        )
        val steps = maintenanceTimeline(MaintenanceAction.REBOOT, events, finished = false, failed = false)
        assertEquals(listOf(StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING), steps.map { it.status })
        assertEquals(2L, steps[1].at) // first time
        assertEquals("1 of 2 pods evicted", steps[1].message) // last message

        val failed = maintenanceTimeline(MaintenanceAction.REBOOT, events, finished = true, failed = true)
        assertEquals(StepStatus.FAILED, failed[1].status)
        val done = maintenanceTimeline(MaintenanceAction.NONE, events, finished = true, failed = false)
        assertTrue(done.all { it.status == StepStatus.DONE })
        // Refused before any step (e.g. the demo cluster): the first step failed.
        assertEquals(StepStatus.FAILED, maintenanceTimeline(MaintenanceAction.REBOOT, emptyList(), finished = true, failed = true)[0].status)
    }

    @Test
    fun cordonAfterRun() {
        assertNull(cordonedAfter(MaintenanceAction.REBOOT, null, running = false, failed = true, wasCordoned = false))
        assertNull(cordonedAfter(MaintenanceAction.REBOOT, MaintenancePhase.CORDON, running = false, failed = true, wasCordoned = false))
        assertEquals(false, cordonedAfter(MaintenanceAction.REBOOT, MaintenancePhase.UNCORDON, running = false, failed = false, wasCordoned = false))
        // Cordoned before the run: a successful reboot leaves it cordoned.
        assertEquals(true, cordonedAfter(MaintenanceAction.REBOOT, MaintenancePhase.UNCORDON, running = false, failed = false, wasCordoned = true))
        assertEquals(true, cordonedAfter(MaintenanceAction.REBOOT, MaintenancePhase.WAITING, running = false, failed = true, wasCordoned = false))
        assertEquals(true, cordonedAfter(MaintenanceAction.SHUTDOWN, MaintenancePhase.SHUTDOWN, running = false, failed = false, wasCordoned = false))
        assertEquals(true, cordonedAfter(MaintenanceAction.NONE, MaintenancePhase.DRAIN, running = false, failed = false, wasCordoned = false))
    }
}
