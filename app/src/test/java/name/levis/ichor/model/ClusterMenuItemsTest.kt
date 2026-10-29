package name.levis.ichor.model

import name.levis.ichor.model.ClusterMenuItem.ADD_NODE
import name.levis.ichor.model.ClusterMenuItem.ADD_TO_HOME
import name.levis.ichor.model.ClusterMenuItem.INCIDENT_SUMMARY
import name.levis.ichor.model.ClusterMenuItem.MANAGE
import name.levis.ichor.model.ClusterMenuItem.RUNBOOKS
import name.levis.ichor.model.ClusterMenuItem.SHARE_LINK
import name.levis.ichor.model.ClusterMenuItem.SWITCH
import name.levis.ichor.model.ClusterMenuItem.UPGRADE_CLUSTER
import name.levis.ichor.model.ClusterMenuItem.UPDATES
import name.levis.ichor.model.ClusterMenuItem.UPGRADE_KUBERNETES
import org.junit.Assert.assertEquals
import org.junit.Test

class ClusterMenuItemsTest {

    private val share = listOf(INCIDENT_SUMMARY, SHARE_LINK, ADD_TO_HOME)

    @Test
    fun talosAdminGetsEveryGroupInOrder() {
        val admin = ContextSummary(name = "admin@lab", roles = listOf("os:admin"))
        assertEquals(
            listOf(listOf(SWITCH, MANAGE), listOf(UPGRADE_CLUSTER, UPGRADE_KUBERNETES, ADD_NODE, UPDATES, RUNBOOKS), share),
            clusterMenuGroups(admin, canSwitch = true),
        )
    }

    @Test
    fun talosReaderCannotUpgrade() {
        val reader = ContextSummary(name = "reader@lab", roles = listOf("os:reader"))
        assertEquals(listOf(ADD_NODE, UPDATES, RUNBOOKS), clusterMenuGroups(reader, canSwitch = true)[1])
    }

    @Test
    fun omniUpgradesAndAddsANode() {
        val omni = ContextSummary(name = "omni-lab", omni = true)
        assertEquals(
            listOf(listOf(MANAGE), listOf(UPGRADE_CLUSTER, UPGRADE_KUBERNETES, ADD_NODE, UPDATES, RUNBOOKS), share),
            clusterMenuGroups(omni, canSwitch = false),
        )
    }

    @Test
    fun kubeconfigClusterOperatesWithRunbooksOnly() {
        val kube = ContextSummary(name = "kind-dev", kind = KIND_KUBE)
        assertEquals(
            listOf(listOf(SWITCH, MANAGE), listOf(UPDATES, RUNBOOKS), share),
            clusterMenuGroups(kube, canSwitch = true),
        )
    }

    @Test
    fun noActiveClusterHasNoIncidentSummary() {
        assertEquals(
            listOf(listOf(MANAGE), listOf(RUNBOOKS), listOf(SHARE_LINK, ADD_TO_HOME)),
            clusterMenuGroups(null, canSwitch = false),
        )
    }
}
