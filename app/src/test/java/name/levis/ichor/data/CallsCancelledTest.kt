package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import name.levis.ichor.ui.surviveClusterSwitch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class CallsCancelledTest {
    // The exact text of errCallsCancelled (go/ichorgo/callscope.go, pinned by a test there).
    private val left = Exception("cancelled: the app left this cluster")

    @Test
    fun recognisesTheCoreText() {
        assertTrue(left.isCallsCancelled())
        assertTrue(Exception("read pods: cancelled: the app left this cluster").isCallsCancelled())
        assertFalse(Exception("context canceled").isCallsCancelled())
    }

    @Test
    fun aReadOfTheClusterOnScreenEndsQuietly() {
        assertThrows(CallsCancelledException::class.java) { quietWhenLeft<Unit>(pinned = null) { throw left } }
    }

    @Test
    fun theMonitorKeepsTheErrorToSkipTheCluster() {
        val thrown = assertThrows(Exception::class.java) { quietWhenLeft<Unit>(pinned = "other") { throw left } }
        assertEquals(left, thrown)
    }

    @Test
    fun anyOtherErrorIsLeftAlone() {
        val other = IllegalStateException("refused")
        val thrown = assertThrows(IllegalStateException::class.java) { quietWhenLeft<Unit>(pinned = null) { throw other } }
        assertEquals(other, thrown)
    }

    @Test
    fun aPollingLoopOutlivesTheCut() = runBlocking {
        var runs = 0
        surviveClusterSwitch {
            runs++
            if (runs == 1) throw CallsCancelledException()
        }
        assertEquals(2, runs)
    }
}
