package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ObjectImageScanTest {

    private fun ref(group: String, resource: String, kind: String, namespace: String = "shop", name: String = "web") =
        KubeObjectRef(group, "v1", resource, kind, namespace, name, editable = true)

    @Test
    fun scanModeByKind() {
        assertEquals(ObjectScanMode.POD, KubeObjectRef.pod("shop", "web-0").scanMode)
        assertEquals(ObjectScanMode.WORKLOAD, ref("apps", "deployments", "Deployment").scanMode)
        assertEquals(ObjectScanMode.WORKLOAD, ref("apps", "statefulsets", "StatefulSet").scanMode)
        assertEquals(ObjectScanMode.WORKLOAD, ref("apps", "daemonsets", "DaemonSet").scanMode)
        // KubeWorkloadsNamed reads neither: no scan offered for them yet.
        assertNull(KubeObjectRef.job("shop", "migrate").scanMode)
        assertNull(KubeObjectRef.cronJob("shop", "nightly").scanMode)
        assertNull(KubeObjectRef.service("shop", "web").scanMode)
        assertNull(ref("", "configmaps", "ConfigMap").scanMode)
        // A CRD that happens to be called deployments is not a workload.
        assertNull(ref("example.com", "deployments", "Deployment").scanMode)
    }

    @Test
    fun scanIdKeysByKindNamespaceAndName() {
        assertEquals("object:Pod:shop/web-0", KubeObjectRef.pod("shop", "web-0").scanId)
        assertEquals("object:Deployment:shop/web", ref("apps", "deployments", "Deployment").scanId)
        assertEquals(objectScanId("StatefulSet", "db", "pg"), ref("apps", "statefulsets", "StatefulSet", "db", "pg").scanId)
    }

    @Test
    fun scanLabelOfObjectIdsOnly() {
        assertEquals("Deployment shop/web", objectScanLabel(objectScanId("Deployment", "shop", "web")))
        assertEquals("Pod shop/web-0", objectScanLabel("object:Pod:shop/web-0"))
        assertNull(objectScanLabel("grafana"))
        assertNull(objectScanLabel(systemImagesScanId("10.0.0.2")))
        assertNull(objectScanLabel("object:"))
        assertNull(objectScanLabel("object:Pod"))
    }

    @Test
    fun scanTargets() {
        val pod = KubeObjectRef.pod("shop", "web-0")
        assertEquals(listOf(RoutePod("shop", "web-0")), pod.scanPods)
        assertEquals(WorkloadRef("Deployment", "shop", "web"), ref("apps", "deployments", "Deployment").scanWorkload)
    }
}
