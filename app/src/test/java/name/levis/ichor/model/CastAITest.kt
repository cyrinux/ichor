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

    @Test
    fun decodesRecommendations() {
        val c = services.castai!!
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
    fun absentSectionIsNotDetected() {
        val none = TalosJson.decodeFromString(DataServices.serializer(), "{}")
        assertNull(none.castai)
        assertTrue(DataServiceKind.CASTAI !in none.detected)
    }
}
