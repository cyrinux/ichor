package name.levis.talosmobile.model

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
