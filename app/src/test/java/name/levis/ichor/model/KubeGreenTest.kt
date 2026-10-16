package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeGreenTest {
    private val json = """
        {"kubeGreen":{"version":"v0.7.1","error":"",
         "sleepInfos":[
          {"namespace":"staging","name":"nights","weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Europe/Rome",
           "suspendDeployments":true,"suspendStatefulSets":true,"suspendCronJobs":false,
           "excludeCount":1,"includeCount":0,"patchCount":0,
           "operation":"SLEEP","lastScheduleAt":1000,"nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":true,"health":"ok","reasons":[]},
          {"namespace":"sandbox","name":"until-changed","weekdays":"1-5","sleepAt":"19:00","wakeUpAt":"",
           "operation":"SLEEP","lastScheduleAt":1000,"nextSleepAt":2000,"nextWakeUpAt":0,"asleep":true,
           "health":"warning","reasons":["noWakeUp"]},
          {"namespace":"staging","name":"broken","weekdays":"1-5","sleepAt":"25:00","wakeUpAt":"07:00",
           "operation":"","lastScheduleAt":0,"nextSleepAt":0,"nextWakeUpAt":0,"asleep":false,
           "health":"warning","reasons":["invalidSchedule"]},
          {"namespace":"preview","name":"fresh","weekdays":"*","sleepAt":"22:30","wakeUpAt":"06:00",
           "operation":"","lastScheduleAt":0,"nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":false,
           "health":"idle","reasons":["neverRan"]}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesSleepInfos() {
        val k = services.kubeGreen!!
        assertEquals("v0.7.1", k.version)
        assertEquals(4, k.sleepInfos.size)
        val nights = k.sleepInfos[0]
        assertTrue(nights.asleep)
        assertEquals(listOf(KubeGreenReason.NO_WAKE_UP), k.sleepInfos[1].reasonList)
        assertEquals(0, k.sleepInfos[1].nextWakeUpAt)
        assertEquals(listOf(KubeGreenReason.INVALID_SCHEDULE), k.sleepInfos[2].reasonList)
        assertEquals(ServiceHealth.IDLE, k.sleepInfos[3].serviceHealth)
        assertEquals(listOf(DataServiceKind.KUBE_GREEN), services.detected)
    }

    @Test
    fun summaryCountsSchedules() {
        // 4 schedules, the two warnings need a look.
        assertEquals(ServiceSummary(total = 4, attention = 2, health = ServiceHealth.WARNING), services.summary(DataServiceKind.KUBE_GREEN))
    }

    // What a workload or CronJob row reads: one namespace, asleep.
    private val namespace = TalosJson.decodeFromString(
        KubeGreenNamespace.serializer(),
        """
        {"installed":true,"asleep":true,
         "sleepInfos":[
          {"namespace":"staging","name":"nights","suspendDeployments":true,"suspendStatefulSets":true,"suspendCronJobs":true,
           "nextSleepAt":2000,"nextWakeUpAt":3000,"asleep":true},
          {"namespace":"staging","name":"weekend","suspendCronJobs":true,"nextSleepAt":9000,"asleep":false}],
         "sleepsDeployments":true,"sleepsStatefulSets":true,"sleepsCronJobs":true,
         "nextWakeUpAt":3000,"nextSleepAt":2000,"filtered":false}
        """.trimIndent(),
    )

    @Test
    fun namespaceSaysWhatItSleeps() {
        assertTrue(namespace.installed)
        assertTrue(namespace.asleep)
        assertEquals("staging/nights", namespace.asleepInfo?.name?.let { "staging/$it" })
        assertTrue(namespace.sleeps("Deployment"))
        assertTrue(namespace.sleeps("StatefulSet"))
        assertTrue(namespace.sleeps("CronJob"))
        // A kind kube-green never suspends.
        assertEquals(false, namespace.sleeps("DaemonSet"))
    }

    @Test
    fun warnsBeforeAChangeItWouldUndo() {
        // Asleep now: warn whatever the clock says.
        assertEquals(KubeGreenWarn.ASLEEP, namespace.warnBeforeChange(now = 1))
        // Awake but sleeping within a day.
        val soon = namespace.copy(asleep = false, nextSleepAt = 5_000)
        assertEquals(KubeGreenWarn.SLEEPS_SOON, soon.warnBeforeChange(now = 1))
        // Awake and sleeping only much later: nothing to say.
        val far = namespace.copy(asleep = false, nextSleepAt = 500_000_000)
        assertEquals(null, far.warnBeforeChange(now = 1))
        // No schedule at all.
        assertEquals(null, KubeGreenNamespace(installed = true).warnBeforeChange(now = 1))
    }

    @Test
    fun scheduleEditorReadsAndWritesWeekdays() {
        assertEquals((0..6).toSet(), kubeGreenDays("*"))
        assertEquals((1..5).toSet(), kubeGreenDays("1-5"))
        assertEquals(setOf(0, 6), kubeGreenDays("0,6"))
        assertEquals(setOf(0, 5, 6), kubeGreenDays("5-7"))
        // Steps and names are edited as text.
        assertEquals(null, kubeGreenDays("*/2"))
        assertEquals(null, kubeGreenDays("MON"))

        assertEquals("*", kubeGreenWeekdays((0..6).toSet()))
        assertEquals("1-5", kubeGreenWeekdays((1..5).toSet()))
        assertEquals("0,6", kubeGreenWeekdays(setOf(6, 0)))
        assertEquals("1,3-4", kubeGreenWeekdays(setOf(1, 3, 4)))
    }

    @Test
    fun scheduleEditorRefusesWhatTheCoreRefuses() {
        assertTrue(kubeGreenTimeValid("20:00"))
        assertTrue(kubeGreenTimeValid("*/2:30"))
        assertFalse(kubeGreenTimeValid("25:00"))
        assertFalse(kubeGreenTimeValid("20:61"))
        assertFalse(kubeGreenTimeValid("20h"))
        assertTrue(kubeGreenWeekdaysValid("1-5"))
        assertFalse(kubeGreenWeekdaysValid("mon-fri"))
        assertTrue(kubeGreenZoneValid("Europe/Paris"))
        assertTrue(kubeGreenZoneValid(""))
        assertFalse(kubeGreenZoneValid("Mars/Olympus"))
    }
}
