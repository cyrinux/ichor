package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeJobsTest {

    private val json = """{"jobs":[
        {"namespace":"ops","name":"backup-manual-abcde","state":"failed","owner":"backup","manual":true,
         "started":1760000000000,"finished":1760000300000,"duration":300000,"completions":"0/1",
         "reason":"BackoffLimitExceeded","level":"critical"},
        {"namespace":"ops","name":"import","state":"suspended","completions":"0/1","level":"warning"},
        {"namespace":"ops","name":"migrate","state":"running","started":1760000000000,"duration":600000,"completions":"1/3","level":"newer-level"}]}"""

    private val jobs = TalosJson.decodeFromString(KubeJobs.serializer(), json).jobs

    @Test
    fun decodesTheCoreJson() {
        val failed = jobs.first()
        assertEquals(StorageLevel.CRITICAL, failed.level)
        assertEquals(JobRunState.FAILED, failed.runState)
        assertEquals(300L, failed.durationSeconds)
        assertEquals(KubeObjectRef.cronJob("ops", "backup"), failed.ownerRef)
        assertEquals("jobs", failed.ref.resource)
        assertTrue(failed.ref.isJob)
        // A suspended Job has no run state of the CronJobs screen, nor a duration.
        val held = jobs[1]
        assertTrue(held.suspended)
        assertNull(held.runState)
        assertNull(held.durationSeconds)
        assertNull(held.ownerRef)
        // A level this version does not know reads as ok.
        assertEquals(StorageLevel.OK, jobs[2].level)
        assertFalse(jobs[2].suspended)
    }

    @Test
    fun searchesNameOwnerStateAndReason() {
        assertEquals(listOf("backup-manual-abcde"), jobs.filteredJobs("BACKOFF").map { it.name })
        assertEquals(listOf("backup-manual-abcde"), jobs.filteredJobs("backup").map { it.name })
        assertEquals(listOf("import"), jobs.filteredJobs("suspended").map { it.name })
        assertEquals(listOf("migrate"), jobs.filteredJobs("ops/mig").map { it.name })
        assertEquals(3, jobs.filteredJobs(" ").size)
    }
}
