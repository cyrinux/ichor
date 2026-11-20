package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SignInKeysTest {

    @Test
    fun omniClustersKeepTheirSharedSignIn() {
        val contexts = listOf(
            ContextSummary("lab", fingerprint = "t1"),
            ContextSummary("acme-demo", fingerprint = "o1", omni = true, authKey = "omnikey"),
            ContextSummary("acme-prod", fingerprint = "o2", omni = true, authKey = "omnikey"),
            ContextSummary("oidc", fingerprint = "k1", kind = KIND_KUBE),
        )

        // A certificate context signs in to nothing; two clusters of one Omni identity share one key.
        assertEquals(listOf("o1", "omnikey", "o2", "k1"), signInKeys(contexts))
    }

    @Test
    fun teleportClustersKeepTheirProxySignIn() {
        val contexts = listOf(
            ContextSummary("teleport.example.org-a", fingerprint = "k1", kind = KIND_KUBE, signIn = "teleport", authKey = "teleport:proxy/a"),
            ContextSummary("teleport.example.org-b", fingerprint = "k2", kind = KIND_KUBE, signIn = "teleport", authKey = "teleport:proxy/a"),
        )

        assertEquals(listOf("k1", "teleport:proxy/a", "k2"), signInKeys(contexts))
    }

    @Test
    fun kubeAndTalosClustersKeepTheirSshBastion() {
        val contexts = listOf(
            ContextSummary("jump", fingerprint = "k1", kind = KIND_KUBE, bastionKey = "bastionk"),
            // A Talos context keeps only its bastion: its certificate lives in the talosconfig.
            ContextSummary("lab", fingerprint = "t1", bastionKey = "bastiont"),
            // The demo has none.
            ContextSummary("demo", fingerprint = "t2", demo = true),
        )

        // Without it the store would prune the entry and the cluster would silently go direct.
        assertEquals(listOf("k1", "bastionk", "bastiont"), signInKeys(contexts))
        assertEquals(
            mapOf("bastionk" to "b", "bastiont" to "t"),
            restoredKubeAuth(mapOf("bastionk" to "b", "bastiont" to "t"), signInKeys(contexts)),
        )
    }

    @Test
    fun backupKeepsTheOmniServiceAccount() {
        val states = mapOf("omnikey" to "sa-state", "gone" to "x")
        val kept = backupKubeAuth(listOf("omnikey"), states) { "backed-up-$it" }

        assertEquals(mapOf("omnikey" to "backed-up-sa-state"), kept)
        assertEquals(mapOf("omnikey" to "s"), restoredKubeAuth(mapOf("omnikey" to "s", "old" to "t"), listOf("omnikey")))
    }

    @Test
    fun removingAnOmniClusterKeepsTheSignInItsSiblingsShare() {
        val contexts = listOf(
            ContextSummary("lab", fingerprint = "t1"),
            ContextSummary("acme-demo", fingerprint = "o1", omni = true, authKey = "omnikey", signIn = "omni"),
            ContextSummary("acme-prod", fingerprint = "o2", omni = true, authKey = "omnikey", signIn = "omni"),
            ContextSummary("other", fingerprint = "o3", omni = true, authKey = "otherkey", signIn = "omni-service-account"),
            ContextSummary("oidc", fingerprint = "k1", kind = KIND_KUBE, signIn = "oidc"),
        )

        // The other cluster of the identity still signs in with the shared key.
        assertFalse(signsOutOnRemoval(contexts, "acme-demo"))
        // The identity's last cluster takes its sign-in with it.
        assertTrue(signsOutOnRemoval(contexts, "other"))
        assertTrue(signsOutOnRemoval(contexts, "oidc"))
        // A certificate context signs in to nothing; an unknown name neither.
        assertFalse(signsOutOnRemoval(contexts, "lab"))
        assertFalse(signsOutOnRemoval(contexts, "gone"))
    }
}
