package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ApiHealthTest {

    // A KubeAPIHealth answer (go/ichorgo/kube_apihealth.go), cut down from the demo's.
    private val report = TalosJson.decodeFromString(
        ApiHealthReport.serializer(),
        """
            {"status":"busy","version":"v1.34.1",
             "ready":{"ok":true,"checks":[{"name":"ping","ok":true},{"name":"etcd","ok":true}]},
             "live":{"ok":false,"checks":[{"name":"ping","ok":true},{"name":"etcd","reason":"reason withheld"}]},
             "uptimeSeconds":950400,"windowSeconds":5,"requestRate":142.6,"errorRate":0.2,"throttledRate":0,"rejectedRate":0,
             "inflightRead":18,"inflightMutate":3,"queued":4,"watches":612,"watchEventRate":88.4,"etcdLatencyMs":6.8,
             "clients":[{"name":"service-accounts","priority":"workload-low","rate":61.2,"rejectedRate":0,"queued":4,"waitMs":140}],
             "priorities":[{"name":"workload-low","executing":21,"limit":24,"queued":4,"rejectedRate":0},{"name":"exempt","executing":2,"limit":0,"queued":0,"rejectedRate":0}],
             "requests":[{"verb":"LIST","resource":"pods","rate":48.6,"errorRate":0,"latencyMs":412},{"verb":"GET","resource":"","rate":4.2,"errorRate":0,"latencyMs":0.6}],
             "watchedKinds":[{"resource":"pods","count":148}],
             "objects":[{"resource":"events","count":4210}],
             "queuedRequests":[{"user":"system:serviceaccount:monitoring:pod-exporter","flowSchema":"service-accounts","priority":"workload-low","verb":"list","path":"/api/v1/pods"}],
             "futureField":true}
        """.trimIndent(),
    )

    @Test
    fun decodesTheReport() {
        assertEquals(ApiStatus.BUSY, report.verdict)
        assertTrue(report.ratesAreLive)
        assertEquals("service-accounts", report.clients.single().name)
        assertEquals(1, report.queuedRequests.size)
        assertEquals("", report.requests[1].resource)
        assertEquals("", report.metricsError)
    }

    @Test
    fun failedChecksAreListedOnce() {
        assertEquals(listOf(ApiCheck("etcd", ok = false, reason = "reason withheld")), report.failedChecks)
    }

    @Test
    fun priorityShareIsNullWhenExempt() {
        assertEquals(0.875f, report.priorities[0].share!!, 0.0001f)
        assertNull(report.priorities[1].share)
    }

    @Test
    fun unknownStatusAndMissingFieldsDefault() {
        val bare = TalosJson.decodeFromString(ApiHealthReport.serializer(), """{"status":"new","metricsError":"forbidden"}""")
        assertEquals(ApiStatus.OK, bare.verdict)
        assertFalse(bare.ratesAreLive)
        assertTrue(bare.clients.isEmpty())
    }

    @Test
    fun formatsRatesAndDurations() {
        assertEquals("142/s", formatRate(142.4))
        assertEquals("3.6/s", formatRate(3.6))
        assertEquals("0.02/s", formatRate(0.0201))
        assertEquals("0/s", formatRate(0.0))
        assertEquals("412 ms", formatMs(412.2))
        assertEquals("6.8 ms", formatMs(6.8))
    }
}
