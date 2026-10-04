package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CronJobsTest {

    private val jobs = listOf(
        KubeCronJob("shop", "db-backup", title = "Database backup", state = "succeeded", schedule = "0 3 * * *", images = listOf("postgres:17")),
        KubeCronJob("shop", "mailer", state = "failed", description = "Weekly report"),
        KubeCronJob("infra", "renew", state = "running"),
        KubeCronJob("a", "never-ran", state = "never"),
    )

    @Test
    fun decodesTheGoJson() {
        val json = """{"cronJobs":[{"namespace":"shop","name":"db-backup","title":"Database backup","icon":"postgresql",""" +
            """"schedule":"0 3 * * *","suspended":false,"triggerable":false,"active":0,"state":"failed","lastSchedule":1,""" +
            """"lastSuccess":0,"nextRun":2,"created":0,"images":["postgres:17"],""" +
            """"runs":[{"name":"db-backup-manual-x","state":"failed","manual":true,"started":1000,"finished":95000}]}]}"""
        val c = TalosJson.decodeFromString(KubeCronJobList.serializer(), json).cronJobs.single()
        assertEquals("shop/db-backup", c.key)
        assertEquals("Database backup", c.displayName)
        assertEquals(JobRunState.FAILED, c.runState)
        assertFalse(c.triggerable)
        assertTrue(c.hasIcon)
        val run = c.runs.single()
        assertTrue(run.manual)
        assertEquals(94_000L, run.durationMillis)
    }

    @Test
    fun runningAndFailedFirst() {
        assertEquals(listOf("renew", "mailer", "never-ran", "db-backup"), jobs.filteredCronJobs(null, "").map { it.name })
    }

    @Test
    fun filtersByNamespaceTitleDescriptionScheduleOrImage() {
        assertEquals(listOf("mailer", "db-backup"), jobs.filteredCronJobs("shop", "").map { it.name })
        assertEquals(listOf("db-backup"), jobs.filteredCronJobs(null, "DATABASE").map { it.name })
        assertEquals(listOf("mailer"), jobs.filteredCronJobs(null, "weekly").map { it.name })
        assertEquals(listOf("db-backup"), jobs.filteredCronJobs(null, "0 3").map { it.name })
        assertEquals(listOf("db-backup"), jobs.filteredCronJobs(null, "postgres").map { it.name })
        assertEquals(listOf("a", "infra", "shop"), jobs.cronNamespaces)
    }

    @Test
    fun noIconUsesTheDefault() {
        val c = KubeCronJob("a", "b")
        assertFalse(c.hasIcon)
        assertEquals("b", c.displayName)
        assertFalse(KubeCronJob("a", "b", remoteIcon = "../x").hasIcon)
    }

    @Test
    fun noDurationWhileRunning() {
        assertNull(KubeJobRun("r", started = 1000).durationMillis)
        assertNull(KubeJobRun("r", started = 5000, finished = 1000).durationMillis)
    }
}
