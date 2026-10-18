package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfirmationTest {

    @Test
    fun typedTokenConfirms() {
        assertTrue(confirmationMatches("cp-1", "cp-1"))
        assertTrue(confirmationMatches("  cp-1 ", "cp-1"))
        assertFalse(confirmationMatches("cp-", "cp-1"))
        assertFalse(confirmationMatches("CP-1", "cp-1"))
    }

    @Test
    fun longNameIsConfirmedByItsEnd() {
        val volume = "pvc-0b8e6a1c-4d2f-4a7e-9b3c-7f1e2d9c0e1a"
        assertEquals("c0e1a", confirmationCode(volume))
        assertTrue(confirmationMatches("c0e1a", volume))
        assertTrue(confirmationMatches(" c0e1a ", volume))
        assertTrue(confirmationMatches(volume, volume))
        assertFalse(confirmationMatches("0e1a", volume))
        assertFalse(confirmationMatches("pvc-0", volume))
        assertFalse(confirmationMatches("C0E1A", volume))
    }

    @Test
    fun shortNameIsTypedWhole() {
        assertEquals("worker-12345", confirmationCode("worker-12345"))
        assertFalse(confirmationMatches("12345", "worker-12345"))
        assertEquals("r-1234", confirmationCode("worker-1234").takeLast(6))
    }

    @Test
    fun blankTokenNeverConfirms() {
        assertFalse(confirmationMatches("", ""))
        assertFalse(confirmationMatches("  ", " "))
        assertFalse(confirmationMatches("anything", ""))
    }

    @Test
    fun memberWithoutHostnameIsConfirmedByItsId() {
        assertEquals("cp-2", EtcdMemberRef(id = "b1c2", hostname = "cp-2").confirmToken)
        assertEquals("b1c2", EtcdMemberRef(id = "b1c2", hostname = " ").confirmToken)
        assertFalse(confirmationMatches("", EtcdMemberRef(id = "b1c2").confirmToken))
    }
}
