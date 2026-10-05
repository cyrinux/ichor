package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FluxTest {
    // Shaped like KubeFlux's answer for the demo cluster (kube_flux_demo.go), trimmed.
    private val json = """
        {"installed":true,"version":"v2.7.0","helmError":"","sourcesError":"","futureField":1,"apps":[
          {"kind":"Kustomization","namespace":"flux-system","name":"apps","level":"critical","ready":"False","reason":"HealthCheckFailed",
           "message":"health check failed after 5m0s","reconciling":false,"pending":false,"stalled":false,"suspended":false,
           "owner":{"kind":"Kustomization","namespace":"flux-system","name":"flux-system"},
           "source":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"},"sourceURL":"ssh://git@git.example.com/homelab/fleet.git",
           "path":"./apps/homelab","chart":"","chartVersion":"","targetNamespace":"","interval":"10m","prune":true,
           "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","attemptedRevision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d",
           "dependsOn":["flux-system/infra-controllers"],"failures":0,
           "conditions":[{"type":"Ready","status":"False","reason":"HealthCheckFailed","message":"health check failed after 5m0s","at":1000}],
           "resources":[{"group":"","kind":"Service","namespace":"demo","name":"worker"},{"group":"apps","kind":"Deployment","namespace":"demo","name":"worker"},
                        {"group":"","kind":"ConfigMap","namespace":"demo","name":"worker-config"},{"group":"apps","kind":"Deployment","namespace":"demo","name":"hello-ichor"}],
           "history":[],"unhealthyPods":[{"namespace":"demo","name":"worker-7d9","status":"CrashLoopBackOff","node":"worker-2"}],"reconciledAt":1000},
          {"kind":"HelmRelease","namespace":"ingress-nginx","name":"ingress-nginx","level":"critical","icon":"ingress-nginx","ready":"False",
           "reason":"RetriesExceeded","message":"Failed to upgrade after 3 attempt(s)","stalled":true,"failures":3,
           "source":{"kind":"HelmRepository","namespace":"flux-system","name":"ingress-nginx"},"chart":"ingress-nginx","chartVersion":"4.13.3",
           "revision":"4.12.1","attemptedRevision":"4.13.3","dependsOn":null,"resources":null,
           "history":[{"version":4,"chartVersion":"4.13.3","appVersion":"1.13.3","status":"failed","deployedAt":3000},
                      {"version":3,"chartVersion":"4.12.1","appVersion":"1.12.1","status":"deployed","deployedAt":2000}]},
          {"kind":"Kustomization","namespace":"flux-system","name":"infra-controllers","level":"warning","ready":"Unknown","reason":"Progressing",
           "reconciling":true,"path":"./infrastructure/controllers","revision":"main@sha1:91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e"},
          {"kind":"HelmRelease","namespace":"monitoring","name":"kube-prometheus-stack","level":"warning","icon":"prometheus","ready":"Unknown",
           "reason":"Progressing","reconciling":true,"chart":"kube-prometheus-stack","chartVersion":"77.x","revision":"77.5.0","attemptedRevision":"77.6.1"},
          {"kind":"Kustomization","namespace":"flux-system","name":"flux-system","level":"ok","icon":"flux","ready":"True","reason":"ReconciliationSucceeded",
           "path":"./clusters/homelab","revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"},
          {"kind":"HelmRelease","namespace":"demo","name":"podinfo","level":"ok","ready":"True","chart":"podinfo","revision":"6.9.2","attemptedRevision":"6.9.2",
           "source":{"kind":"OCIRepository","namespace":"flux-system","name":"podinfo"}},
          {"kind":"HelmRelease","namespace":"cache","name":"redis","level":"idle","ready":"True","suspended":true,"chart":"redis","chartVersion":"21.2.x","revision":"21.2.5"}],
         "sources":[
          {"kind":"HelmRepository","namespace":"flux-system","name":"ingress-nginx","level":"critical","url":"https://kubernetes.github.io/ingress-nginx",
           "ready":"False","reason":"IndexationFailed","interval":"1h","fetchedAt":500,"revision":"sha256:7e1a0f4c2b9d","apps":1},
          {"kind":"GitRepository","namespace":"flux-system","name":"flux-system","level":"ok","url":"ssh://git@git.example.com/homelab/fleet.git","ref":"main",
           "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","ready":"True","pending":true,"apps":3},
          {"kind":"OCIRepository","namespace":"flux-system","name":"podinfo","level":"ok","url":"oci://ghcr.io/stefanprodan/charts/podinfo","ref":"6.9.x",
           "revision":"6.9.2@sha256:3b1f9c0a8e7d","ready":"True","apps":1}]}
    """.trimIndent()

    private val status = TalosJson.decodeFromString(FluxStatus.serializer(), json)
    private fun app(name: String) = status.apps.first { it.name == name }

    @Test
    fun decodesTheGoAnswer() {
        assertTrue(status.installed)
        assertEquals("v2.7.0", status.version)
        assertEquals(7, status.apps.size)
        assertEquals(3, status.sources.size)
        val apps = app("apps")
        assertEquals(ServiceHealth.CRITICAL, apps.serviceHealth)
        assertEquals("Kustomization/flux-system/apps", apps.key)
        assertEquals("flux-system", apps.owner?.name)
        assertEquals(listOf("flux-system/infra-controllers"), apps.dependsOn)
        assertEquals("CrashLoopBackOff", apps.unhealthyPods.single().status)
        // null on the wire
        assertEquals(emptyList<String>(), app("ingress-nginx").dependsOn)
        assertEquals(emptyList<FluxResource>(), app("ingress-nginx").resources)
        assertNull(app("redis").source)
        assertEquals(3, status.sources.first { it.kind == "GitRepository" }.apps)
    }

    @Test
    fun statesFromLevelSuspensionAndWork() {
        assertEquals(FluxState.FAILING, app("apps").state)
        assertEquals(FluxState.FAILING, app("ingress-nginx").state)
        assertEquals(FluxState.RECONCILING, app("infra-controllers").state)
        assertEquals(FluxState.READY, app("flux-system").state)
        assertEquals(FluxState.SUSPENDED, app("redis").state)
        // A pending reconcile on a ready object reads as reconciling until handled.
        assertEquals(FluxState.RECONCILING, app("podinfo").copy(pending = true).state)
        // Suspended wins over a failure: Flux ignores the object.
        assertEquals(FluxState.SUSPENDED, app("apps").copy(suspended = true).state)
    }

    @Test
    fun actionRules() {
        assertTrue(app("ingress-nginx").canForce)
        assertFalse(app("apps").canForce)
        assertFalse(app("redis").canForce)
        assertFalse(app("redis").canReconcile)
        assertTrue(app("apps").canReconcile)
        assertTrue(app("ingress-nginx").revisionDiffers)
        assertFalse(app("podinfo").revisionDiffers)
        assertEquals(listOf("reconcile", "reconcileWithSource", "suspend", "resume", "force", "reset"), FluxAction.entries.map { it.wire })
    }

    @Test
    fun shortRevisions() {
        assertEquals("main@4be1d0c", app("apps").versionLabel)
        assertEquals("ingress-nginx@4.12.1", app("ingress-nginx").versionLabel)
        assertEquals("6.9.2@3b1f9c0", shortFluxRevision("6.9.2@sha256:3b1f9c0a8e7d"))
        assertEquals("7e1a0f4", shortFluxRevision("sha256:7e1a0f4c2b9d"))
        assertEquals("4be1d0c", shortFluxRevision("4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"))
        assertEquals("v1.2.3", shortFluxRevision("v1.2.3"))
        assertEquals("", shortFluxRevision(""))
        assertEquals("redis@21.2.x", app("redis").copy(revision = "").versionLabel)
    }

    @Test
    fun filtersAndCounts() {
        val counts = status.apps.fluxFilterCounts()
        assertEquals(7, counts[FluxFilter.ALL])
        assertEquals(2, counts[FluxFilter.FAILING])
        assertEquals(2, counts[FluxFilter.RECONCILING])
        assertEquals(1, counts[FluxFilter.SUSPENDED])
        assertEquals(3, counts[FluxFilter.KUSTOMIZATIONS])
        assertEquals(4, counts[FluxFilter.HELM_RELEASES])
        assertEquals(listOf("kube-prometheus-stack"), status.apps.fluxFiltered(FluxFilter.HELM_RELEASES, "PROM").map { it.name })
        assertEquals(listOf("apps"), status.apps.fluxFiltered(FluxFilter.FAILING, "homelab").map { it.name })
        assertEquals(
            mapOf(FluxState.FAILING to 2, FluxState.RECONCILING to 2, FluxState.READY to 2, FluxState.SUSPENDED to 1),
            status.apps.stateCounts(),
        )
    }

    @Test
    fun sortsWorstFirstAndSummarises() {
        assertEquals(listOf("apps", "ingress-nginx", "infra-controllers", "kube-prometheus-stack", "flux-system", "podinfo", "redis"), status.sortedApps.map { it.name })
        assertEquals(listOf("ingress-nginx", "flux-system", "podinfo"), status.sortedSources.map { it.name })
        assertEquals(listOf("apps", "ingress-nginx"), status.failingApps.map { it.name })
        assertTrue(status.anyBusy)
        assertFalse(status.allFine)
        val calm = FluxStatus(apps = listOf(app("flux-system"), app("redis")), sources = status.sources.filter { !it.isBusy })
        assertTrue(calm.allFine)
        assertFalse(calm.anyBusy)
        assertEquals("podinfo", status.sourceOf(app("podinfo"))?.name)
        assertNull(status.sourceOf(app("redis")))
    }

    @Test
    fun inventoryGroupedByKind() {
        val groups = app("apps").resourcesByKind
        assertEquals(listOf("ConfigMap", "Deployment", "Service"), groups.map { it.first })
        assertEquals(listOf("hello-ichor", "worker"), groups[1].second.map { it.name })
        assertTrue(groups[1].second.all { it.restartable })
        assertFalse(groups[0].second.single().restartable)
    }

    @Test
    fun inventoryHint() {
        assertTrue(Inventory(apps = listOf(InventoryApp(id = FLUX_CATALOG_ID, name = "Flux"))).hasFlux)
        assertFalse(Inventory(apps = listOf(InventoryApp(id = "miniflux", name = "Miniflux"))).hasFlux)
    }
}
