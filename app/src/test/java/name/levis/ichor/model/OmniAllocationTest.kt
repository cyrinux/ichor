package name.levis.ichor.model

import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OmniAllocationTest {
    @Test
    fun decodesMachineSets() {
        val got = TalosJson.decodeFromString(
            OmniMachineSets.serializer(),
            """{"cluster":"lab","machineSets":[
               {"id":"lab-control-planes","role":"controlplane","allocation":{"className":"","type":"none","count":0},"matchLabels":[]},
               {"id":"lab-workers","role":"worker","allocation":{"className":"small","type":"static","count":2},
                "matchLabels":["omni.sidero.dev/arch = amd64"]}]}""",
        )
        assertEquals("lab", got.cluster)
        assertTrue(got.machineSets[0].isControlPlane)
        assertFalse(got.machineSets[1].isControlPlane)
        assertEquals(OmniAllocation("small", "static", 2), got.machineSets[1].allocation)
        assertEquals(listOf("omni.sidero.dev/arch = amd64"), got.machineSets[1].matchLabels)
    }

    @Test
    fun decodesMachines() {
        val got = TalosJson.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(OmniMachine.serializer()),
            """[{"id":"aaaaaaaa-1111","hostname":"talos-new","addresses":["192.0.2.7"],"talosVersion":"v1.11.2",
                 "connected":true,"maintenance":true,"labels":{"ichor/role":"worker"}},
                {"id":"bbbbbbbb-2222","hostname":"","addresses":[],"talosVersion":"","connected":false,"maintenance":false,"labels":{}}]""",
        )
        assertEquals("talos-new", got[0].name)
        assertTrue(got[0].maintenance)
        assertEquals("worker", got[0].labels["ichor/role"])
        assertEquals("bbbbbbbb-2222", got[1].name)
    }

    @Test
    fun labelsCarryTheRoleAndWhatWasTyped() {
        assertEquals("""{"ichor/role":"worker"}""", omniLabelsJson("worker", ""))
        assertEquals(
            """{"ichor/role":"controlplane","rack":"b2","spare":""}""",
            omniLabelsJson("controlplane", " rack = b2 , spare,, "),
        )
        assertEquals("""{"ichor/role":"mine"}""", omniLabelsJson("worker", "ichor/role=mine"))
    }

    @Test
    fun anAllocationHasItsOwnTimeline() {
        val request = JoinRequest(
            address = "", template = "", role = "worker", hostname = "talos-new", disk = "", staticAddress = "",
            omni = true, omniAllocate = OmniAllocateSpec("aaaaaaaa-1111", "lab-workers"),
        )
        assertEquals(JoinPhase.omniAllocateRun, request.phases)
        val steps = joinTimeline(
            listOf(JoinProgress("allocated", "bound", 1), JoinProgress("booting", "booting", 2)),
            finished = false,
            failed = false,
            phases = request.phases,
        )
        assertEquals(listOf(StepStatus.DONE, StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING), steps.map { it.status })
    }
}
