package name.levis.ichor.model

import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ReadProgressTest {
    @Test fun decodesGoJson() {
        val progress = TalosJson.decodeFromString(
            ReadProgress.serializer(),
            """{"running":true,"done":5,"total":17,"steps":[{"id":"api","state":"done","ms":120},{"id":"storage","state":"running","count":12,"total":40},{"id":"helm","state":"pending"}]}""",
        )
        assertTrue(progress.running)
        assertEquals(5f / 17, progress.fraction!!, 0.0001f)
        assertEquals(ReadStep("storage", ReadStep.RUNNING, count = 12, total = 40), progress.steps[1])
        assertEquals(120L, progress.steps[0].ms)
    }

    @Test fun noStepsNoFraction() {
        val idle = TalosJson.decodeFromString(ReadProgress.serializer(), """{"running":false,"done":0,"total":0,"steps":[]}""")
        assertNull(idle.fraction)
    }

    @Test fun pollSkipsIdleProgressAndStopsWhenCancelled() = runBlocking {
        var reads = 0
        val seen = mutableListOf<ReadProgress>()
        val poll = launch {
            pollReadProgress(
                read = {
                    reads++
                    when (reads) {
                        1 -> ReadProgress(running = false) // the read has not started its count yet
                        2 -> error("not available")
                        else -> ReadProgress(running = true, done = reads, total = 17)
                    }
                },
                emit = { seen += it },
                intervalMs = 5,
            )
        }
        delay(60)
        poll.cancel()
        poll.join()
        val after = seen.size
        delay(30)
        assertTrue("no running progress emitted", after > 0)
        assertTrue(seen.all { it.running })
        assertEquals("emitted after cancel", after, seen.size)
    }
}
