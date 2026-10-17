package name.levis.ichor.monitor

import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Test

class MonitorOrderTest {

    private val contexts = (1..23).map { ContextSummary(name = "lab-$it", fingerprint = "f$it", clusterId = "c$it") }

    private fun names(active: String, foreground: Boolean, lastFailed: Set<String> = emptySet()) =
        monitoredOrder(contexts, active, foreground, lastFailed).map { it.name }

    @Test
    fun keepsTheOrderWhenNothingFailedInTheBackground() {
        assertEquals(contexts.map { it.name }, names(active = "lab-1", foreground = false))
    }

    @Test
    fun theActiveClusterLastInTheForeground() {
        val order = names(active = "lab-1", foreground = true)
        assertEquals("lab-1", order.last())
        assertEquals(contexts.drop(1).map { it.name }, order.dropLast(1))
    }

    @Test
    fun lastFailedFirstInTheirOwnOrder() {
        val order = names(active = "lab-1", foreground = false, lastFailed = setOf("f20", "f5"))
        assertEquals(listOf("lab-5", "lab-20", "lab-1"), order.take(3))
        assertEquals(23, order.size)
    }

    @Test
    fun theActiveClusterLastEvenWhenItFailed() {
        val order = names(active = "lab-2", foreground = true, lastFailed = setOf("f2", "f9"))
        assertEquals("lab-9", order.first())
        assertEquals("lab-2", order.last())
    }

    @Test
    fun aFailedClusterIsInTheFirstBatch() {
        val order = names(active = "lab-1", foreground = true, lastFailed = setOf("f23"))
        assertEquals(true, "lab-23" in order.take(MONITOR_CLUSTER_PARALLELISM))
    }

    @Test
    fun lastFailedAreTheClustersWithFailures() {
        val state = MonitorState(emptyMap(), reach = mapOf("f1" to Reach(failures = 2), "f2" to Reach()))
        assertEquals(setOf("f1"), lastFailedKeys(state))
    }

    @Test
    fun aCheckAskedForWhileTheHomeLoadsWaits() {
        assertEquals(MONITOR_RUN_NOW_DELAY_MS, runNowDelayMs(foreground = true, overviewLoaded = false))
        assertEquals(0L, runNowDelayMs(foreground = true, overviewLoaded = true))
        assertEquals(0L, runNowDelayMs(foreground = false, overviewLoaded = false))
    }
}
