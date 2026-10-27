package name.levis.ichor.ui.nav

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterRailTest {
    @Test
    fun everyRouteFamilyMapsToItsSection() {
        val cases = mapOf(
            Routes.OVERVIEW to RailSection.OVERVIEW,
            Routes.NODES to RailSection.NODES,
            Routes.NODE_TABS to RailSection.NODES,
            Routes.KUBE_NODES to RailSection.NODES,
            Routes.ALERTS to RailSection.ALERTS,
            Routes.WORKLOADS to RailSection.KUBERNETES,
            Routes.ARGO_CD to RailSection.ARGO_CD,
            Routes.ARGO_APP to RailSection.ARGO_CD,
            Routes.FLUX to RailSection.FLUX,
            Routes.FLUX_APP to RailSection.FLUX,
            Routes.DATA_SERVICES to RailSection.DATA_SERVICES,
            Routes.ACTIVITY to RailSection.ACTIVITY,
            Routes.SEARCH to RailSection.SEARCH,
            Routes.SETTINGS to RailSection.SETTINGS,
        )
        cases.forEach { (route, section) -> assertEquals(route, section, railSelection(route)) }
    }

    @Test
    fun unknownRoutesSelectNothing() {
        assertNull(railSelection(null))
        assertNull(railSelection(Routes.IMPORT))
        assertNull(railSelection("no-such-screen?x={x}"))
    }

    @Test
    fun theSectionsFollowTheClusterAndTheRole() {
        val talosAdmin = ContextSummary(name = "lab", roles = listOf("os:admin"))
        val talosReader = ContextSummary(name = "lab", roles = listOf("os:reader"))
        val kube = ContextSummary(name = "kube", kind = KIND_KUBE)
        assertEquals(RailSection.entries, railSections(talosAdmin))
        // A role without the Kubernetes API: no Kubernetes, GitOps or data services section.
        assertEquals(
            listOf(RailSection.OVERVIEW, RailSection.NODES, RailSection.ALERTS, RailSection.ACTIVITY, RailSection.SEARCH, RailSection.SETTINGS),
            railSections(talosReader),
        )
        assertEquals(RailSection.entries, railSections(kube))
    }

    @Test
    fun eachSectionOpensItsScreen() {
        // The Kubernetes nodes and the activity routes encode with android.net.Uri, absent from
        // JVM tests: their section mapping is covered by everyRouteFamilyMapsToItsSection.
        val talos = ContextSummary(name = "lab", roles = listOf("os:admin"))
        assertEquals(Routes.nodes(null), RailSection.NODES.route(talos))
        assertEquals(Routes.ARGO_CD, RailSection.ARGO_CD.route(talos))
        assertEquals(Routes.SETTINGS, RailSection.SETTINGS.route(talos))
        assertNull(RailSection.OVERVIEW.route(talos))
        // Every section a screen opens belongs back to that section.
        RailSection.entries.filter { it != RailSection.NODES && it != RailSection.ACTIVITY && it != RailSection.OVERVIEW && it != RailSection.ALERTS && it != RailSection.DATA_SERVICES }
            .forEach { assertEquals(it, railSelection(it.route(talos))) }
    }
}
