package name.levis.talosmobile.ui

import name.levis.talosmobile.ui.machineconfig.matchingLines
import org.junit.Assert.assertEquals
import org.junit.Test

class MachineConfigTest {
    private val yaml = "machine:\n  type: controlplane\n  token: '******'\ncluster:\n  clusterName: home"

    @Test
    fun blankQueryKeepsEveryLine() {
        assertEquals(5, matchingLines(yaml, "  ").size)
    }

    @Test
    fun filtersCaseInsensitively() {
        assertEquals(listOf("  token: '******'"), matchingLines(yaml, "TOKEN"))
        assertEquals(listOf("cluster:", "  clusterName: home"), matchingLines(yaml, "cluster"))
    }
}
