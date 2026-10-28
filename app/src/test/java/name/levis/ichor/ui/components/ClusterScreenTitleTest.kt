package name.levis.ichor.ui.components

import name.levis.ichor.model.ClusterLabels
import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterScreenTitleTest {
    private val lab = ContextSummary(name = "admin@lab", fingerprint = "fp-lab")
    private val edge = ContextSummary(name = "admin@edge", fingerprint = "fp-edge")
    private val labels = ClusterLabels(mapOf("fp-lab" to "Lab"))

    @Test
    fun oneClusterNeedsNoLine() {
        assertNull(clusterTitleLine(listOf(lab), lab, labels))
        assertEquals("node-a", clusterTitleLine(listOf(lab), lab, labels, "node-a"))
    }

    @Test
    fun severalClustersNameTheActiveOneByItsLabel() {
        assertEquals("Lab", clusterTitleLine(listOf(lab, edge), lab, labels))
        assertEquals("admin@edge", clusterTitleLine(listOf(lab, edge), edge, labels))
    }

    @Test
    fun theSubtitleComesFirst() {
        assertEquals("node-a · Lab", clusterTitleLine(listOf(lab, edge), lab, labels, "node-a"))
        assertEquals("Lab", clusterTitleLine(listOf(lab, edge), lab, labels, " "))
    }

    @Test
    fun noActiveClusterOnlyTheSubtitle() {
        assertNull(clusterTitleLine(listOf(lab, edge), null, labels))
    }
}
