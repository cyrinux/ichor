package name.levis.ichor.model

import name.levis.ichor.model.KubernetesAction.API_ADDRESS
import name.levis.ichor.model.KubernetesAction.ARGO_ROLLOUTS
import name.levis.ichor.model.KubernetesAction.AUTOSCALING
import name.levis.ichor.model.KubernetesAction.API_HEALTH
import name.levis.ichor.model.KubernetesAction.CAPACITY
import name.levis.ichor.model.KubernetesAction.CASTAI
import name.levis.ichor.model.KubernetesAction.CERT_MANAGER
import name.levis.ichor.model.KubernetesAction.CHECKUP
import name.levis.ichor.model.KubernetesAction.CLUSTER_LIFECYCLE
import name.levis.ichor.model.KubernetesAction.CROSSPLANE
import name.levis.ichor.model.KubernetesAction.DATA_SERVICES
import name.levis.ichor.model.KubernetesAction.EVENTS
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
import name.levis.ichor.model.KubernetesActionGroup.EXPLORE
import name.levis.ichor.model.KubernetesActionGroup.HEALTH
import name.levis.ichor.model.KubernetesActionGroup.INSTALLED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubernetesBarTest {

    @Test
    fun defaultKeepsThreeIconsSoTheTitleFits() {
        val bar = KubernetesAction.bar.default
        assertEquals(listOf(CHECKUP, NETWORK_POLICIES, SHARE), bar.icons)
        assertEquals(listOf(API_HEALTH, FLOWS, RESOURCES, HELM, STORAGE, SERVICES, JOBS, EVENTS, API_ADDRESS, POLICIES, AUTOSCALING, VIRTUAL_MACHINES, CROSSPLANE, TAILSCALE, ARGO_ROLLOUTS, CLUSTER_LIFECYCLE, DATA_SERVICES, CERT_MANAGER, CASTAI, CAPACITY), bar.menu)
        assertTrue(bar.isDefault)
    }

    /** The same names in the same order as iOS's KubernetesAction (KubernetesBarTests.testTheSameActionsAsAndroid). */
    @Test
    fun theSameActionsAsIos() {
        assertEquals(
            "CHECKUP,NETWORK_POLICIES,SHARE,API_HEALTH,FLOWS,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,API_ADDRESS,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,CERT_MANAGER,CASTAI,CAPACITY",
            KubernetesAction.entries.joinToString(",") { it.name },
        )
        assertEquals(23, KubernetesAction.entries.size)
    }

    @Test
    fun aBarSavedWithCleanUpDropsItAndGetsCapacityLast() {
        val saved = KubernetesAction.bar.parse("CHECKUP,FLOWS|SHARE,CLEAN_UP_PODS,RESOURCES")
        assertEquals(listOf(CHECKUP, FLOWS), saved.icons)
        assertFalse(saved.encode().contains("CLEAN_UP_PODS"))
        assertEquals(listOf(SHARE, RESOURCES), saved.menu.take(2))
        assertEquals(CAPACITY, saved.menu.last())
        assertEquals(KubernetesAction.entries.toSet(), saved.order.toSet())
    }

    @Test
    fun theMenuReadsInSectionsAndASavedOrderHoldsInside() {
        val default = groupedMenu(KubernetesAction.bar.default.menu)
        assertEquals(listOf(EXPLORE, HEALTH, INSTALLED), default.map { it.first })
        assertEquals(listOf(RESOURCES, HELM, STORAGE, SERVICES, JOBS, EVENTS), default[0].second)
        assertEquals(listOf(API_HEALTH, FLOWS, API_ADDRESS, CAPACITY), default[1].second)
        // Events moved before Resources by the user: still in Explore, still first there.
        val saved = KubernetesAction.bar.parse("CHECKUP|EVENTS,CROSSPLANE,RESOURCES")
        assertEquals(listOf(EVENTS, RESOURCES), groupedMenu(saved.menu).first { it.first == EXPLORE }.second.take(2))
        // A section with nothing offered is left out.
        assertEquals(listOf(EXPLORE), groupedMenu(listOf(HELM)).map { it.first })
    }

    @Test
    fun everyActionHasASection() {
        assertEquals(KubernetesAction.entries.size, KubernetesActionGroup.entries.sumOf { g -> KubernetesAction.entries.count { it.group == g } })
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
        assertEquals(listOf(DATA_SERVICES, CERT_MANAGER, CASTAI, CAPACITY), saved.menu.takeLast(4))
    }

    @Test
    fun aSavedBarDropsTheEntriesOfSystemsThatBecamePlugins() {
        val saved = KubernetesAction.bar.parse("CHECKUP,EXTERNAL_DNS|SEALED_SECRETS,CERT_MANAGER")
        assertEquals(listOf(CHECKUP), saved.icons)
        assertTrue(CERT_MANAGER in saved.menu)
        assertEquals(KubernetesAction.entries.size - 1, saved.menu.size)
    }

    @Test
    fun keptApartFromTheOverviewsBar() {
        val bar = KubernetesAction.bar.default.toBar(FLOWS).toMenu(CHECKUP)
        assertEquals("NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,STORAGE,SERVICES,JOBS,EVENTS,API_ADDRESS,POLICIES,AUTOSCALING,VIRTUAL_MACHINES,CROSSPLANE,TAILSCALE,ARGO_ROLLOUTS,CLUSTER_LIFECYCLE,DATA_SERVICES,CERT_MANAGER,CASTAI,CAPACITY", bar.encode())
        assertEquals(bar, KubernetesAction.bar.parse(bar.encode()))
        assertFalse(bar.isDefault)
        // Another screen's names are unknown here: none of them is taken.
        val foreign = KubernetesAction.bar.parse("HEALTH,ETCD|METRICS")
        assertTrue(foreign.icons.isEmpty())
        assertEquals(KubernetesAction.entries, foreign.menu)
    }
}
