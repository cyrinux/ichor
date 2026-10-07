package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class StoreWritesTest {

    private val log = mutableListOf<String>()

    private fun step(name: String, fails: Boolean = false, undoFails: Boolean = false) = StoreWrite(
        write = {
            if (fails) throw IllegalStateException("$name failed")
            log += "write $name"
        },
        undo = {
            if (undoFails) throw IllegalStateException("undo $name failed")
            log += "undo $name"
        },
    )

    @Test
    fun allWritesWhenNothingFails() {
        writeAll(listOf(step("talos"), step("kube")))

        assertEquals(listOf("write talos", "write kube"), log)
    }

    @Test
    fun aFailedSecondWriteUndoesTheFirst() {
        val e = assertThrows(IllegalStateException::class.java) { writeAll(listOf(step("talos"), step("kube", fails = true))) }

        assertEquals("kube failed", e.message)
        assertEquals(listOf("write talos", "undo talos"), log)
    }

    @Test
    fun aFailedFirstWriteUndoesNothing() {
        assertThrows(IllegalStateException::class.java) { writeAll(listOf(step("talos", fails = true), step("kube"))) }

        assertEquals(emptyList<String>(), log)
    }

    @Test
    fun aFailedUndoKeepsTheOriginalFailure() {
        val e = assertThrows(IllegalStateException::class.java) {
            writeAll(listOf(step("talos", undoFails = true), step("kube", fails = true)))
        }

        assertEquals("kube failed", e.message)
        assertEquals(1, e.suppressed.size)
    }
}
