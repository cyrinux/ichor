package name.levis.ichor.data

import kotlinx.coroutines.runBlocking
import name.levis.ichor.security.DekHolder
import name.levis.ichor.security.SecurityKeyRequiredException
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.io.IOException
import java.security.ProviderException
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

class SecureStoreRekeyTest {

    @get:Rule
    val tmp = TemporaryFolder()

    /** A Keystore of software keys; StrongBox refuses [chipRefusals] times (UNKNOWN_ERROR) first. */
    private class FakeKeys(var chipRefusals: Int = 0) : KeySlots {
        val entries = mutableMapOf<String, Pair<SecretKey, Boolean>>()

        override fun get(alias: String): SecretKey? = entries[alias]?.first

        override fun generate(alias: String, strongBox: Boolean): SecretKey {
            if (strongBox && chipRefusals > 0) {
                chipRefusals--
                throw ProviderException("Keystore key generation failed")
            }
            val key = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
            entries[alias] = key to strongBox
            return key
        }

        override fun delete(alias: String) {
            entries.remove(alias)
        }

        fun inStrongBox(alias: String): Boolean = entries[alias]?.second == true
    }

    /** What a kill does: nothing after it runs, no cleanup either (an Error, not an Exception). */
    private class Kill : Error()

    private class Dek(private val dek: ByteArray?) : DekHolder {
        override fun dek(): ByteArray? = dek
        override fun sealing(): Boolean = true
    }

    private val config = "contexts:\n  sample-lab:\n    endpoints: [192.0.2.10]\n".encodeToByteArray()
    private val keys = FakeKeys()
    private val file by lazy { File(tmp.root, "talosconfig.enc") }

    private fun store(
        keys: KeySlots = this.keys,
        outer: DekHolder? = null,
        strongBox: Boolean = true,
        beforeStep: (RekeyStep) -> Unit = {},
    ) = SecureStore(file, "talosconfig", strongBox, outer, {}, keys, beforeStep)

    /** A config stored under a TEE key, as after the chip kept refusing at the first write. */
    private fun storedInTee(outer: DekHolder? = null) {
        keys.generate("talosconfig", strongBox = false)
        store(outer = outer).write(config)
    }

    private fun files(): List<String> = tmp.root.list().orEmpty().sorted()

    private fun rekey(store: SecureStore, delays: List<Long> = emptyList(), pauses: MutableList<Long> = mutableListOf()) =
        runBlocking { store.rekeyToStrongBox(delays) { pauses += it } }

    private fun assertMovedToStrongBox() {
        assertEquals(setOf("talosconfig.next"), keys.entries.keys)
        assertTrue(keys.inStrongBox("talosconfig.next"))
        assertEquals(listOf("talosconfig.enc", "talosconfig.enc.alias"), files())
        assertEquals("talosconfig.next", store().aliasInUse())
        assertArrayEquals(config, store().read())
    }

    private fun assertUnchanged() {
        assertEquals(setOf("talosconfig"), keys.entries.keys)
        assertFalse(keys.inStrongBox("talosconfig"))
        assertEquals(listOf("talosconfig.enc"), files())
        assertArrayEquals(config, store().read())
    }

    @Test
    fun aMoveLeavesExactlyOneStrongBoxKeyAndOneFile() {
        storedInTee()
        assertTrue(rekey(store()))
        assertMovedToStrongBox()
    }

    @Test
    fun aSecondMoveGoesBackToTheFirstAlias() {
        storedInTee()
        assertTrue(rekey(store()))
        assertTrue(rekey(store()))
        assertEquals(setOf("talosconfig"), keys.entries.keys)
        assertTrue(keys.inStrongBox("talosconfig"))
        assertEquals("talosconfig", store().aliasInUse())
        assertArrayEquals(config, store().read())
    }

    @Test
    fun aWriteAfterAMoveUsesTheNewKey() {
        storedInTee()
        assertTrue(rekey(store()))
        val changed = "contexts: {}\n".encodeToByteArray()
        store().write(changed)
        assertEquals(setOf("talosconfig.next"), keys.entries.keys)
        assertArrayEquals(changed, store().read())
    }

    @Test
    fun aChipThatKeepsRefusingChangesNothing() {
        storedInTee()
        keys.chipRefusals = Int.MAX_VALUE
        val pauses = mutableListOf<Long>()
        assertFalse(rekey(store(), delays = listOf(1, 2), pauses = pauses))
        assertEquals(listOf(1L, 2L), pauses)
        assertUnchanged()
    }

    @Test
    fun aChipBusyOnceIsTriedAgain() {
        storedInTee()
        keys.chipRefusals = 1
        assertTrue(rekey(store(), delays = listOf(1)))
        assertMovedToStrongBox()
    }

    @Test
    fun withoutStrongBoxNothingMoves() {
        storedInTee()
        assertFalse(rekey(store(strongBox = false)))
        assertUnchanged()
    }

