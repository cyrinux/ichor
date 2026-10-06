package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosRolloutTest {

    private val latest = "v1.14.2"

    private fun node(name: String, version: String = "v1.14.1", cp: Boolean = false, reachable: Boolean = true, ready: Boolean = true) =
        NodeOverview(
            node = name, hostname = name, reachable = reachable, version = version,
            role = if (cp) "controlplane" else "worker", ready = reachable && ready,
        )

    private val cluster = listOf(
        node("w2"), node("w1", "v1.13.5"),
        node("cp2", cp = true), node("cp1", cp = true), node("cp3", latest, cp = true),
    )

    private fun Rollout.row(name: String) = (controlPlane + workers).first { it.node.hostname == name }

    @Test
    fun planByRoleInUpgradeOrder() {
        val plan = rollout(cluster, latest)
        // Pending first (oldest version, then hostname), done last.
        assertEquals(listOf("cp1", "cp2", "cp3"), plan.controlPlane.map { it.node.hostname })
        assertEquals(listOf("w1", "w2"), plan.workers.map { it.node.hostname })
        assertEquals(RolloutState.DONE, plan.row("cp3").state)
        assertEquals(RolloutState.PENDING, plan.row("cp1").state)
        assertEquals(1, plan.controlPlaneDone)
        assertEquals(0, plan.workersDone)
        assertNull(plan.hold)
        assertEquals("cp1", plan.next?.node?.hostname)
        assertTrue(plan.canOpen(plan.row("cp2")))
        assertFalse(plan.canOpen(plan.row("cp3")))
    }

    @Test
    fun workersAreASoftGateUntilTheControlPlaneIsDone() {
        val plan = rollout(cluster, latest)
        assertTrue(plan.workersWait)
        assertTrue(plan.canOpen(plan.row("w1")))
        assertTrue(plan.needsConfirm(plan.row("w1")))
        assertFalse(plan.needsConfirm(plan.row("cp1")))

        val done = rollout(cluster.map { if (it.role == "controlplane") it.copy(version = latest) else it }, latest)
        assertFalse(done.workersWait)
        assertFalse(done.needsConfirm(done.row("w1")))
        assertEquals("w1", done.next?.node?.hostname)
    }

    @Test
    fun oneAtATime() {
        val plan = rollout(cluster, latest, RolloutRun("cp1"))
        assertEquals(RolloutState.UPGRADING, plan.row("cp1").state)
        assertEquals(RolloutHold.Upgrading(plan.row("cp1").node, waiting = false), plan.hold)
        // Its own row opens the progress; every other one is held.
        assertTrue(plan.canOpen(plan.row("cp1")))
        assertFalse(plan.canOpen(plan.row("cp2")))
        assertFalse(plan.canOpen(plan.row("w1")))
        assertEquals("cp2", plan.next?.node?.hostname)
        assertFalse(plan.canOpen(plan.next!!))

        val rebooted = cluster.map { if (it.hostname == "cp1") it.copy(reachable = false, ready = false) else it }
        val waiting = rollout(rebooted, latest, RolloutRun("cp1", waiting = true), EtcdHealth(2, 3))
        assertEquals(RolloutState.WAITING_HEALTHY, waiting.row("cp1").state)
        assertEquals(RolloutHold.Upgrading(waiting.row("cp1").node, waiting = true), waiting.hold)
        assertEquals(EtcdHealth(2, 3), waiting.etcd)
    }

    @Test
    fun upgradedNodeWaitsUntilHealthy() {
        // On the new version but not ready yet, with no run followed (the app was restarted).
        val back = cluster.map { if (it.hostname == "cp1") it.copy(version = latest, ready = false) else it }
        val plan = rollout(back, latest)
        assertEquals(RolloutState.WAITING_HEALTHY, plan.row("cp1").state)
        assertEquals(RolloutHold.Unhealthy(listOf(plan.row("cp1").node)), plan.hold)
        assertFalse(plan.canOpen(plan.row("cp1")))
        assertFalse(plan.canOpen(plan.row("cp2")))

        // The run ended well, the overview still shows the old version: not pending again.
        val stale = rollout(cluster, latest, RolloutRun("cp1", waiting = true, finished = true))
        assertEquals(RolloutState.WAITING_HEALTHY, stale.row("cp1").state)
        assertEquals(RolloutHold.Upgrading(stale.row("cp1").node, waiting = true), stale.hold)
    }

    @Test
    fun unknownVersionsSortLast() {
        val plan = rollout(listOf(node("b", ""), node("a"), node("c", "v1.13.0")), latest)
        assertEquals(listOf("c", "a", "b"), plan.workers.map { it.node.hostname })
        assertEquals(RolloutState.PENDING, plan.row("b").state)
    }

    @Test
    fun failedNodeIsRetriedFirst() {
        val plan = rollout(cluster, latest, RolloutRun("cp2", finished = true, failed = true))
        assertEquals(RolloutState.FAILED, plan.row("cp2").state)
        assertNull(plan.hold)
        assertEquals("cp2", plan.next?.node?.hostname)
        assertTrue(plan.canOpen(plan.row("cp2")))
        assertTrue(plan.canOpen(plan.row("cp1")))
    }

    @Test
    fun degradedClusterHoldsTheRollout() {
        val down = cluster.map { if (it.hostname == "w2") it.copy(reachable = false, ready = false) else it }
        val plan = rollout(down, latest)
        assertEquals(RolloutHold.Unhealthy(listOf(plan.row("w2").node)), plan.hold)
        assertFalse(plan.canOpen(plan.row("cp1")))
        assertFalse(plan.canOpen(plan.row("w2"))) // unreachable: nothing to ask it

        // The only unhealthy node may itself be upgraded (or retried): no second node goes down.
        val notReady = cluster.map { if (it.hostname == "cp1") it.copy(ready = false) else it }
        val own = rollout(notReady, latest)
        assertTrue(own.canOpen(own.row("cp1")))
        assertFalse(own.canOpen(own.row("cp2")))

        val etcd = rollout(cluster, latest, etcd = EtcdHealth(2, 3))
        assertEquals(RolloutHold.Etcd(EtcdHealth(2, 3)), etcd.hold)
        assertFalse(etcd.canOpen(etcd.row("cp1")))
        assertNull(rollout(cluster, latest, etcd = EtcdHealth(3, 3)).hold)
    }

    @Test
    fun waitsForNodeOnceItRebooted() {
        assertFalse(upgradeWaitsForNode(listOf(UpgradeProgress("requested"), UpgradeProgress("installing"), UpgradeProgress("rebooting"))))
        assertTrue(upgradeWaitsForNode(listOf(UpgradeProgress("rebooting"), UpgradeProgress("waiting for node"))))
        assertTrue(upgradeWaitsForNode(listOf(UpgradeProgress("booted"))))
        assertFalse(upgradeWaitsForNode(emptyList()))
    }

    @Test
    fun etcdHealth() {
        val members = listOf(EtcdMember("a", "cp1"), EtcdMember("b", "cp2"), EtcdMember("c", "cp3"))
        val statuses = listOf(
            EtcdNodeStatus(node = "1", memberId = "a"),
            EtcdNodeStatus(node = "2", memberId = "b", errors = listOf("etcdserver: no leader")),
            EtcdNodeStatus(node = "3", error = "unreachable"),
        )
        assertEquals(EtcdHealth(1, 3), EtcdOverview(members = members, statuses = statuses).health())
        assertTrue(EtcdHealth(1, 3).degraded)
        assertNull(EtcdOverview(error = "no control plane answered").health())
        assertNull(EtcdOverview().health())
    }
}
