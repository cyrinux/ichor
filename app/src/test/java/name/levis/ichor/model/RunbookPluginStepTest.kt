package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RunbookPluginStepTest {

    private val plan = TalosJson.decodeFromString(
        RunbookPlan.serializer(),
        """{"name":"Plugin steps","steps":[
            {"id":"typed","action":"plugin/demo-actions/restart","target":{"kind":"Widget","namespace":"demo-shop","name":"alpha","node":""},
             "destructive":true,"role":"os:admin","allowed":true,"pluginId":"demo-actions","pluginName":"Demo Actions","pluginIcon":"","label":"Redémarrer","confirm":"typedName"},
            {"id":"plain","action":"plugin/demo-actions/pause","target":{"kind":"Widget","namespace":"demo-shop","name":"alpha"},
             "destructive":true,"pluginId":"demo-actions","label":"Pause","confirm":"plain"},
            {"id":"pool","action":"plugin/demo-actions/retire","target":{"kind":"Pool","namespace":"","name":"shared"},
             "destructive":true,"pluginId":"demo-actions","label":"Retire","confirm":"typedName"},
            {"id":"scale","action":"scale","target":{"kind":"Deployment","namespace":"demo-shop","name":"ingest-api"},"destructive":true},
            {"id":"reboot","action":"nodeReboot","target":{"node":"10.5.0.5"},"destructive":true}],"errors":[]}""",
    )

    @Test
    fun aPluginStepSaysItsLabel() {
        val (typed, _, _, scale) = plan.steps
        assertTrue(typed.isPlugin)
        assertEquals("Demo Actions", typed.pluginName)
        assertEquals("Redémarrer", typed.actionLabel)
        assertFalse(scale.isPlugin)
        assertEquals("scale", scale.actionLabel)
    }

    @Test
    fun theConfirmationFollowsThePlugin() {
        val (typed, plain, _, scale) = plan.steps
        val reboot = plan.steps[4]
        assertTrue(typed.confirmsByHostname())
        assertEquals("alpha", typed.confirmName)
        assertFalse(plain.confirmsByHostname())
        assertFalse(scale.confirmsByHostname())
        assertTrue(reboot.confirmsByHostname())
        assertEquals("10.5.0.5", reboot.confirmName)
    }

    @Test
    fun aClusterScopedObjectHasNoNamespaceInItsLabel() {
        assertEquals("Pool shared", plan.steps[2].target.label)
        assertEquals("Widget demo-shop/alpha", plan.steps[0].target.label)
    }
}
