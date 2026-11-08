package name.levis.ichor.model
import org.junit.Assert.*
import org.junit.Test
class LonghornAttachmentTest {
    @Test fun requiresReadyChosenNodeAndFreshCurrentNode() {
        val p = LonghornAttachmentPreview(canAttach = true, nodes = listOf(LonghornAttachmentNode("n1", true), LonghornAttachmentNode("n2", false)))
        assertFalse(p.accepts("", true)); assertTrue(p.accepts("n1", true)); assertFalse(p.accepts("n2", true)); assertFalse(p.accepts("foreign", true))
        assertFalse(p.copy(error = "denied").accepts("n1", true))
        val attached = p.copy(canAttach = false, canDetach = true, node = "n1")
        assertTrue(attached.accepts("n1", false)); assertFalse(attached.accepts("n2", false))
    }
}
