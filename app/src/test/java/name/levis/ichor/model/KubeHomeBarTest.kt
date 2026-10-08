package name.levis.ichor.model

import name.levis.ichor.model.KubeHomeAction.API_HEALTH
import name.levis.ichor.model.KubeHomeAction.CHECKUP
import name.levis.ichor.model.KubeHomeAction.DATA_SERVICES
import name.levis.ichor.model.KubeHomeAction.HELM
import name.levis.ichor.model.KubeHomeAction.METRICS
import name.levis.ichor.model.KubeHomeAction.NETWORK_POLICIES
import name.levis.ichor.model.KubeHomeAction.RESOURCES
import name.levis.ichor.model.KubeHomeAction.SETTINGS
import name.levis.ichor.model.KubeHomeAction.WORKLOADS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeHomeBarTest {

    @Test
    fun defaultKeepsThreeIconsSoTheClusterNameFits() {
        val bar = KubeHomeAction.bar.default
        assertEquals(listOf(WORKLOADS, RESOURCES, METRICS), bar.icons)
        assertEquals(listOf(HELM, DATA_SERVICES, CHECKUP, API_HEALTH, NETWORK_POLICIES, SETTINGS), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val bar = KubeHomeAction.bar.default.toBar(DATA_SERVICES).toMenu(RESOURCES)
        assertEquals("WORKLOADS,METRICS,DATA_SERVICES|RESOURCES,HELM,CHECKUP,API_HEALTH,NETWORK_POLICIES,SETTINGS", bar.encode())
        assertEquals(bar, KubeHomeAction.bar.parse(bar.encode()))
        assertFalse(bar.isDefault)
    }

    @Test
    fun theOverviewsNamesAreNotTaken() {
        val bar = KubeHomeAction.bar.parse("HEALTH,EVENTS|KUBESPAN")
        assertTrue(bar.icons.isEmpty())
        assertEquals(KubeHomeAction.entries, bar.menu)
    }
}
