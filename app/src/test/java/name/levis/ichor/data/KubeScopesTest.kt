package name.levis.ichor.data

import name.levis.ichor.model.KubeScope
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class KubeScopesTest {

    @Test
    fun remembersTheScopePickedPerCluster() {
        val prefs = MemoryPrefs()
        val scopes = KubeScopes(prefs)
        scopes.set("fp1", KubeScope("shop", chosen = true))
        scopes.set("fp2", KubeScope(chosen = true))
        scopes.set("", KubeScope("ignored", chosen = true))

        // Read back after a restart.
        val again = KubeScopes(prefs)
        assertEquals(KubeScope("shop", chosen = true), again.scope("fp1"))
        assertEquals(KubeScope(chosen = true), again.scope("fp2"))
        assertNull(again.scope("fp3"))

        // A scope not chosen (the default) forgets the cluster's.
        again.set("fp1", KubeScope())
        assertNull(again.scope("fp1"))
        assertEquals(mapOf("fp2" to ""), again.scopes.value)
    }
}
