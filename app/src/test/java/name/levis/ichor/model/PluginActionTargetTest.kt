package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** An action dialog stays bound to the object it was opened on, whatever a reload does to the rows. */
class PluginActionTargetTest {
    private fun ref(namespace: String, name: String) = PluginRef(group = "example.com", version = "v1", resource = "widgets", namespace = namespace, name = name)

    private val resize = PluginAction(
        id = "resize",
        label = "Resize",
        enabled = true,
        params = listOf(PluginParam(name = "size", label = "Size", type = "int"), PluginParam(name = "mode", type = "enum")),
    )
    private val opened = PluginActionPending(ref("team-a", "alpha"), "resize")

    @Test
    fun sameObjectKeepsTheDialog() {
        assertEquals(resize, pendingAction(opened, ref("team-a", "alpha"), listOf(resize)))
    }

    @Test
    fun anotherObjectClosesIt() {
        assertNull(pendingAction(opened, ref("team-a", "beta"), listOf(resize)))
        // Same name in another namespace: not the object confirmed.
        assertNull(pendingAction(opened, ref("team-b", "alpha"), listOf(resize)))
        assertNull(pendingAction(opened, null, listOf(resize)))
        assertNull(pendingAction(null, ref("team-a", "alpha"), listOf(resize)))
    }

    @Test
    fun anActionWithItsOwnTargetRunsOnIt() {
        // An Ollama model row opens its server's Deployment; Load runs on the model.
        val row = PluginRef(group = "apps", version = "v1", resource = "deployments", kind = "Deployment", namespace = "ai", name = "ollama")
        val model = PluginRef(group = "ichor.dev", version = "v1alpha1", resource = "ollamamodels", kind = "OllamaModel", namespace = "ai", name = "llama3.2:3b")
        val load = PluginAction(id = "load", label = "Load", enabled = true, target = model)

        assertEquals(model, load.targetOn(row))
        assertEquals(row, resize.targetOn(row))
        assertEquals(model, load.targetOn(null))

        val pending = PluginActionPending(model, "load")
        assertEquals(load, pendingAction(pending, row, listOf(load)))
        // A reload that put another model in the row closes the dialog.
        assertNull(pendingAction(pending, row, listOf(load.copy(target = model.copy(name = "qwen2.5:14b")))))
        // Bound to the row's object, a pending run does not move to the action's own target.
        assertNull(pendingAction(PluginActionPending(row, "load"), row, listOf(load)))
    }

    @Test
    fun anActionNoLongerUsableClosesIt() {
        val here = ref("team-a", "alpha")
        assertNull(pendingAction(opened, here, listOf(resize.copy(enabled = false))))
        assertNull(pendingAction(opened, here, listOf(resize.copy(allowed = false))))
        assertNull(pendingAction(opened, here, listOf(resize.copy(id = "other"))))
        assertEquals(resize.copy(allowed = null), pendingAction(opened, here, listOf(resize.copy(allowed = null))))
    }

    @Test
    fun paramLabels() {
        assertEquals("Size", resize.paramLabel("size"))
        assertEquals("mode", resize.paramLabel("mode"))
        assertEquals("unknown", resize.paramLabel("unknown"))
    }

    @Test
    fun displayNames() {
        assertEquals("team-a/alpha", ref("team-a", "alpha").displayName)
        assertEquals("alpha", ref("", "alpha").displayName)
    }

    @Test
    fun rowKeysFollowTheObject() {
        val a = PluginRow(ref = ref("team-a", "alpha"), title = "alpha")
        val b = PluginRow(ref = ref("team-a", "beta"), title = "beta")
        val loose = PluginRow(title = "no object")
        val before = pluginRowKeys(listOf(a, b, loose))
        val after = pluginRowKeys(listOf(b, a, loose))
        assertEquals(before[0], after[1])
        assertEquals(before[1], after[0])
        assertEquals("i:2", before[2])
        // The same object twice: positions, so that keys stay unique.
        assertEquals(listOf("i:0", "i:1", "o:${b.ref!!.key}"), pluginRowKeys(listOf(a, a, b)))
    }
}