    @Test
    fun aFailureBeforeTheSwapLeavesTheOriginalFileAndKey() {
        RekeyStep.entries.filter { it <= RekeyStep.RENAME }.forEach { failing ->
            reset()
            storedInTee()
            assertFalse("$failing", rekey(store { if (it == failing) throw IOException("injected at $it") }))
            assertUnchanged()
        }
    }

    @Test
    fun aFailureAfterTheSwapStillReadsAndTheNextReadFinishesTheMove() {
        RekeyStep.entries.filter { it > RekeyStep.RENAME }.forEach { failing ->
            reset()
            storedInTee()
            rekey(store { if (it == failing) throw IOException("injected at $it") })
            assertArrayEquals("$failing", config, store().read())
            assertEquals("$failing", "talosconfig.next", store().aliasInUse())
            assertTrue("$failing", keys.inStrongBox("talosconfig.next"))
        }
    }

    @Test
    fun aKillAtEveryStepNeverLosesTheConfigAndTheNextMoveCleansUp() {
        RekeyStep.entries.forEach { killed ->
            reset()
            storedInTee()
            assertThrows("$killed", Kill::class.java) { rekey(store { if (it == killed) throw Kill() }) }
            // The app starts again: a new store, the same Keystore and files.
            assertArrayEquals("$killed", config, store().read())
            val inUse = store().aliasInUse()!!
            if (!keys.inStrongBox(inUse)) assertTrue("$killed", rekey(store()))
            assertEquals("$killed", 1, keys.entries.size)
            assertTrue("$killed", keys.inStrongBox(store().aliasInUse()!!))
            assertEquals("$killed", listOf("talosconfig.enc", "talosconfig.enc.alias"), files())
            assertArrayEquals("$killed", config, store().read())
        }
    }

    @Test
    fun aKillBetweenTheSwapAndTheMarkerReadsWithTheOtherAliasAndRecordsIt() {
        storedInTee()
        assertThrows(Kill::class.java) { rekey(store { if (it == RekeyStep.SYNC_DIR) throw Kill() }) }
        // The file is under the new key, the marker (none) still says the old alias.
        assertEquals(listOf("talosconfig.enc"), files())
        assertEquals(setOf("talosconfig", "talosconfig.next"), keys.entries.keys)
        assertEquals("talosconfig", store().aliasInUse())

        assertArrayEquals(config, store().read())
        assertEquals("talosconfig.next", File(tmp.root, "talosconfig.enc.alias").readText())
        assertMovedToStrongBox()
    }

    @Test
    fun aKillAfterTheNewKeyWasMadeKeepsWritingUnderTheOldOne() {
        storedInTee()
        assertThrows(Kill::class.java) { rekey(store { if (it == RekeyStep.WRITE_NEXT) throw Kill() }) }
        assertEquals("talosconfig", store().aliasInUse())
        val changed = "contexts: {}\n".encodeToByteArray()
        store().write(changed)
        assertArrayEquals(changed, store().read())
        // The leftover chip key is replaced by the next move.
        assertTrue(rekey(store()))
        assertEquals(setOf("talosconfig.next"), keys.entries.keys)
        assertArrayEquals(changed, store().read())
    }

    @Test
    fun aSealedFileStaysSealedWithTheSameSecurityKey() {
        val dek = ByteArray(32) { it.toByte() }
        storedInTee(outer = Dek(dek))
        assertTrue(store(outer = Dek(dek)).isSealed())
        assertTrue(rekey(store(outer = Dek(dek))))
        assertTrue(store(outer = Dek(dek)).isSealed())
        assertArrayEquals(config, store(outer = Dek(dek)).read())
        assertThrows(SecurityKeyRequiredException::class.java) { store(outer = Dek(null)).read() }
    }

    @Test
    fun aKeyWithoutAFileIsDroppedAndCountsAsDone() {
        storedInTee()
        assertTrue(rekey(store()))
        file.delete()
        keys.generate("talosconfig", strongBox = false)
        assertTrue(rekey(store()))
        assertEquals(emptySet<String>(), keys.entries.keys)
        assertEquals(emptyList<String>(), files())
        assertEquals(null, store().read())
    }

    @Test
    fun aSealedFileWithoutTheSecurityKeyThrowsBeforeAnyChange() {
        storedInTee(outer = Dek(ByteArray(32) { it.toByte() }))
        assertThrows(SecurityKeyRequiredException::class.java) { rekey(store(outer = Dek(null))) }
        assertEquals(setOf("talosconfig"), keys.entries.keys)
        assertEquals(listOf("talosconfig.enc"), files())
    }

    @Test
    fun clearDropsBothKeysAndTheMarker() {
        storedInTee()
        assertThrows(Kill::class.java) { rekey(store { if (it == RekeyStep.DELETE_OLD_KEY) throw Kill() }) }
        store().clear()
        assertEquals(emptySet<String>(), keys.entries.keys)
        assertEquals(emptyList<String>(), files())
    }

    private fun reset() {
        keys.entries.clear()
        keys.chipRefusals = 0
        tmp.root.listFiles()?.forEach { it.delete() }
    }
}
