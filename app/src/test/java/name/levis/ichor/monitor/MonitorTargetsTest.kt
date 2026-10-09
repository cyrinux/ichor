package name.levis.ichor.monitor

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MonitorTargetsTest {

    private val admin = ContextSummary(name = "lab-admin", fingerprint = "fa", clusterId = "lab")
    private val reader = ContextSummary(name = "lab-reader", fingerprint = "fr", clusterId = "lab")
    private val prod = ContextSummary(name = "prod", fingerprint = "fp", clusterId = "prod")
    private val demo = ContextSummary(name = "demo", fingerprint = "fd", clusterId = "demo", demo = true)
    private val summary = ConfigSummary(current = "lab-admin", contexts = listOf(admin, reader, prod, demo))

    private fun names(active: String, unwatched: Set<String> = emptySet()) =
        monitoredContexts(summary, active, unwatched).map { it.name }

    @Test
    fun oneContextPerClusterTheFirstOne() {
        assertEquals(listOf("lab-admin", "prod"), names(active = "prod"))
    }

    @Test
    fun theActiveContextWhenItIsOfThatCluster() {
        assertEquals(listOf("lab-reader", "prod"), names(active = "lab-reader"))
    }

    @Test
    fun aClusterTurnedOffIsLeftOutWhicheverContextCarriesIt() {
        assertEquals(listOf("lab-admin"), names(active = "lab-admin", unwatched = setOf("fp")))
        assertEquals(listOf("prod"), names(active = "lab-admin", unwatched = setOf("fr")))
    }

    @Test
    fun theDemoOnlyWhileOnScreen() {
        assertEquals(listOf("lab-admin", "prod", "demo"), names(active = "demo"))
    }

    @Test
    fun contextsWithoutClusterIdAreClustersOfTheirOwn() {
        val a = ContextSummary(name = "a", fingerprint = "f1")
        val b = ContextSummary(name = "b", fingerprint = "f2")
        assertEquals(listOf("a", "b"), monitoredContexts(ConfigSummary(current = "a", contexts = listOf(a, b)), "a", emptySet()).map { it.name })
    }

    @Test
    fun aClusterSettingCoversAllItsContexts() {
        assertEquals(listOf("fa", "fr"), clusterFingerprints(summary, reader))
        assertEquals(listOf("fp"), clusterFingerprints(summary, prod))
    }

    @Test
    fun unreachableAlertsAtTheNthFailureOnce() {
        var reach: Reach? = null
        val alerts = (1..5).map {
            val step = reachStep(reach, reachable = false, runs = 3, enabled = true)
            reach = step.next
            step.alert
        }
        assertEquals(listOf(null, null), alerts.take(2))
        assertEquals(AlertKind.CLUSTER_UNREACHABLE, alerts[2]?.kind)
        assertEquals(UNREACHABLE_KEY, alerts[2]?.key)
        assertEquals("3", alerts[2]?.detail)
        assertEquals(listOf(null, null), alerts.drop(3))
    }

    @Test
    fun reachableAgainOnceThenQuiet() {
        val back = reachStep(Reach(4, notified = true), reachable = true, runs = 3, enabled = true)
        assertEquals(AlertKind.CLUSTER_REACHABLE, back.alert?.kind)
        assertFalse(back.alert!!.problem)
        assertEquals(Reach(), back.next)
        assertNull(reachStep(back.next, reachable = true, runs = 3, enabled = true).alert)
    }

    @Test
    fun anAnswerResetsTheCount() {
        val twice = reachStep(Reach(1), reachable = false, runs = 3, enabled = true).next
        val reset = reachStep(twice, reachable = true, runs = 3, enabled = true)
        assertNull(reset.alert)
        assertEquals(Reach(), reset.next)
        assertNull(reachStep(reset.next, reachable = false, runs = 3, enabled = true).alert)
    }

    @Test
    fun turnedOffItCountsWithoutAlerting() {
        val step = reachStep(Reach(5), reachable = false, runs = 3, enabled = false)
        assertNull(step.alert)
        assertEquals(Reach(6), step.next)
        // Turned on later: alerts at the next failure.
        assertTrue(reachStep(step.next, reachable = false, runs = 3, enabled = true).alert != null)
    }

    @Test
    fun notificationIdsDifferAcrossClusters() {
        assertNotEquals(alertNotificationId("fa", "node:10.0.0.2"), alertNotificationId("fp", "node:10.0.0.2"))
        assertEquals(alertNotificationId("fa", "node:10.0.0.2"), alertNotificationId("fa", "node:10.0.0.2"))
    }
}
