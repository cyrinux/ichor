package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterLifecycleTest {
    // Go KubeClusterAPI's demo answer, trimmed to a few rows of each kind.
    private val raw = """
        {"installed":true,"version":"v1beta2","sidero":true,"talosControlPlane":true,"talosBootstrap":true,"truncated":false,
         "clusters":[
          {"namespace":"fleet","name":"old-lab","phase":"Failed","paused":false,"endpoint":"10.0.2.5:6443","infraKind":"MetalCluster",
           "controlPlaneKind":"TalosControlPlane","infrastructureReady":true,"controlPlaneReady":false,
           "controlPlane":{"desired":1,"ready":0,"available":0,"upToDate":0},"workers":{"desired":0,"ready":0,"available":0,"upToDate":0},
           "versions":["v1.33.4"],"conditions":[],"failureReason":"CreateError","failureMessage":"the control plane machine failed to provision",
           "health":"critical","reasons":["clusterFailed","controlPlaneDegraded"]},
          {"namespace":"fleet","name":"staging","phase":"Provisioning","endpoint":"10.0.1.5:6443",
           "controlPlane":{"desired":1,"ready":0},"workers":{"desired":4,"ready":2,"available":2,"upToDate":2},"versions":["v1.34.1"],
           "conditions":[{"type":"ControlPlaneInitialized","status":"False","reason":"NotInitialized","message":"waiting for the first control plane machine"}],
           "health":"warning","reasons":["clusterProvisioning","controlPlaneDegraded"]},
          {"namespace":"fleet","name":"prod-edge","phase":"Provisioned","controlPlane":null,"workers":{"desired":4,"ready":4},"health":"ok","reasons":[]}],
         "machines":[
          {"namespace":"fleet","name":"old-lab-cp-q9d4m","cluster":"old-lab","controlPlane":true,"phase":"Failed","node":"","version":"v1.34.1",
           "addresses":["10.0.2.10"],"failureReason":"CreateError","failureMessage":"server srv-b2 did not boot from disk","health":"critical","reasons":["machineFailed"]},
          {"namespace":"fleet","name":"staging-cp-x7k2p","cluster":"staging","controlPlane":true,"phase":"Provisioning","stuck":true,
           "lastUpdated":1791900000000,"health":"warning","reasons":["machineStuck"]},
          {"namespace":"fleet","name":"prod-edge-workers-6b8d-k2m9x","cluster":"prod-edge","controlPlane":false,"deployment":"prod-edge-workers",
           "phase":"Running","node":"prod-edge-w-1","health":"ok","reasons":[]},
          {"namespace":"fleet","name":"prod-edge-cp-4fj8s","cluster":"prod-edge","controlPlane":true,"phase":"Running","node":"prod-edge-cp-1","health":"ok","reasons":[]}],
         "machineDeployments":[
          {"namespace":"fleet","name":"staging-workers","cluster":"staging","phase":"ScalingUp","desired":4,"ready":2,"available":2,"upToDate":2,
           "unavailable":2,"version":"v1.34.1","health":"warning","reasons":["workersShort"]}],
         "servers":[
          {"name":"srv-b2","hostname":"rack2-u02","accepted":true,"ready":false,"inUse":true,"power":"off","serverClasses":["any"],
           "cpu":"AMD EPYC 7313P, 16 cores","memoryBytes":137438953472,"health":"critical","reasons":["serverPoweredOff","serverNotReady"]},
          {"name":"srv-b1","hostname":"rack2-u01","accepted":false,"power":"unknown","health":"warning","reasons":["serverNotAccepted"]}],
         "serverClasses":[{"name":"any","available":1,"inUse":3},{"name":"large","available":0,"inUse":1}],
         "counts":{"clusters":3,"machines":9,"servers":5,"ok":12,"warning":4,"critical":3}}
    """.trimIndent()

    private val report = TalosJson.decodeFromString(ClusterLifecycle.serializer(), raw)

    @Test fun decodesEveryKind() {
        assertTrue(report.installed && report.sidero && report.talosControlPlane)
        assertEquals(listOf("old-lab", "staging", "prod-edge"), report.clusters.map { it.name })
        assertEquals(ServiceHealth.from("critical"), report.clusters[0].serviceHealth)
        assertEquals(1, report.clusters[0].controlPlane?.desired)
        assertNull(report.clusters[2].controlPlane)
        assertTrue(report.machines[1].stuck)
        assertEquals("staging-workers", report.machineDeployments.single().name)
        assertEquals("off", report.servers[0].power)
        assertFalse(report.servers[1].accepted)
        assertEquals(3, report.serverClasses[0].inUse)
        assertEquals(3, report.counts.critical)
        assertFalse(report.isEmpty)
    }

    @Test fun problemIsTheFailureElseTheConditions() {
        assertEquals("CreateError: the control plane machine failed to provision", report.clusters[0].problem)
        assertEquals("ControlPlaneInitialized: waiting for the first control plane machine", report.clusters[1].problem)
        assertEquals("", report.clusters[2].problem)
    }

    @Test fun machinesGroupByClusterControlPlaneFirst() {
        val groups = report.machinesByCluster
        assertEquals(listOf("old-lab", "staging", "prod-edge"), groups.map { it.first })
        assertEquals(listOf("prod-edge-cp-4fj8s", "prod-edge-workers-6b8d-k2m9x"), groups[2].second.map { it.name })
    }

    @Test fun stuckSinceFallsBackToCreation() {
        assertEquals(1791900000000, report.machines[1].stuckSince)
        assertEquals(5L, CapiMachine(createdAt = 5).stuckSince)
        assertEquals(0L, CapiMachine().stuckSince)
    }

    @Test fun clustersOfTheSameNameInTwoNamespacesStayApart() {
        val r = ClusterLifecycle(machines = listOf(CapiMachine(namespace = "a", name = "m1", cluster = "c"), CapiMachine(namespace = "b", name = "m2", cluster = "c")))
        assertEquals(listOf(listOf("m1"), listOf("m2")), r.machinesByCluster.map { g -> g.second.map { it.name } })
    }

    @Test fun refsAreNamespacedForClusterAPIAndClusterScopedForSidero() {
        val machine = report.ref(LifecycleKind.MACHINE, "fleet", "staging-cp-x7k2p")
        assertEquals(CAPI_GROUP, machine.group)
        assertEquals("v1beta2", machine.version)
        assertEquals("fleet", machine.namespace)
        val server = report.ref(LifecycleKind.SERVER, "ignored", "srv-b2")
        assertEquals(SIDERO_GROUP, server.group)
        assertEquals(SIDERO_VERSION, server.version)
        assertEquals("", server.namespace)
        assertEquals(CAPI_DEFAULT_VERSION, ClusterLifecycle().ref(LifecycleKind.CLUSTER, "fleet", "x").version)
    }

    @Test fun notInstalledDecodesEmpty() {
        val absent = TalosJson.decodeFromString(ClusterLifecycle.serializer(), """{"installed":false}""")
        assertFalse(absent.installed)
        assertTrue(absent.isEmpty)
    }
}
