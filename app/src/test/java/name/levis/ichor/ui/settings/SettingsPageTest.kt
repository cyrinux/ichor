package name.levis.ichor.ui.settings

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SettingsPageTest {

    @Test
    fun pagesKeepTheSettingsOrder() {
        assertEquals(
            listOf("appearance", "security", "privacy", "alerts", "ai", "git-hosts", "plugins", "kubernetes", "clusters", "support", "about"),
            SettingsPage.entries.map { it.route },
        )
    }

    @Test
    fun kubernetesNeedsTheKubeconfigFeature() {
        val reader = ContextSummary(name = "lab", roles = listOf("os:reader"))
        val admin = ContextSummary(name = "lab", roles = listOf("os:admin"))
        val kube = ContextSummary(name = "lab", kind = KIND_KUBE)
        assertFalse(SettingsPage.KUBERNETES.visible(null))
        assertFalse(SettingsPage.KUBERNETES.visible(reader))
        assertTrue(SettingsPage.KUBERNETES.visible(admin))
        assertTrue(SettingsPage.KUBERNETES.visible(kube))
        assertEquals(10, visibleSettingsPages(reader).size)
        assertEquals(11, visibleSettingsPages(admin).size)
    }

    @Test
    fun everyOtherPageIsAlwaysShown() {
        SettingsPage.entries.filter { it != SettingsPage.KUBERNETES }.forEach { assertTrue(it.name, it.visible(null)) }
    }

    @Test
    fun routesAreDistinctAndRoundTrip() {
        assertEquals(SettingsPage.entries.size, SettingsPage.entries.map { it.route }.toSet().size)
        SettingsPage.entries.forEach { assertEquals(it, SettingsPage.fromRoute(it.route)) }
        assertEquals(null, SettingsPage.fromRoute("nope"))
    }
}
