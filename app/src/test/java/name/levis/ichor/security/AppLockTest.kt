package name.levis.ichor.security

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AppLockTest {

    private class MemoryStore(override var lockEnabled: Boolean = false) : LockSettings

    private var now = 0L
    private fun lock(enabled: Boolean) = AppLock(MemoryStore(enabled), clock = { now }, graceMillis = 30_000)

    @Test
    fun coldStartIsLockedOnlyWhenEnabled() {
        assertTrue(lock(enabled = true).locked.value)
        assertFalse(lock(enabled = false).locked.value)
    }

    @Test
    fun nothingCountsAsUnlockedBeforeTheFirstUnlock() {
        // A new process (cold start, or restored after a process death) starts from here.
        val lock = lock(enabled = true)
        assertFalse(lock.everUnlocked.value)

        lock.unlock()
        assertTrue(lock.everUnlocked.value)

        // A later relock draws over the app: it stays loaded.
        lock.onBackground()
        now += 30_000
        lock.onForeground()
        assertTrue(lock.locked.value)
        assertTrue(lock.everUnlocked.value)
    }

    @Test
    fun withoutLockTheAppLoadsAtOnce() {
        assertTrue(lock(enabled = false).everUnlocked.value)
        assertTrue(lock(enabled = true).apply { setEnabled(false) }.everUnlocked.value)
    }

    @Test
    fun relocksAfterGracePeriodInBackground() {
        val lock = lock(enabled = true).apply { unlock() }

        lock.onBackground()
        now += 29_999
        lock.onForeground()
        assertFalse("short trip (file picker, camera) must not relock", lock.locked.value)

        lock.onBackground()
        now += 30_000
        lock.onForeground()
        assertTrue(lock.locked.value)
    }

    @Test
    fun foregroundWithoutBackgroundDoesNothing() {
        val lock = lock(enabled = true).apply { unlock() }
        now += 999_999
        lock.onForeground()
        assertFalse(lock.locked.value)
    }

    @Test
    fun disabledLockNeverRelocks() {
        val lock = lock(enabled = false)
        lock.onBackground()
        now += 999_999
        lock.onForeground()
        assertFalse(lock.locked.value)
    }

    @Test
    fun enablingPersistsAndDisablingUnlocks() {
        val store = MemoryStore()
        val lock = AppLock(store, clock = { now })

        lock.setEnabled(true)
        assertTrue(store.lockEnabled)
        assertFalse("enabling from an unlocked session keeps it unlocked", lock.locked.value)

        lock.onBackground()
        now += 60_000
        lock.onForeground()
        assertTrue(lock.locked.value)

        lock.setEnabled(false)
        assertFalse(store.lockEnabled)
        assertFalse(lock.locked.value)
    }
}
