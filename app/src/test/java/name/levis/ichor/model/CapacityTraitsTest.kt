package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CapacityTraitsTest {

    private fun w(cls: String, priority: Int, qos: String, noClass: Boolean = false) =
        CapacityWorkload(kind = "Deployment", namespace = "demo", name = "$cls-$priority-$qos", priorityClassName = cls, priority = priority, qos = qos, noPriorityClass = noClass)

    private val list = listOf(
        w("apps-optional", 10, "Burstable"),
        w("apps-optional", 10, "Burstable"),
        w("apps-optional", 10, "Guaranteed"),
        w("web-high", 1000, "Burstable"),
        w("", 0, "BestEffort", noClass = true),
    )

    @Test
    fun majorityIsTheValueMoreThanHalfShare() {
        val m = list.majority()
        assertEquals("apps-optional", m.priorityClass)
        assertEquals(10, m.priority)
        assertEquals(QosClass.BURSTABLE, m.qos)
    }

    @Test
    fun noMajorityWithoutMoreThanHalf() {
        val m = listOf(w("a", 1, "Burstable"), w("b", 2, "Guaranteed")).majority()
        assertNull(m.priorityClass)
        assertNull(m.priority)
        assertNull(m.qos)
    }

    @Test
    fun aSingleWorkloadHidesNothing() {
        val one = listOf(w("a", 1, "Burstable"))
        val t = one[0].traitsBeside(one.majority())
        assertTrue(t.priorityClass && t.priority && t.qos)
    }

    @Test
    fun rowsShowOnlyWhatDiffersFromTheMajority() {
        val m = list.majority()
        assertFalse(list[0].traitsBeside(m).any)
        assertEquals(WorkloadTraits(priorityClass = false, priority = false, qos = true), list[2].traitsBeside(m))
        assertEquals(WorkloadTraits(priorityClass = true, priority = true, qos = false), list[3].traitsBeside(m))
        assertEquals(WorkloadTraits(priorityClass = true, priority = true, qos = true), list[4].traitsBeside(m))
    }

    @Test
    fun noClassIsHiddenWhenMostHaveNone() {
        val none = listOf(w("", 0, "BestEffort", noClass = true), w("", 0, "BestEffort", noClass = true), w("x", 5, "Burstable"))
        val m = none.majority()
        assertEquals("", m.priorityClass)
        assertFalse(none[0].traitsBeside(m).priorityClass)
        assertTrue(none[2].traitsBeside(m).priorityClass)
    }

    @Test
    fun unknownQosIsNeverShown() {
        val odd = w("a", 1, "SomethingNew")
        assertFalse(odd.traitsBeside(CapacityMajority()).qos)
    }

    @Test
    fun rolesLineLeadsWithControlPlane() {
        assertNull(nodeRolesLine(emptyList()))
        assertEquals("control-plane", nodeRolesLine(listOf("control-plane")))
        assertEquals("storage, mail", nodeRolesLine(listOf("storage", "mail")))
        assertEquals("control-plane · storage, mail", nodeRolesLine(listOf("storage", "control-plane", "mail")))
    }

    @Test
    fun overcommittedNodesSortFirstKeepingOrder() {
        val nodes = listOf(CapacityNode(name = "a"), CapacityNode(name = "b", overcommitted = true), CapacityNode(name = "c"))
        assertEquals(listOf("b", "a", "c"), nodes.overcommittedFirst().map { it.name })
    }
}
