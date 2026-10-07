package name.levis.ichor.ui.debug

import name.levis.ichor.ui.UiText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DebugShellsTest {

    @Test
    fun onlyAStartingOrRunningShellKeepsTheAppUp() {
        assertTrue(ShellState.Starting(UiText.Raw("pulling")).isLive)
        assertTrue(ShellState.Running.isLive)
        assertFalse(ShellState.Setup.isLive)
        assertFalse(ShellState.Exited(0, "").isLive)
        assertFalse(ShellState.Exited(-1, "connection lost").isLive)
    }

    @Test
    fun aShellIsOnePerNodeOfACluster() {
        val key = ShellKey("prod", "10.0.0.2")
        assertEquals(key.notificationId, ShellKey("prod", "10.0.0.2").notificationId)
        // The same address in another cluster is another shell, with its own notification.
        assertNotEquals(key, ShellKey("lab", "10.0.0.2"))
        assertNotEquals(key.notificationId, ShellKey("lab", "10.0.0.2").notificationId)
    }

    @Test
    fun aPodShellIsOnePerContainer() {
        val web = ShellKey("prod", "", "apps", "web-0", "web")
        assertTrue(web.isPod)
        assertFalse(ShellKey("prod", "10.0.0.2").isPod)
        assertEquals(web, ShellKey("prod", "", "apps", "web-0", "web"))
        // Its sidecar is another shell, with its own notification.
        assertNotEquals(web.notificationId, web.copy(container = "envoy").notificationId)
        assertEquals(listOf(web), orphanedShells(listOf(web), setOf("lab")))
    }

    @Test
    fun notificationIdsArePositive() {
        listOf(ShellKey("", ""), ShellKey("prod", "10.0.0.2"), ShellKey("a", "b"))
            .forEach { assertTrue(it.notificationId > 0) }
    }

    @Test
    fun shellsOfRemovedClustersAreClosed() {
        val prod = ShellKey("prod", "10.0.0.2")
        val lab = ShellKey("lab", "10.0.0.3")
        assertEquals(listOf(lab), orphanedShells(listOf(prod, lab), setOf("prod")))
        assertEquals(listOf(prod, lab), orphanedShells(listOf(prod, lab), emptySet()))
        assertEquals(emptyList<ShellKey>(), orphanedShells(listOf(prod), setOf("prod", "lab")))
    }
}
