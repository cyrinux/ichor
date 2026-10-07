package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OmniSignInTest {

    @Test
    fun readsTheReasonAfterTheCode() {
        assertEquals("no key", omniSignInNeeded("omni-sign-in-required: sign in to Omni: no key"))
        // Wrapped by the call that failed.
        assertEquals("Omni refused the key", omniSignInNeeded("overview: omni-sign-in-required: sign in to Omni: Omni refused the key"))
        assertEquals("", omniSignInNeeded("omni-sign-in-required"))
    }

    @Test
    fun otherErrorsAreNotASignIn() {
        assertNull(omniSignInNeeded(null))
        assertNull(omniSignInNeeded("connection refused"))
        assertNull(omniSignInNeeded("kube-sign-in-required: sign in to this cluster (oidc)"))
    }

    @Test
    fun omniIsATalosContextWithOmniAuth() {
        assertTrue(ContextSummary(name = "acme", auth = AUTH_OMNI).isOmni)
        assertFalse(ContextSummary(name = "lab").isOmni)
        assertFalse(ContextSummary(name = "eks", kind = KIND_KUBE, auth = AUTH_OMNI).isOmni)
    }

    @Test
    fun omniAllowsAllButIssuingACertificate() {
        val omni = ContextSummary(name = "acme", auth = AUTH_OMNI)
        Feature.entries.filter { it != Feature.ISSUE_CONFIG }.forEach { assertTrue(it.name, omni.allows(it)) }
        assertFalse(omni.allows(Feature.ISSUE_CONFIG))
    }

    @Test
    fun storedNameFollowsTheChoice() {
        val conflicts = listOf(ImportConflict(0, "acme-1", sameAs = "acme"))
        assertEquals("acme-1", storedContextName(0, "acme", conflicts, listOf(ImportChoice(0))))
        assertEquals("prod", storedContextName(0, "acme", conflicts, listOf(ImportChoice(0, name = " prod "))))
        assertEquals("acme", storedContextName(0, "acme", conflicts, listOf(ImportChoice(0, name = "x", replace = true))))
        // No conflict: its own name.
        assertEquals("lab", storedContextName(1, "lab", conflicts, emptyList()))
    }
}
