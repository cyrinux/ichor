package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertmanagerTest {
    private val firing = AmAlert(
        fingerprint = "f1",
        alertname = "KubePodCrashLooping",
        severity = AmSeverity.WARNING,
        summary = "Pod is crash looping.",
        labels = mapOf("alertname" to "KubePodCrashLooping", "namespace" to "shop", "pod" to "api-7d9f", "instance" to "10.0.0.7:8080"),
    )
    private val silenced = AmAlert(fingerprint = "f2", alertname = "CPUThrottlingHigh", severity = AmSeverity.INFO, state = AmState.SUPPRESSED, silencedBy = listOf("s1"))
    private val inhibited = AmAlert(fingerprint = "f3", alertname = "NodeDown", severity = AmSeverity.CRITICAL, state = AmState.SUPPRESSED, inhibitedBy = listOf("f9"))

    @Test
    fun theDefaultFilterShowsWhatFires() {
        val filter = AmFilter()
        assertTrue(filter.matches(firing))
        assertFalse(filter.matches(silenced))
        assertFalse(filter.matches(inhibited))
        // Unprocessed counts as firing.
        assertTrue(filter.matches(firing.copy(state = AmState.UNPROCESSED)))
    }

    @Test
    fun statesSeveritiesAndTextNarrow() {
        assertTrue(AmFilter(active = false, silenced = true).matches(silenced))
        assertFalse(AmFilter(active = false, silenced = true).matches(inhibited))
        assertTrue(AmFilter(inhibited = true).matches(inhibited))
        assertFalse(AmFilter(severities = setOf(AmSeverity.CRITICAL)).matches(firing))
        assertTrue(AmFilter(query = "crash").matches(firing))
        assertTrue(AmFilter(query = "api-7d").matches(firing))
        assertFalse(AmFilter(query = "postgres").matches(firing))
    }

    @Test
    fun filteredDropsEmptyGroups() {
        val alerts = AmAlerts(
            groups = listOf(
                AmGroup("KubePodCrashLooping", AmSeverity.WARNING, 1, 1, listOf(firing)),
                AmGroup("CPUThrottlingHigh", AmSeverity.INFO, 1, 0, listOf(silenced)),
            ),
        )
        assertEquals(listOf("KubePodCrashLooping"), alerts.filtered(AmFilter()).map { it.alertname })
    }

    @Test
    fun objectLinksFromLabels() {
        assertEquals(AmObjectLinks(pod = "api-7d9f", namespace = "shop", node = "10.0.0.7"), firing.objectLinks(setOf("10.0.0.7", "worker-1")))
        // A node only when the cluster has one by that name; a pod only with its namespace.
        assertEquals(AmObjectLinks(), AmAlert(labels = mapOf("pod" to "x", "instance" to "db.example.net:9100")).objectLinks(setOf("worker-1")))
        assertEquals("worker-1", AmAlert(labels = mapOf("node" to "worker-1")).objectLinks(setOf("worker-1")).node)
        assertEquals("fd00::5", AmAlert(labels = mapOf("instance" to "[fd00::5]:9100")).objectLinks(setOf("fd00::5")).node)
    }

    @Test
    fun whereIsShort() {
        assertEquals("shop/api-7d9f", firing.where)
        assertEquals("worker-1", AmAlert(labels = mapOf("node" to "worker-1", "namespace" to "monitoring")).where)
        assertEquals("monitoring", AmAlert(labels = mapOf("namespace" to "monitoring")).where)
        assertEquals("", AmAlert().where)
    }

    @Test
    fun matcherText() {
        assertEquals("alertname=\"X\"", AmMatcher("alertname", "X").text)
        assertEquals("pod=~\"api-.*\"", AmMatcher("pod", "api-.*", isRegex = true).text)
        assertEquals("job!=\"a\"", AmMatcher("job", "a", isEqual = false).text)
        assertEquals("job!~\"a.*\"", AmMatcher("job", "a.*", isRegex = true, isEqual = false).text)
    }

    @Test
    fun decodesTheGoShape() {
        val json = """{"groups":[{"alertname":"Watchdog","severity":"info","count":1,"active":1,"alerts":[
            {"fingerprint":"d4","alertname":"Watchdog","severity":"info","labels":{"alertname":"Watchdog"},"annotations":{},
             "startsAt":1791540000000,"endsAt":0,"updatedAt":0,"receivers":["null"],"state":"active","silencedBy":[],"inhibitedBy":[]}]}],
            "counts":{"critical":0,"warning":0,"info":1,"other":0,"suppressed":2},"total":3,"truncated":false}"""
        val alerts = TalosJson.decodeFromString(AmAlerts.serializer(), json)
        assertEquals(1, alerts.counts.firing)
        assertEquals(2, alerts.counts.suppressed)
        assertEquals("Watchdog", alerts.groups.single().alerts.single().alertname)
    }
}
