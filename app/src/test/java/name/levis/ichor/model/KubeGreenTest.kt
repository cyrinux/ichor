package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
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
}
