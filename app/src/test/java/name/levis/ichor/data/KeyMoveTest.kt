package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import name.levis.ichor.security.SecurityKeyRequiredException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class KeyMoveTest {

    /** A store at [level] whose move goes to [movesTo], or throws [fails]; counts its moves. */
    private class Store(var level: KeyProtection?, val movesTo: KeyProtection? = level, val fails: Exception? = null) {
        var moves = 0
        var unreadable = false

        fun key() = MovableKey({ if (unreadable) throw IllegalStateException("Keystore busy") else level }) {
            moves++
            fails?.let { throw it }
            level = movesTo
            movesTo == KeyProtection.STRONGBOX
        }
    }

    private fun move(vararg stores: Store) = runBlocking { moveKeys(stores.map { it.key() }) }

    @Test
    fun bothMovedIsMoved() {
        val a = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        val b = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        assertEquals(KeyMoveResult.MOVED, move(a, b))
        assertEquals(1, a.moves)
        assertEquals(1, b.moves)
    }

    @Test
    fun aKeyAlreadyInTheChipIsNotMovedAgain() {
        val a = Store(KeyProtection.STRONGBOX)
        val b = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        assertEquals(KeyMoveResult.MOVED, move(a, b))
        assertEquals(0, a.moves)
    }

    @Test
    fun oneMovedAndOneRefusedIsPartial() {
        val a = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        val b = Store(KeyProtection.TEE, KeyProtection.TEE)
        assertEquals(KeyMoveResult.PARTIAL, move(a, b))
    }

    @Test
    fun noneMovedIsRefused() {
        assertEquals(KeyMoveResult.REFUSED, move(Store(KeyProtection.TEE), Store(KeyProtection.STRONGBOX)))
    }

    @Test
    fun aStoreWithNothingStoredCountsAsDoneAndDropsItsStrayKey() {
        val empty = Store(null)
        assertEquals(KeyMoveResult.MOVED, move(Store(KeyProtection.TEE, KeyProtection.STRONGBOX), empty))
        assertEquals(1, empty.moves)
    }

    @Test
    fun aLevelThatCannotBeReadIsNotMoved() {
        val busy = Store(KeyProtection.STRONGBOX).apply { unreadable = true }
        assertEquals(KeyMoveResult.REFUSED, move(busy))
        // It was tried, since its level was unknown.
        assertEquals(1, busy.moves)
    }

    @Test
    fun aLevelUnreadableAfterTheMoveIsPartialNotMoved() {
        val a = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        val b = Store(KeyProtection.TEE, KeyProtection.STRONGBOX)
        val keys = listOf(a.key(), MovableKey({ if (b.moves > 0) throw IllegalStateException("busy") else b.level }) { b.moves++; true })
        assertEquals(KeyMoveResult.PARTIAL, runBlocking { moveKeys(keys) })
    }

    @Test
    fun aSealedFileWithoutTheSecurityKeySaysSo() {
        val sealed = Store(KeyProtection.TEE, fails = SecurityKeyRequiredException())
        assertEquals(KeyMoveResult.SECURITY_KEY_REQUIRED, move(sealed, Store(KeyProtection.TEE, KeyProtection.STRONGBOX)))
    }

    @Test
    fun aMoveThatThrowsIsRefused() {
        assertEquals(KeyMoveResult.REFUSED, move(Store(KeyProtection.TEE, fails = IllegalStateException("chip"))))
    }

    @Test
    fun theWeakestLevelIsShown() {
        assertEquals(KeyProtection.TEE, weakest(listOf(KeyProtection.STRONGBOX, KeyProtection.TEE)))
        assertEquals(KeyProtection.TEE, weakest(listOf(KeyProtection.TEE, null)))
        assertEquals(KeyProtection.STRONGBOX, weakest(listOf(null, KeyProtection.STRONGBOX)))
        assertEquals(KeyProtection.SOFTWARE, weakest(listOf(KeyProtection.TEE, KeyProtection.SOFTWARE)))
        assertNull(weakest(listOf(null, null)))
    }
}
