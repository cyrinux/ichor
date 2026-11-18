package name.levis.ichor.ui

import name.levis.ichor.model.BastionProblem
import org.junit.Assert.assertEquals
import org.junit.Test

class BastionTextTest {
    @Test
    fun bastionFailuresBecomeTheirOwnText() {
        assertEquals(UiText.Bastion(BastionProblem.FORWARD), RuntimeException("list pods: kube-bastion-forward: prohibited").uiText())
        assertEquals(UiText.Bastion(BastionProblem.HOST_KEY), RuntimeException("kube-bastion-host-key").uiText())
    }

    @Test
    fun otherFailuresAreNoBastionText() {
        assertEquals(false, RuntimeException("kube-bastion-unavailable: proxy-url").uiText() is UiText.Bastion)
    }

    @Test
    fun beforeTheAppStartsABastionMessageIsKept() {
        // No app context in a unit test: the message, never the sign-in fallback.
        assertEquals("kube-bastion-auth: denied", goErrorText("kube-bastion-auth: denied"))
    }

    @Test
    fun everyProblemHasASentence() {
        assertEquals(BastionProblem.entries.size, BastionProblem.entries.map { it.text }.toSet().size)
    }
}
