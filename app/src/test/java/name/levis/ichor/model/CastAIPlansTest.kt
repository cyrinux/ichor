package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CastAIPlansTest {
    private val hour = 3_600_000L
    private val now = 1_800_000_000_000L

    private val json = """
        {"castai":{"version":"v1","recommendations":[],
         "stuck":[{"node":"edge-aaaa","failures":1,"retrying":true}],
         "plans":[
          {"name":"p4","createdAt":${now - hour / 10},"mode":"delete-empty","state":"Running","execute":true,"currency":"USD",
           "beforeMonthly":60,"afterMonthly":0,"savingsPercent":100,"clusterMonthly":4800,"clusterNodes":38,
           "removing":[{"name":"edge-aaaa","status":"inProgress","events":[{"at":${now - hour / 10},"status":"InProgress","description":"Starting deletion process"}]}]},
          {"name":"p3","createdAt":${now - 2 * hour},"endedAt":${now - hour},"mode":"full","state":"Failed","execute":true,"currency":"USD",
           "beforeMonthly":318.35,"afterMonthly":177.54,"missedMonthly":98.0,"failureReason":"Timeout","failurePhase":"Deletion","message":"timed out",
           "removing":[{"name":"edge-aaaa","status":"failed"},{"name":"edge-bbbb","status":"success"}],
           "adding":[{"name":"cast-0","status":"success","instanceType":"c7g.2xlarge","spot":true,"priceHourly":0.1608}],
           "budgets":[{"nodePool":"edge","allowed":1,"disrupting":0,"nodes":5}]},
          {"name":"p2","createdAt":${now - 3 * hour},"mode":"full","state":"Done","execute":true,"currency":"USD",
           "beforeMonthly":128.19,"afterMonthly":76.36,"achievedMonthly":51.83},
          {"name":"p1","createdAt":${now - 4 * hour},"mode":"delete-empty","state":"Done","execute":true,"currency":"USD",
           "beforeMonthly":50,"afterMonthly":0},
          {"name":"p0","createdAt":${now - 30 * hour},"mode":"full","state":"Done","execute":true,"currency":"USD","beforeMonthly":999},
          {"name":"waiting","createdAt":${now - hour},"state":"Created","execute":false,"mode":"weird"}]}}
    """.trimIndent()

    private val status = TalosJson.decodeFromString(DataServices.serializer(), json).castai!!

    @Test
    fun decodesPlans() {
        val failed = status.plans[1]
        assertEquals(CastAIPlanState.FAILED, failed.planState)
        assertEquals(CastAIPlanMode.FULL, failed.planMode)
        assertEquals(1, failed.removed)
        assertEquals(1, failed.added)
        assertEquals(CastAINodeStatus.FAILED, failed.removing.first().nodeStatus)
        assertEquals(140.81, failed.plannedMonthly, 0.001)
        assertNull(failed.savedMonthly)
        assertEquals(CastAIPlanState.AWAITING_APPROVAL, status.plans.last().planState)
        assertEquals(CastAIPlanMode.OTHER, status.plans.last().planMode)
    }

    @Test
    fun savedIsMeasuredWhenKnownElsePlanned() {
        assertEquals(51.83, status.plans[2].savedMonthly!!, 0.001)
        assertEquals(50.0, status.plans[3].savedMonthly!!, 0.001)
    }

    @Test
    fun summarizesTheLastDay() {
        val s = status.planSummary(now)
        // p0 is older than a day; the waiting plan counts as other.
        assertEquals(101.83, s.savedMonthly, 0.001)
        // p1 was not measured: the saved total is partly planned.
        assertTrue(s.savedEstimated)
        // Only the nodes the failed plan left, not its whole planned saving.
        assertEquals(98.0, s.missedMonthly, 0.001)
        assertFalse(s.missedEstimated)
        assertEquals(listOf(2, 1, 1, 1), listOf(s.done, s.failed, s.running, s.other))
        assertEquals("USD", s.currency)
        assertEquals(38, s.clusterNodes)
    }

    @Test
    fun groupsWaitingAndRunningFirst() {
        assertEquals(
            listOf(CastAIPlanState.AWAITING_APPROVAL, CastAIPlanState.RUNNING, CastAIPlanState.FAILED, CastAIPlanState.DONE),
            status.planGroups().map { it.first },
        )
        assertEquals(listOf("p2", "p1", "p0"), status.planGroups().last().second.map { it.name })
    }

    @Test
    fun aStuckNodeMakesAWarningNotAnItem() {
        val summary = status.summary()
        assertEquals(0, summary.total)
        assertEquals(0, summary.attention)
        assertEquals(ServiceHealth.WARNING, summary.health)
    }

    @Test
    fun plansErrorIsKeptApart() {
        val s = TalosJson.decodeFromString(DataServices.serializer(), """{"castai":{"plansError":"forbidden"}}""").castai!!
        assertEquals("forbidden", s.plansError)
        assertEquals("", s.error)
        assertEquals(ServiceHealth.OK, s.summary().health)
    }
}
