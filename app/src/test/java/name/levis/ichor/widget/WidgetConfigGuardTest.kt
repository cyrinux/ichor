package name.levis.ichor.widget

import android.appwidget.AppWidgetManager
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.security.AppLock
import name.levis.ichor.security.LockSettings
import name.levis.ichor.security.PanicResponse
import name.levis.ichor.security.SecurityKeyEnrolment
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WidgetConfigGuardTest {

    private class MemoryStore(
        override var lockEnabled: Boolean,
        override var securityKeys: SecurityKeyEnrolment? = null,
        override var panicTriggerPackage: String? = null,
        override var panicResponse: PanicResponse = PanicResponse.LOCK,
    ) : LockSettings

    private val real = listOf(ContextSummary(name = "prod"))
    private val demo = listOf(ContextSummary(name = "demo", demo = true))

    @Test
    fun onlyThisAppsPlacedWidgetsAreAccepted() {
        val placed = intArrayOf(12, 40)
        assertTrue(isPlacedWidget(12, placed))
        assertFalse("another app's or a stale id", isPlacedWidget(13, placed))
        assertFalse("no id in the intent", isPlacedWidget(AppWidgetManager.INVALID_APPWIDGET_ID, placed))
        assertFalse("no widget placed", isPlacedWidget(12, intArrayOf()))
    }

    @Test
    fun namesFollowTheLockFromColdStartToRelock() {
        var now = 0L
        val lock = AppLock(MemoryStore(lockEnabled = true), clock = { now }, graceMillis = 30_000)
        fun allowed() = clusterNamesAllowed(lock.locked.value, lock.enabled.value, real)

        assertFalse("cold start", allowed())
        lock.unlock()
        assertTrue("after authenticating", allowed())

        lock.onBackground()
        now += 30_000
        lock.onForeground()
        assertFalse("relocked after backgrounding", allowed())

        lock.unlock()
        assertTrue(allowed())
        lock.lock()
        assertFalse("a panic trigger", allowed())
    }

    @Test
    fun realClustersStoredWithoutTheLockStayHidden() {
        assertFalse(clusterNamesAllowed(locked = false, lockEnabled = false, contexts = real))
        assertTrue("the demo holds no credentials", clusterNamesAllowed(locked = false, lockEnabled = false, contexts = demo))
    }
}
