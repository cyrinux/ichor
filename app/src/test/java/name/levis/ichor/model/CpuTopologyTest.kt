package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CpuTopologyTest {

    @Test
    fun decodesGoJson() {
        val json = """
            {"numa":[{"id":0,"cpus":"0-7","cpuCount":8,"memoryBytes":34359738368},{"id":1,"cpus":"8-15","cpuCount":8,"memoryBytes":34359738368}],
             "cpuScaling":[{"policy":"policy0","cpus":"0-7","driver":"intel_pstate","governor":"performance","energyPreference":"performance",
               "minKhz":800000,"maxKhz":5100000,"coreType":"performance","configuredBy":"performance-cores"},
              {"policy":"policy8","cpus":"8-15","driver":"intel_pstate","governor":"powersave","energyPreference":"",
               "minKhz":800000,"maxKhz":3000000,"coreType":"","configuredBy":""}],
             "errors":{}}
        """.trimIndent()
        val hw = TalosJson.decodeFromString(NodeHardware.serializer(), json)
        assertEquals(listOf(NumaNode(0, "0-7", 8, 34_359_738_368), NumaNode(1, "8-15", 8, 34_359_738_368)), hw.numa)
        assertEquals(
            CpuScalingPolicy("policy0", "0-7", "intel_pstate", "performance", "performance", 800_000, 5_100_000, "performance", "performance-cores"),
            hw.cpuScaling.first(),
        )
        assertTrue(hw.showsNuma)
        assertTrue(hw.showsCpuScaling)
        assertTrue(hw.powersaveWhileThrottled(throttled = true))
        assertFalse(hw.powersaveWhileThrottled(throttled = false))
    }

    @Test
    fun anOlderNodeOrASingleNodeShowsNeitherCard() {
        // A Talos 1.14 node, or a core before these fields: no lists at all.
        val old = TalosJson.decodeFromString(NodeHardware.serializer(), """{"errors":{}}""")
        assertFalse(old.showsNuma)
        assertFalse(old.showsCpuScaling)
        assertFalse(NodeHardware(numa = listOf(NumaNode(0, "0-3", 4))).showsNuma)
        assertTrue(NodeHardware(errors = mapOf(HardwareSection.CPU_SCALING to "unavailable")).showsCpuScaling)
    }
}
