package name.levis.talosmobile.model

import name.levis.talosmobile.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UpgradeTest {

    private val plan = UpgradePlan(node = "192.0.2.10", currentVersion = "v1.14.0", currentImage = "ghcr.io/siderolabs/installer:v1.14.0")

    @Test
    fun decodesPlan() {
        val p = TalosJson.decodeFromString(
            UpgradePlan.serializer(),
            """{"node":"192.0.2.10","hostname":"cp-1","controlPlane":true,"currentVersion":"v1.14.0","currentImage":"factory.talos.dev/installer/abc:v1.14.0",
               "schematic":"abc","etcd":{"members":3,"healthy":2,"thisNodeMember":true,"quorumAfterLoss":false},
               "blockers":["etcd would lose quorum"],"warnings":null,"forceable":true}""",
        )
        assertTrue(p.controlPlane)
        assertEquals(EtcdUpgradeCheck(3, 2, thisNodeMember = true, quorumAfterLoss = false), p.etcd)
        assertTrue(p.warnings.isEmpty())
        assertTrue(p.etcdBlocked)
        assertNull(TalosJson.decodeFromString(UpgradePlan.serializer(), """{"node":"n","etcd":null}""").etcd)
    }

    @Test
    fun blockersDisableStart() {
        val blocked = plan.copy(blockers = listOf("node is not ready"))
        assertFalse(upgradeGate(blocked, "v1.14.2", "img", force = false, otherRunning = false).canStart)
        // Force cannot override blockers that are not all etcd checks.
        val gate = upgradeGate(blocked, "v1.14.2", "img", force = true, otherRunning = false)
        assertFalse(gate.canStart)
        assertFalse(gate.showForce)
    }

    @Test
    fun forceOnlyForEtcdBlockers() {
        val etcd = plan.copy(blockers = listOf("etcd would lose quorum"), forceable = true)
        val off = upgradeGate(etcd, "v1.14.2", "img", force = false, otherRunning = false)
        assertTrue(off.showForce)
        assertFalse(off.canStart)
        assertTrue(upgradeGate(etcd, "v1.14.2", "img", force = true, otherRunning = false).canStart)
        // No blockers: no force option, even if the core marked the plan forceable.
        assertFalse(upgradeGate(plan.copy(forceable = true), "v1.14.2", "img", force = false, otherRunning = false).showForce)
    }

    @Test
    fun startNeedsVersionImageAndNoOtherRun() {
        assertTrue(upgradeGate(plan, "v1.14.2", "img", force = false, otherRunning = false).canStart)
        assertFalse(upgradeGate(plan, " ", "img", force = false, otherRunning = false).canStart)
        assertFalse(upgradeGate(plan, "v1.14.2", "", force = false, otherRunning = false).canStart)
        assertFalse(upgradeGate(plan, "v1.14.2", "img", force = false, otherRunning = true).canStart)
        // Warnings never block.
        assertTrue(upgradeGate(plan.copy(warnings = listOf("w")), "v1.14.2", "img", force = false, otherRunning = false).canStart)
    }

    @Test
    fun phasesInOrder() {
        assertEquals(
            listOf("requested", "installing", "rebooting", "waiting for node", "booted", "done"),
            UpgradePhase.entries.map { it.wire },
        )
        assertEquals(UpgradePhase.WAITING, UpgradePhase.of("Waiting for node"))
        assertNull(UpgradePhase.of("unknown"))
    }

    @Test
    fun timelineWhileRunning() {
        val events = listOf(
            UpgradeProgress("requested", "requesting", 1),
            UpgradeProgress("installing", "draining", 2),
            UpgradeProgress("installing", "installing", 3),
            UpgradeProgress("mystery", "ignored", 4),
        )
        val steps = upgradeTimeline(events, finished = false, failed = false)
        assertEquals(UpgradePhase.entries, steps.map { it.phase })
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING),
            steps.map { it.status },
        )
        assertEquals(2L, steps[1].at)
        assertEquals("installing", steps[1].message)
    }

    @Test
    fun timelineSkippedPhasesAndEnd() {
        // A phase may be skipped (or reported out of order): everything before the latest is done.
        val events = listOf(UpgradeProgress("booted", "", 5), UpgradeProgress("requested", "", 1))
        val running = upgradeTimeline(events, finished = false, failed = false)
        assertEquals(StepStatus.DONE, running[UpgradePhase.REBOOTING.ordinal].status)
        assertEquals(StepStatus.CURRENT, running[UpgradePhase.BOOTED.ordinal].status)
        val done = upgradeTimeline(events + UpgradeProgress("done", "", 6), finished = true, failed = false)
        assertTrue(done.all { it.status == StepStatus.DONE })
        val failed = upgradeTimeline(events, finished = true, failed = true)
        assertEquals(StepStatus.FAILED, failed[UpgradePhase.BOOTED.ordinal].status)
        assertEquals(StepStatus.PENDING, failed[UpgradePhase.DONE.ordinal].status)
    }

    @Test
    fun failureBeforeAnyPhase() {
        val steps = upgradeTimeline(emptyList(), finished = true, failed = true)
        assertTrue(steps.all { it.status == StepStatus.PENDING })
    }

    @Test
    fun releaseSuggestionsStableFirst() {
        val releases = listOf(TalosRelease("v1.15.0-beta.1", prerelease = true), TalosRelease("v1.14.2"), TalosRelease("v1.14.1"))
        assertEquals(listOf("v1.14.2", "v1.14.1", "v1.15.0-beta.1"), releaseSuggestions(releases).map { it.version })
        assertEquals(listOf("v1.14.2", "v1.14.1"), releaseSuggestions(releases, includePrerelease = false).map { it.version })
    }
}
