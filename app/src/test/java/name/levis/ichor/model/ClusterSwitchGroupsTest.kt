package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class ClusterSwitchGroupsTest {
    @Test
    fun credentialsForOneClusterStayTogetherInFirstSeenOrder() {
        val admin = ContextSummary(name = "admin", clusterId = "talos-a", fingerprint = "admin-fp")
        val other = ContextSummary(name = "other", clusterId = "talos-b", fingerprint = "other-fp")
        val operator = ContextSummary(name = "operator", clusterId = "talos-a", fingerprint = "operator-fp")
        val reader = ContextSummary(name = "reader", clusterId = "talos-a", fingerprint = "reader-fp")

        assertEquals(listOf(listOf(admin, operator, reader), listOf(other)), clusterSwitchGroups(listOf(admin, other, operator, reader)))
    }

    @Test
    fun unknownClusterIdsDoNotMergeUnrelatedContexts() {
        val first = ContextSummary(name = "first")
        val second = ContextSummary(name = "second")
        assertEquals(listOf(listOf(first), listOf(second)), clusterSwitchGroups(listOf(first, second)))
    }
}
