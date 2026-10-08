package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CastAITest {
    private val json = """
        {"castai":{"version":"v1","error":"","compared":2,"cpuDeltaMilli":-430,"memoryDeltaBytes":-268435456,
         "recommendations":[
          {"namespace":"shop","name":"worker-statefulset","kind":"StatefulSet","workload":"worker","mode":"immediate",
           "health":"critical","reasons":["vpa"],"message":"webhook unreachable",
           "containers":[{"name":"worker","cpu":"2","memory":"3Gi"}]},
          {"namespace":"shop","name":"report-cronjob","kind":"CronJob","workload":"report","mode":"deferred","readOnly":true,
           "health":"warning","reasons":["readOnly","later"],"message":"managed by another autoscaler",
           "containers":[{"name":"report","cpu":"50m","memory":"256Mi","originalCpu":"100m","originalMemory":"128Mi"}],
           "cpuDeltaMilli":-50,"memoryDeltaBytes":134217728},
          {"namespace":"shop","name":"api-deployment","kind":"Deployment","workload":"api","mode":"deferred","health":"ok","reasons":[],
           "containers":[{"name":"api","cpu":"120m","memory":"640Mi","cpuLimit":"1","memoryLimit":"1Gi","originalCpu":"500m","originalMemory":"1Gi"}],
           "cpuDeltaMilli":-380,"memoryDeltaBytes":-402653184}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)
    private val castai = services.castai!!

    @Test
    fun decodesRecommendations() {
        val c = castai
        assertEquals(3, c.recommendations.size)
        assertEquals(-430L, c.cpuDeltaMilli)
        val api = c.recommendations.last()
        assertEquals("shop/api", api.label)
        assertEquals(CastAIMode.DEFERRED, api.applyMode)
        assertEquals("500m", api.containers.single().originalCpu)
        // An unknown reason from a newer core is dropped, the known one kept.
        assertEquals(listOf(CastAIReason.READ_ONLY), c.recommendations[1].reasonList)
        assertEquals(CastAIMode.IMMEDIATE, c.recommendations.first().applyMode)
    }

    @Test
    fun summaryCountsProblems() {
        val summary = services.summary(DataServiceKind.CASTAI)!!
        assertEquals(3, summary.total)
        assertEquals(2, summary.attention)
        assertEquals(ServiceHealth.CRITICAL, summary.health)
        assertTrue(DataServiceKind.CASTAI in services.detected)
    }

    @Test
    fun onlyUnappliedRecommendationsAlert() {
        val issues = dataIssuesOf(services)
        assertEquals(DATA_CRITICAL, issues["castai|shop/worker"])
        assertNull(issues["castai|shop/report"])
        assertNull(issues["castai|shop/api"])
    }

    @Test
    fun formatsMillicores() {
        assertEquals("-380m", formatMilliCores(-380, signed = true))
        assertEquals("+2", formatMilliCores(2000, signed = true))
        assertEquals("250m", formatMilliCores(250))
        assertEquals("0", formatMilliCores(0, signed = true))
    }

    @Test
    fun classifiesChanges() {
        val (worker, report, api) = castai.recommendations
        assertEquals(CastAIChange.UNKNOWN, worker.change)
        // Less CPU but more memory: it grows.
        assertEquals(CastAIChange.GROW, report.change)
        assertEquals(CastAIChange.SHRINK, api.change)
        assertEquals(CastAIChangeCounts(shrink = 1, grow = 1, same = 0, unknown = 1), castai.changeCounts())
        assertEquals(CastAIMode.DEFERRED to 2, castai.commonMode())
    }

    @Test
    fun overviewPutsProblemsThenGrowthThenSavings() {
        val sections = castai.sections(CastAIView.OVERVIEW)
        assertEquals(
            listOf(CastAISectionKind.ATTENTION, CastAISectionKind.GROWS, CastAISectionKind.REDUCTIONS, CastAISectionKind.OTHER),
            sections.map { it.kind },
        )
        assertEquals(listOf("shop/report", "shop/worker"), sections[0].rows.map { it.label })
        assertEquals(listOf("shop/report"), sections[1].rows.map { it.label })
        assertEquals(listOf("shop/api"), sections[2].rows.map { it.label })
        // The filter narrows every section; one left empty is dropped.
        assertEquals(listOf(CastAISectionKind.REDUCTIONS), castai.sections(CastAIView.OVERVIEW, "API").map { it.kind })
    }

    @Test
    fun overviewShowsTheFirstGrowersOnly() {
        val grower = { i: Int ->
            CastAIRecommendation(
                namespace = "ns", name = "w$i", workload = "w$i", cpuDeltaMilli = 100L * i,
                containers = listOf(CastAIContainer(name = "c", cpu = "1", originalCpu = "500m")),
            )
        }
        val status = CastAIStatus(recommendations = (1..8).map(grower))
        val grows = status.sections(CastAIView.OVERVIEW).single { it.kind == CastAISectionKind.GROWS }
        assertEquals(CASTAI_GROWS_PREVIEW, grows.rows.size)
        assertEquals(3, grows.hidden)
        // Biggest first.
        assertEquals("ns/w8", grows.rows.first().label)
        assertEquals(8, status.sections(CastAIView.GROWS).single().rows.size)
    }

    @Test
    fun namespacesCarryTheirTotals() {
        val two = castai.copy(
            recommendations = castai.recommendations + CastAIRecommendation(namespace = "ads", name = "x", workload = "x", cpuDeltaMilli = -10),
        )
        val sections = two.sections(CastAIView.NAMESPACES)
        assertEquals(listOf("ads", "shop"), sections.map { it.namespace })
        assertEquals(-430L, sections[1].cpuDeltaMilli)
    }

    @Test
    fun flagsARequestNearItsMemoryLimit() {
        val near = CastAIRecommendation(containers = listOf(CastAIContainer(memoryLimitPercent = 40), CastAIContainer(memoryLimitPercent = 88)))
        assertTrue(near.nearMemoryLimit)
        assertEquals(88, near.memoryLimitPercent)
        assertTrue(!CastAIRecommendation(containers = listOf(CastAIContainer(memoryLimitPercent = 60))).nearMemoryLimit)
    }

    @Test
    fun absentSectionIsNotDetected() {
        val none = TalosJson.decodeFromString(DataServices.serializer(), "{}")
        assertNull(none.castai)
        assertTrue(DataServiceKind.CASTAI !in none.detected)
    }
}
