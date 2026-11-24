package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EksUpgradeTest {
    @Test
    fun theAutoscalingAnswerNeverCarriesThePlan() {
        val report = TalosJson.decodeFromString(AutoscalingReport.serializer(), """{"hpa":{"installed":true},"eksUpgrade":{"installed":true}}""")
        assertFalse(report.eksUpgrade.installed)
    }

    @Test
    fun missingFieldsTakeTheirDefaults() {
        val plan = TalosJson.decodeFromString(EksUpgradePlan.serializer(), """{"updating":null}""")
        assertFalse(plan.installed)
        assertNull(plan.updating)
        assertFalse(plan.canUpgrade)
        assertEquals(EksUpgradeState.UNKNOWN, plan.state(null))
    }

    @Test
    fun statesFollowTheNextMinorAndTheUpdate() {
        assertEquals(EksUpgradeState.UP_TO_DATE, EksUpgradePlan(version = "1.35").state(null))
        assertEquals(EksUpgradeState.UNCHECKED, EksUpgradePlan(version = "1.34", next = "1.35").state(null))
        assertEquals(EksUpgradeState.AVAILABLE, EksUpgradePlan(version = "1.34", next = "1.35", nextKnown = true).state(null))

        val running = NodeGroupUpdate(id = "c-1", status = NODE_GROUP_UPDATE_IN_PROGRESS, type = NODE_GROUP_UPDATE_VERSION)
        val plan = EksUpgradePlan(version = "1.34", next = "1.35", nextKnown = true, updating = running)
        assertEquals(EksUpgradeState.RUNNING, plan.state(null))
        // The update the screen follows wins over the plan's.
        assertEquals(EksUpgradeState.AVAILABLE, plan.state(running.copy(status = NODE_GROUP_UPDATE_SUCCESSFUL)))
    }

    @Test
    fun aBlockerAnErrorOrARunningUpdateStopsTheUpgrade() {
        val ready = EksUpgradePlan(installed = true, version = "1.34", next = "1.35", nextKnown = true)
        assertTrue(ready.canUpgrade)
        assertFalse(ready.copy(blockers = listOf("node groups are behind")).canUpgrade)
        assertFalse(ready.copy(error = "eks:DescribeCluster is not allowed for this role").canUpgrade)
        assertFalse(ready.copy(next = "").canUpgrade)
        assertFalse(ready.copy(updating = NodeGroupUpdate(id = "c-1", status = NODE_GROUP_UPDATE_IN_PROGRESS)).canUpgrade)
    }
}
