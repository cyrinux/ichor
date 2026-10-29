package name.levis.ichor.util

import android.app.ApplicationExitInfo
import java.io.ByteArrayInputStream
import java.io.InputStream
import name.levis.ichor.model.ExitRecord
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExitRecordsTest {

    @Test
    fun reasonsHaveStableNames() {
        val expected = mapOf(
            ApplicationExitInfo.REASON_UNKNOWN to "unknown",
            ApplicationExitInfo.REASON_EXIT_SELF to "exit_self",
            ApplicationExitInfo.REASON_SIGNALED to "signaled",
            ApplicationExitInfo.REASON_LOW_MEMORY to "low_memory",
            ApplicationExitInfo.REASON_CRASH to "crash",
            ApplicationExitInfo.REASON_CRASH_NATIVE to "crash_native",
            ApplicationExitInfo.REASON_ANR to "anr",
            ApplicationExitInfo.REASON_INITIALIZATION_FAILURE to "initialization_failure",
            ApplicationExitInfo.REASON_PERMISSION_CHANGE to "permission_change",
            ApplicationExitInfo.REASON_EXCESSIVE_RESOURCE_USAGE to "excessive_resource_usage",
            ApplicationExitInfo.REASON_USER_REQUESTED to "user_requested",
            ApplicationExitInfo.REASON_USER_STOPPED to "user_stopped",
            ApplicationExitInfo.REASON_DEPENDENCY_DIED to "dependency_died",
            ApplicationExitInfo.REASON_OTHER to "other",
            ApplicationExitInfo.REASON_FREEZER to "freezer",
            ApplicationExitInfo.REASON_PACKAGE_STATE_CHANGE to "package_state_change",
            ApplicationExitInfo.REASON_PACKAGE_UPDATED to "package_updated",
        )
        expected.forEach { (reason, name) -> assertEquals(name, ExitRecords.reasonName(reason)) }
        assertEquals("every name is distinct", expected.size, expected.values.toSet().size)
    }

    @Test
    fun aReasonFromANewerAndroidKeepsItsNumber() {
        assertEquals("reason_99", ExitRecords.reasonName(99))
    }

    @Test
    fun onlyAnrsAndNativeCrashesKeepATrace() {
        assertTrue(ExitRecords.keepsTrace(ApplicationExitInfo.REASON_ANR))
        assertTrue(ExitRecords.keepsTrace(ApplicationExitInfo.REASON_CRASH_NATIVE))
        assertFalse(ExitRecords.keepsTrace(ApplicationExitInfo.REASON_CRASH))
        assertFalse(ExitRecords.keepsTrace(ApplicationExitInfo.REASON_USER_REQUESTED))
    }

    @Test
    fun aTraceIsCappedAt64KbAndTheRestIsNeverRead() {
        val big = ByteArray(200 * 1024) { 'a'.code.toByte() }
        val stream = CountingStream(ByteArrayInputStream(big))
        val text = ExitRecords.traceText(stream, native = false)
        assertEquals(64 * 1024, ExitRecords.TRACE_CAP)
        assertEquals(ExitRecords.TRACE_CAP, text.length)
        assertTrue("read ${stream.read} bytes", stream.read <= ExitRecords.TRACE_CAP)
    }

    @Test
    fun aShortTraceIsKeptWhole() {
        val dump = "----- pid 4242 -----\n\"main\" prio=5 tid=1 Blocked\n"
        assertEquals(dump, ExitRecords.traceText(ByteArrayInputStream(dump.toByteArray()), native = false))
    }

    @Test
    fun aNativeTombstoneKeepsItsReadableRuns() {
        val bytes = byteArrayOf(0x0a, 0x12) + "abort message".toByteArray() + byteArrayOf(0x00, 0x01, 0x7f) +
            "ab".toByteArray() + byteArrayOf(0x02) + "libichorgo.so".toByteArray()
        assertEquals("abort message\nlibichorgo.so", ExitRecords.traceText(ByteArrayInputStream(bytes), native = true))
    }

    @Test
    fun onlyTheRecordsAfterTheLastLaunchAreNew() {
        val old = ExitRecord(at = 1_000, reason = "anr", status = 0, importance = 100)
        val fresh = ExitRecord(at = 2_000, reason = "user_requested", status = 0, importance = 400)
        assertEquals(listOf(fresh), ExitRecords.newerThan(listOf(fresh, old), since = 1_000))
        assertEquals(listOf(fresh, old), ExitRecords.newerThan(listOf(fresh, old), since = 0))
    }

    private class CountingStream(private val inner: InputStream) : InputStream() {
        var read = 0
        override fun read(): Int = inner.read().also { if (it >= 0) read++ }
        override fun read(b: ByteArray, off: Int, len: Int): Int = inner.read(b, off, len).also { if (it > 0) read += it }
    }
}
