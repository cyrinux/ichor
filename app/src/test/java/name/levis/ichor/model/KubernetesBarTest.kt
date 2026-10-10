package name.levis.ichor.model

import name.levis.ichor.model.KubernetesAction.API_ADDRESS
import name.levis.ichor.model.KubernetesAction.API_HEALTH
import name.levis.ichor.model.KubernetesAction.CHECKUP
import name.levis.ichor.model.KubernetesAction.EVENTS
import name.levis.ichor.model.KubernetesAction.FLOWS
import name.levis.ichor.model.KubernetesAction.HELM
import name.levis.ichor.model.KubernetesAction.JOBS
import name.levis.ichor.model.KubernetesAction.NETWORK_POLICIES
import name.levis.ichor.model.KubernetesAction.RESOURCES
import name.levis.ichor.model.KubernetesAction.SERVICES
import name.levis.ichor.model.KubernetesAction.SHARE
import name.levis.ichor.model.KubernetesAction.STORAGE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubernetesBarTest {

    @Test
    fun defaultKeepsThreeIconsSoTheTitleFits() {
        val bar = KubernetesAction.bar.default
        assertEquals(listOf(CHECKUP, NETWORK_POLICIES, SHARE), bar.icons)
        assertEquals(listOf(API_HEALTH, FLOWS, RESOURCES, HELM, STORAGE, SERVICES, JOBS, EVENTS, API_ADDRESS), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun keptApartFromTheOverviewsBar() {
        val bar = KubernetesAction.bar.default.toBar(FLOWS).toMenu(CHECKUP)
        assertEquals("NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,API_ADDRESS", bar.encode())
        assertEquals(bar, KubernetesAction.bar.parse(bar.encode()))
        assertFalse(bar.isDefault)
        // Another screen's names are unknown here: none of them is taken.
        val foreign = KubernetesAction.bar.parse("HEALTH,ETCD|METRICS")
        assertTrue(foreign.icons.isEmpty())
        assertEquals(KubernetesAction.entries, foreign.menu)
    }
}
