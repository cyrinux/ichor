package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeGroupsTest {
    private val groups = NodeGroupsReport(
        provider = NODE_GROUPS_EKS,
        installed = true,
        groups = listOf(
            ManagedNodeGroupRow(name = "batch", status = "DEGRADED", desired = 4, max = 4, atMax = true, issues = listOf(NodeGroupIssue("AsgInstanceLaunchFailures", "no capacity"))),
            ManagedNodeGroupRow(name = "spot", status = "UPDATING", capacityType = "SPOT", updating = true, instanceTypes = listOf("m7i.large")),
            ManagedNodeGroupRow(name = "system", status = NODE_GROUP_ACTIVE, desired = 3, min = 2, max = 6),
        ),
    )

    @Test
    fun theAutoscalingAnswerNeverCarriesNodeGroups() {
        val report = TalosJson.decodeFromString(AutoscalingReport.serializer(), """{"hpa":{"installed":true},"nodeGroups":{"installed":true}}""")
        assertFalse(report.nodeGroups.installed)
    }

    @Test
    fun groupsAtMaxOrWithAnIssueComeFirstInNeedsAttention() {
        val problems = AutoscalingReport(nodeGroups = groups).problems()
        assertEquals(listOf("batch", "spot"), problems.map { it.name })
        assertTrue(problems.all { it.group == AUTOSCALING_GROUP_NODE_GROUPS })
        assertEquals(AUTOSCALE_AT_MAX, problems[0].state)
        assertEquals("no capacity", problems[0].message)
        assertEquals(AUTOSCALE_NOT_READY, problems[1].state)
        assertEquals("UPDATING", problems[1].message)
    }

    @Test
    fun aHealthyGroupNeedsNoAttention() {
        assertFalse(groups.groups.last().needsAttention)
        assertTrue(groups.groups.first().needsAttention)
    }

    @Test
    fun theSearchKeepsGroupsByNameOrInstanceTypeUnderEveryNamespace() {
        val report = AutoscalingReport(nodeGroups = groups)
        assertEquals(listOf("spot"), report.filtered(null, "m7i").nodeGroups.groups.map { it.name })
        assertEquals(listOf("system"), report.filtered(null, "sys").nodeGroups.groups.map { it.name })
        // Cluster-level: a namespace filter keeps every group.
        assertEquals(3, report.filtered("shop", "").nodeGroups.groups.size)
    }

    @Test
    fun sizeProblemsMirrorTheGoRefusals() {
        assertEquals(NodeGroupSizeProblem.MIN_NEGATIVE, nodeGroupSizeProblem(1, -1, 3))
        assertEquals(NodeGroupSizeProblem.MAX_TOO_LOW, nodeGroupSizeProblem(0, 0, 0))
        assertEquals(NodeGroupSizeProblem.MIN_ABOVE_MAX, nodeGroupSizeProblem(3, 4, 3))
        assertEquals(NodeGroupSizeProblem.DESIRED_OUTSIDE, nodeGroupSizeProblem(1, 2, 4))
        assertEquals(NodeGroupSizeProblem.DESIRED_OUTSIDE, nodeGroupSizeProblem(5, 2, 4))
        assertEquals(null, nodeGroupSizeProblem(0, 0, 4))
        assertEquals(null, nodeGroupSizeProblem(4, 2, 4))
    }

    @Test
    fun updateStatusMapping() {
        assertTrue(NodeGroupUpdate(id = "u", status = NODE_GROUP_UPDATE_IN_PROGRESS).inProgress)
        assertEquals(null, NodeGroupUpdate(id = "u", status = NODE_GROUP_UPDATE_IN_PROGRESS).failure)
        assertEquals(null, NodeGroupUpdate(id = "u", status = NODE_GROUP_UPDATE_SUCCESSFUL).failure)
        val failed = NodeGroupUpdate(id = "u", status = "Failed", errors = listOf(NodeGroupIssue(code = "X", message = "no capacity")))
        assertEquals("no capacity", failed.failure)
        // No error message: the status itself says it ended.
        assertEquals("Cancelled", NodeGroupUpdate(id = "u", status = "Cancelled").failure)
    }

    @Test
    fun onlyActiveOrDegradedGroupsChangeSize() {
        assertTrue(ManagedNodeGroupRow(name = "a", status = NODE_GROUP_ACTIVE).canChangeSize)
        assertTrue(ManagedNodeGroupRow(name = "b", status = NODE_GROUP_DEGRADED, atMax = true).canChangeSize)
        assertFalse(ManagedNodeGroupRow(name = "c", status = "UPDATING", updating = true).canChangeSize)
        assertFalse(ManagedNodeGroupRow(name = "d", status = "CREATING").canChangeSize)
    }

    @Test
    fun clusterVersionAndBehindDecodeWithDefaults() {
        val old = TalosJson.decodeFromString(NodeGroupsReport.serializer(), """{"groups":[{"name":"a"}]}""")
        assertEquals("", old.clusterVersion)
        assertFalse(old.groups.single().behind)
        val report = TalosJson.decodeFromString(
            NodeGroupsReport.serializer(),
            """{"clusterVersion":"1.34","groups":[{"name":"gpu","version":"1.33","behind":true}]}""",
        )
        assertEquals("1.34", report.clusterVersion)
        assertTrue(report.groups.single().behind)
    }

    @Test
    fun upgradeChoices() {
        val behind = ManagedNodeGroupRow(name = "a", status = NODE_GROUP_ACTIVE, version = "1.33", behind = true)
        assertEquals(listOf(NodeGroupUpgradeChoice.Kubernetes("1.34"), NodeGroupUpgradeChoice.LatestImage), behind.upgradeChoices("1.34"))
        assertEquals(listOf("1.34", ""), behind.upgradeChoices("1.34").map { it.version })
        // The control plane's version unknown: only the node image.
        assertEquals(listOf(NodeGroupUpgradeChoice.LatestImage), behind.upgradeChoices(""))
        val current = ManagedNodeGroupRow(name = "b", status = NODE_GROUP_ACTIVE, version = "1.34")
        assertEquals(listOf(NodeGroupUpgradeChoice.LatestImage), current.upgradeChoices("1.34"))
        val custom = ManagedNodeGroupRow(name = "c", status = NODE_GROUP_ACTIVE, amiType = NODE_GROUP_AMI_CUSTOM, behind = true)
        assertEquals(listOf(NodeGroupUpgradeChoice.Kubernetes("1.34")), custom.upgradeChoices("1.34"))
        assertTrue(groups.groups[1].copy(behind = true).upgradeChoices("1.34").isEmpty())
    }

    @Test
    fun aPodEvictionFailureIsNamed() {
        val eviction = NodeGroupIssue(NODE_GROUP_POD_EVICTION_FAILURE, "Reached max retries", listOf("ip-1"))
        val failed = NodeGroupUpdate(id = "v", status = "Failed", type = NODE_GROUP_UPDATE_VERSION, errors = listOf(eviction))
        assertTrue(failed.versionUpdate)
        assertEquals(listOf("ip-1"), failed.podEvictionFailure?.resourceIds)
        assertEquals(null, NodeGroupUpdate(id = "u", status = "Failed", type = "ConfigUpdate").podEvictionFailure)
    }
}
