package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class K8sUpgradeTest {
    @Test
    fun planGroupsStepsAndCountsChanges() {
        val plan = TalosJson.decodeFromString(
            K8sUpgradePlan.serializer(),
            """{"from":"1.34.0","to":"1.35.1","supported":true,"supportedRange":"1.30–1.35","talosVersion":"v1.12.0",
               "steps":[
                 {"kind":"controlplane","node":"10.0.0.1","hostname":"cp-1","component":"apiserver",
                  "image":"registry.k8s.io/kube-apiserver:v1.35.1","current":"registry.k8s.io/kube-apiserver:v1.34.0","changed":true},
                 {"kind":"kubelet","node":"10.0.0.3","hostname":"w-1","component":"kubelet",
                  "image":"ghcr.io/siderolabs/kubelet:v1.35.1","current":"ghcr.io/siderolabs/kubelet:v1.35.1","changed":false}
               ],
               "deprecatedApis":[{"api":"flowcontrol.apiserver.k8s.io/v1beta3","removedIn":"1.32","severity":"critical"}],
               "blockers":[],"warnings":[]}""",
        )
        assertEquals(listOf("cp-1"), plan.controlPlaneSteps.map { it.name })
        assertEquals(listOf("w-1"), plan.kubeletSteps.map { it.name })
        assertEquals(1, plan.changes)
        assertTrue(plan.canStart)
        assertEquals("v1.34.0", plan.steps.first().currentTag)
        assertEquals("critical", plan.deprecatedApis.single().severity)
    }

    @Test
    fun onlyVersionsInRangeAreAllowed() {
        val choice = K8sVersionChoice(from = "1.34.2", supportedRange = "1.30–1.35", lo = 30, hi = 35)
        assertTrue(k8sVersionAllowed(choice, "1.34.3"))
        assertTrue(k8sVersionAllowed(choice, "1.35.0"))
        assertFalse(k8sVersionAllowed(choice, "1.34.2")) // the current one
        assertFalse(k8sVersionAllowed(choice, "1.33.9")) // a downgrade
        assertFalse(k8sVersionAllowed(choice, "1.36.0")) // two minors up, and out of range
        assertFalse(k8sVersionAllowed(choice.copy(hi = 34), "1.35.0")) // Talos does not support it
        assertFalse(k8sVersionAllowed(choice, "1.35"))
        assertFalse(k8sVersionAllowed(choice, "latest"))
    }
}
