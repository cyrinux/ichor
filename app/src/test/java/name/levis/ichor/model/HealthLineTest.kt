package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class HealthLineTest {

    private val unmet = "waiting for etcd to be healthy: 10.0.0.3: service is not healthy: etcd"

    @Test
    fun passedConditionIsOk() {
        assertEquals(HealthLineStatus.OK, healthLineStatus("waiting for etcd to be healthy: OK"))
        assertEquals(HealthLineStatus.OK, healthLineStatus(" waiting for all k8s nodes to report ready: OK \n"))
    }

    @Test
    fun unevaluatedConditionIsPending() {
        assertEquals(HealthLineStatus.PENDING, healthLineStatus("waiting for etcd to be healthy: ..."))
        assertEquals(HealthLineStatus.PENDING, healthLineStatus("waiting for kubelet"))
    }

    @Test
    fun unmetConditionIsWarn() {
        assertEquals(HealthLineStatus.WARN, healthLineStatus(unmet))
    }

    @Test
    fun lastLineOfFailedRunIsBad() {
        assertEquals(HealthLineStatus.BAD, healthLineStatus(unmet, failed = true))
        assertEquals(HealthLineStatus.BAD, healthLineStatus("waiting for etcd to be healthy: ...", failed = true))
    }

    @Test
    fun failedRunKeepsPassedAndInfoLines() {
        assertEquals(HealthLineStatus.OK, healthLineStatus("waiting for etcd to be healthy: OK", failed = true))
        assertEquals(HealthLineStatus.INFO, healthLineStatus("discovered nodes: [\"10.0.0.1\"]", failed = true))
    }

    @Test
    fun otherLinesAreInfo() {
        assertEquals(HealthLineStatus.INFO, healthLineStatus("discovered nodes: [\"10.0.0.1\" \"10.0.0.2\"]"))
        assertEquals(HealthLineStatus.INFO, healthLineStatus(""))
    }
}
