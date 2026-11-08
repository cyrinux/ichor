package name.levis.ichor.security

import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AppLockTest {

    private class MemoryStore(
        override var lockEnabled: Boolean = false,
        override var securityKeys: SecurityKeyEnrolment? = null,
        override var panicTriggerPackage: String? = null,
        override var panicResponse: PanicResponse = PanicResponse.LOCK,
    ) : LockSettings

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

    @Test
    fun securityKeysArePersistedAndRequireOnlyWithKeys() {
        val store = MemoryStore(lockEnabled = true)
        val lock = AppLock(store, clock = { now })
        assertFalse(lock.requiresKey)
        assertFalse(lock.sealing())

        val key = EnrolledKey(credentialId = "AQID", publicKey = "AAAA", label = "Security key", enrolledAt = 1)
        lock.setSecurityKeys(SecurityKeyEnrolment.create().withKey(key))
        assertEquals(1, store.securityKeys?.keys?.size)
        assertFalse("UNLOCK mode: fingerprint still opens the app", lock.requiresKey)

        lock.setSecurityKeys(store.securityKeys!!.copy(mode = SecurityKeyMode.REQUIRED))
        assertTrue(lock.requiresKey)
        assertTrue(lock.sealing())
        assertNull("no key tapped yet", lock.dek())
        lock.provideDek(ByteArray(32) { 7 })
        assertEquals(7.toByte(), lock.dek()?.first())

        // Removing the last key drops the requirement and the data key with it.
        lock.setSecurityKeys(store.securityKeys!!.without(key.credentialId))
        assertNull(store.securityKeys)
        assertFalse(lock.requiresKey)
        assertNull(lock.dek())
    }

    @Test
    fun turningTheLockOffForgetsTheKeys() {
        val store = MemoryStore(lockEnabled = true, securityKeys = SecurityKeyEnrolment.create().withKey(EnrolledKey("AQID", "AAAA", "Security key", 1)))
        val lock = AppLock(store, clock = { now })
        assertEquals(1, lock.securityKeys.value?.keys?.size)
        lock.setEnabled(false)
        assertNull(store.securityKeys)
        assertNull(lock.securityKeys.value)
    }

    @Test
    fun aPanicLockClosesTheAppAtOnce() {
        val lock = lock(enabled = true).apply { unlock() }
        lock.lock()
        assertTrue(lock.locked.value)
        assertTrue("the app stays loaded under the lock", lock.everUnlocked.value)
    }

    @Test
    fun aPanicLockDoesNothingWithoutTheAppLock() {
        // No lock screen could open it again (a demo-only install).
        val lock = lock(enabled = false)
        lock.lock()
        assertFalse(lock.locked.value)
    }

    @Test
    fun aPanicLockDropsTheDataKeyWhenAKeyIsRequired() {
        val key = EnrolledKey("AQID", "AAAA", "Security key", 1)
        val required = SecurityKeyEnrolment.create().withKey(key).copy(mode = SecurityKeyMode.REQUIRED)
        val lock = AppLock(MemoryStore(lockEnabled = true, securityKeys = required), clock = { now })
        lock.provideDek(ByteArray(32) { 7 })
        lock.lock()
        assertNull("the sealed config needs the key again", lock.dek())

        val optional = AppLock(MemoryStore(lockEnabled = true), clock = { now })
        optional.provideDek(ByteArray(32) { 7 })
        optional.lock()
        assertEquals(7.toByte(), optional.dek()?.first())
    }

    @Test
    fun thePanicConnectionIsPersisted() {
        val store = MemoryStore(lockEnabled = true)
        val lock = AppLock(store, clock = { now })
        assertNull(lock.panicTriggerPackage.value)
        assertEquals(PanicResponse.LOCK, lock.panicResponse.value)

        lock.setPanicConnection("org.example.panic", PanicResponse.DELETE_CONFIG)
        assertEquals("org.example.panic", store.panicTriggerPackage)
        assertEquals(PanicResponse.DELETE_CONFIG, store.panicResponse)
        assertEquals("org.example.panic", lock.panicTriggerPackage.value)
        assertEquals(PanicResponse.DELETE_CONFIG, lock.panicResponse.value)

        lock.setPanicConnection(null, PanicResponse.LOCK)
        assertNull(store.panicTriggerPackage)
        assertEquals(PanicResponse.LOCK, lock.panicResponse.value)
    }

    @Test
    fun requiredOnceARealClusterIsStored() {
        val real = ContextSummary(name = "prod")
        val demo = ContextSummary(name = "demo", demo = true)
        assertFalse("nothing imported yet", lockRequired(emptyList()))
        assertFalse("the demo holds no credentials", lockRequired(listOf(demo)))
        assertTrue(lockRequired(listOf(real)))
        assertTrue(lockRequired(listOf(demo, real)))
    }
}
