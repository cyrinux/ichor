package name.levis.ichor.ui.node

import org.junit.Assert.assertEquals
import org.junit.Test

class NodeTabsTest {

    @Test
    fun theFiveTalosTabsAreAlwaysShown() {
        assertEquals(listOf(0, 1, 2, 3, 4), nodeTabs(canCgroups = false, canKubePods = false))
    }

    @Test
    fun cgroupsAndKubernetesPodsComeLast() {
        assertEquals(listOf(0, 1, 2, 3, 4, CGROUPS_TAB, KUBE_PODS_TAB), nodeTabs(canCgroups = true, canKubePods = true))
    }

    @Test
    fun kubernetesPodsTakesTheCgroupsPlaceWhenItIsHidden() {
        val tabs = nodeTabs(canCgroups = false, canKubePods = true)
        assertEquals(listOf(0, 1, 2, 3, 4, KUBE_PODS_TAB), tabs)
        assertEquals(5, tabs.indexOf(KUBE_PODS_TAB))
    }

    @Test
    fun aDeepLinkToAHiddenTabFallsBackToServices() {
        val tabs = nodeTabs(canCgroups = true, canKubePods = false)
        assertEquals(0, shownNodeTab(KUBE_PODS_TAB, tabs))
        assertEquals(CGROUPS_TAB, shownNodeTab(CGROUPS_TAB, tabs))
        assertEquals(0, shownNodeTab(CGROUPS_TAB, nodeTabs(canCgroups = false, canKubePods = true)))
    }
}
