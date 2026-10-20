package name.levis.ichor.model

import name.levis.ichor.model.KubernetesAction.API_ADDRESS
import name.levis.ichor.model.KubernetesAction.ARGO_ROLLOUTS
import name.levis.ichor.model.KubernetesAction.AUTOSCALING
import name.levis.ichor.model.KubernetesAction.API_HEALTH
import name.levis.ichor.model.KubernetesAction.CASTAI
import name.levis.ichor.model.KubernetesAction.CERT_MANAGER
import name.levis.ichor.model.KubernetesAction.CHECKUP
import name.levis.ichor.model.KubernetesAction.CLEAN_UP_PODS
import name.levis.ichor.model.KubernetesAction.CLUSTER_LIFECYCLE
import name.levis.ichor.model.KubernetesAction.CROSSPLANE
import name.levis.ichor.model.KubernetesAction.DATA_SERVICES
import name.levis.ichor.model.KubernetesAction.EVENTS
import name.levis.ichor.model.KubernetesAction.EXTERNAL_DNS
import name.levis.ichor.model.KubernetesAction.FLOWS
import name.levis.ichor.model.KubernetesAction.HELM
import name.levis.ichor.model.KubernetesAction.JOBS
import name.levis.ichor.model.KubernetesAction.NETWORK_POLICIES
import name.levis.ichor.model.KubernetesAction.POLICIES
import name.levis.ichor.model.KubernetesAction.RESOURCES
import name.levis.ichor.model.KubernetesAction.SERVICES
import name.levis.ichor.model.KubernetesAction.SHARE
import name.levis.ichor.model.KubernetesAction.STORAGE
import name.levis.ichor.model.KubernetesAction.TAILSCALE
import name.levis.ichor.model.KubernetesAction.VIRTUAL_MACHINES
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubernetesBarTest {

    @Test
    fun defaultKeepsThreeIconsSoTheTitleFits() {
        val bar = KubernetesAction.bar.default
        assertEquals(listOf(CHECKUP, NETWORK_POLICIES, SHARE), bar.icons)
        assertEquals(listOf(API_HEALTH, FLOWS, RESOURCES, HELM, STORAGE, SERVICES, JOBS, EVENTS, API_ADDRESS, POLICIES, AUTOSCALING, VIRTUAL_MACHINES, CLEAN_UP_PODS, CROSSPLANE, TAILSCALE, ARGO_ROLLOUTS, CLUSTER_LIFECYCLE, DATA_SERVICES, EXTERNAL_DNS, CERT_MANAGER, CASTAI), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun aBarSavedBeforeTheOperatorScreensGetsThemLastInItsMenu() {
        val saved = KubernetesAction.bar.parse("CHECKUP,FLOWS|SHARE,API_HEALTH")
        assertEquals(listOf(CHECKUP, FLOWS), saved.icons)
        assertEquals(listOf(CROSSPLANE, TAILSCALE, ARGO_ROLLOUTS, CLUSTER_LIFECYCLE), saved.menu.dropLast(4).takeLast(4))
    }

    @Test
    fun aBarSavedBeforeTheDataServicesEntriesGetsThemLastInItsMenu() {
        val saved = KubernetesAction.bar.parse("CHECKUP,CROSSPLANE|SHARE,CLUSTER_LIFECYCLE")
        assertEquals(listOf(CHECKUP, CROSSPLANE), saved.icons)
        assertEquals(listOf(DATA_SERVICES, EXTERNAL_DNS, CERT_MANAGER, CASTAI), saved.menu.takeLast(4))
    }

    @Test
    fun keptApartFromTheOverviewsBar() {
        val bar = KubernetesAction.bar.default.toBar(FLOWS).toMenu(CHECKUP)
        assertEquals("NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,API_ADDRESS,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CLEAN_UP_PODS,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,EXTERNAL_DNS,CERT_MANAGER,CASTAI", bar.encode())
        assertEquals(bar, KubernetesAction.bar.parse(bar.encode()))
        assertFalse(bar.isDefault)
        // Another screen's names are unknown here: none of them is taken.
        val foreign = KubernetesAction.bar.parse("HEALTH,ETCD|METRICS")
        assertTrue(foreign.icons.isEmpty())
        assertEquals(KubernetesAction.entries, foreign.menu)
    }
}
