package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ArgoCDTest {
    // Shaped like KubeArgoCD's answer for the demo cluster (kube_argocd_demo.go), trimmed.
    private val json = """
        {"installed":true,"version":"v3.4.5","appSetsError":"","futureField":1,"apps":[
          {"namespace":"argocd","name":"demo-worker","project":"apps","owner":null,"level":"critical","health":"Degraded",
           "healthMessage":"Deployment \"worker\" exceeded its progress deadline","sync":"Synced","revision":"4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d",
           "sources":[{"repo":"https://git.example.com/gitops.git","path":"apps/worker","chart":"","targetRevision":"main"}],
           "destination":{"server":"https://kubernetes.default.svc","namespace":"demo"},"autoSync":{"enabled":true,"prune":false,"selfHeal":true},
           "syncOptions":["CreateNamespace=true"],"operation":{"phase":"Succeeded","done":3,"total":3,"wave":0,"waves":[0],"failed":[]},
           "conditions":null,"resources":[{"group":"apps","kind":"Deployment","namespace":"demo","name":"worker","sync":"Synced","health":"Degraded","wave":0,"syncResult":"Synced"}],
           "history":[{"id":12,"revision":"4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","targetRevision":"main","deployedAt":1000,"initiatedBy":"automated"}],
           "unhealthyPods":[{"namespace":"demo","name":"worker-7d9","status":"CrashLoopBackOff","node":"worker-2"}]},
          {"namespace":"argocd","name":"grafana","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"critical","icon":"grafana",
           "health":"Healthy","sync":"OutOfSync","revision":"8.5.2","sources":[{"repo":"https://grafana.github.io/helm-charts","chart":"grafana","targetRevision":"8.6.0"}],
           "destination":{"namespace":"monitoring"},"autoSync":{"enabled":true,"prune":true},"syncOptions":["ServerSideApply=true"],
           "operation":{"phase":"Failed","done":3,"total":4,"wave":0,"waves":[0],"failed":[{"kind":"Deployment","namespace":"monitoring","name":"grafana","message":"image: Required value"}]},
           "conditions":[{"type":"SyncError","message":"Failed sync attempt"}],
           "resources":[{"kind":"Secret","namespace":"monitoring","name":"grafana","sync":"Synced","wave":0,"syncResult":"Synced"},
                        {"group":"apps","kind":"Deployment","namespace":"monitoring","name":"grafana","sync":"OutOfSync","wave":0,"syncResult":"SyncFailed"}]},
          {"namespace":"argocd","name":"longhorn","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"warning","icon":"longhorn",
           "health":"Progressing","sync":"OutOfSync","revision":"1.12.0","sources":[{"chart":"longhorn","targetRevision":"1.12.1"}],
           "destination":{"namespace":"longhorn-system"},"autoSync":{"enabled":true},
           "operation":{"phase":"Running","done":5,"total":9,"wave":1,"waves":[-1,0,1,2],"failed":[]},
           "resources":[{"kind":"CustomResourceDefinition","name":"volumes.longhorn.io","sync":"Synced","wave":-1,"syncResult":"Synced"},
                        {"kind":"ConfigMap","namespace":"longhorn-system","name":"settings","sync":"Synced","wave":0,"syncResult":"Synced"},
                        {"group":"apps","kind":"DaemonSet","namespace":"longhorn-system","name":"longhorn-manager","sync":"OutOfSync","wave":1},
                        {"kind":"Job","namespace":"longhorn-system","name":"post-upgrade","sync":"OutOfSync","wave":2,"hook":true}]},
          {"namespace":"argocd","name":"cert-manager","project":"infra","level":"warning","icon":"cert-manager","health":"Healthy","sync":"OutOfSync",
           "revision":"v1.18.2","destination":{"namespace":"cert-manager"},"autoSync":{"enabled":false},
           "operation":{"phase":"Succeeded","done":5,"total":5,"waves":[0]},
           "resources":[{"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager","sync":"OutOfSync","wave":0},
                        {"kind":"ConfigMap","namespace":"cert-manager","name":"legacy","sync":"OutOfSync","prune":true}],
           "history":[{"id":9,"revision":"v1.18.2","chart":"cert-manager","targetRevision":"v1.18.2","deployedAt":3000},{"id":8,"revision":"v1.18.1","deployedAt":2000}]},
          {"namespace":"argocd","name":"cilium","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"ok","icon":"cilium",
           "health":"Healthy","sync":"Synced","destination":{"namespace":"kube-system"},"autoSync":{"enabled":true},
           "resources":[{"kind":"ConfigMap","name":"cilium-config","sync":"Synced"}]},
          {"namespace":"argocd","name":"nightly-reports","project":"apps","level":"idle","health":"Suspended","sync":"Synced","destination":{"namespace":"reports"},"autoSync":{"enabled":true}}],
         "appSets":[{"namespace":"argocd","name":"infra","level":"critical","apps":4,"conditions":[]}],
         "projects":[{"namespace":"argocd","name":"infra","description":"Cluster infrastructure","syncWindows":1}]}
    """.trimIndent()

    private val status = TalosJson.decodeFromString(ArgoStatus.serializer(), json)
    private fun app(name: String) = status.apps.first { it.name == name }

    @Test
    fun decodesTheGoAnswer() {
        assertTrue(status.installed)
        assertEquals(6, status.apps.size)
        val worker = app("demo-worker")
        assertEquals(ServiceHealth.CRITICAL, worker.serviceHealth)
        assertEquals(ArgoHealth.DEGRADED, worker.healthState)
        assertEquals(emptyList<ArgoCondition>(), worker.conditions) // null on the wire
        assertEquals("argocd/demo-worker", worker.key)
        assertEquals(ServiceHealth.CRITICAL, status.appSets.single().serviceHealth)
        assertEquals(1, status.projects.single().syncWindows)
    }

    @Test
    fun versionLabelPrefersTheChart() {
        assertEquals("grafana@8.6.0", app("grafana").versionLabel)
        assertEquals("4be1d0c", app("demo-worker").versionLabel)
        assertEquals("v1.18.2", app("cert-manager").versionLabel)
        assertEquals("v1.18.2", shortRevision("v1.18.2"))
        assertEquals(3000L, app("cert-manager").deployedAt)
        assertEquals(0L, app("cilium").deployedAt)
    }

    @Test
    fun runningAndRollbackRules() {
        assertTrue(app("longhorn").isRunning)
        assertFalse(app("grafana").isRunning)
        // Owned: no spec change, so no rollback either.
        assertFalse(app("grafana").canChangeSpec)
        assertFalse(app("grafana").canRollback)
        // Auto-sync on: Argo CD would sync straight back.
        assertTrue(app("demo-worker").canChangeSpec)
        assertFalse(app("demo-worker").canRollback)
        assertTrue(app("cert-manager").canRollback)
        val running = app("cert-manager").copy(operation = ArgoOperation(phase = "Running"))
        assertFalse(running.canRollback)
        assertTrue(app("grafana").serverSideApply)
        assertEquals(listOf("legacy"), app("cert-manager").pruneCandidates.map { it.name })
    }

    @Test
    fun filtersAndCounts() {
        val counts = status.apps.filterCounts()
        assertEquals(6, counts[ArgoFilter.ALL])
        assertEquals(2, counts[ArgoFilter.ATTENTION])
        assertEquals(3, counts[ArgoFilter.OUT_OF_SYNC])
        assertEquals(1, counts[ArgoFilter.PROGRESSING])
        assertEquals(1, counts[ArgoFilter.SYNCING])
        assertEquals(1, counts[ArgoFilter.AUTO_SYNC_OFF])
        assertEquals(listOf("grafana", "longhorn"), status.apps.filtered(ArgoFilter.OUT_OF_SYNC, "infra").filter { it.owner != null }.map { it.name })
        assertEquals(listOf("cert-manager"), status.apps.filtered(ArgoFilter.ALL, "CERT").map { it.name })
        assertEquals(listOf("grafana", "cert-manager"), status.apps.syncAllCandidates().map { it.name })
        assertEquals(mapOf(ServiceHealth.CRITICAL to 2, ServiceHealth.WARNING to 2, ServiceHealth.OK to 1, ServiceHealth.IDLE to 1), status.apps.levelCounts())
    }

    @Test
    fun sortsWorstFirstAndSummarises() {
        assertEquals(listOf("demo-worker", "grafana", "cert-manager", "longhorn", "cilium", "nightly-reports"), status.sortedApps.map { it.name })
        assertEquals(listOf("longhorn"), status.runningSyncs.map { it.name })
        assertEquals(listOf("demo-worker", "grafana", "cert-manager"), status.problemApps.map { it.name })
        assertFalse(status.allFine)
        assertTrue(ArgoStatus(apps = listOf(app("cilium"), app("nightly-reports"))).allFine)
    }

    @Test
    fun groupsWithTheUnnamedGroupLast() {
        val bySet = status.apps.grouped(ArgoGroupBy.APP_SET)
        assertEquals(listOf("infra", ""), bySet.map { it.first })
        assertEquals(3, bySet.first().second.size)
        assertEquals(listOf("apps", "infra"), status.apps.grouped(ArgoGroupBy.PROJECT).map { it.first })
        assertEquals(1, status.apps.grouped(ArgoGroupBy.NONE).size)
        assertEquals("cert-manager", status.apps.grouped(ArgoGroupBy.NAMESPACE).first().first)
    }

    @Test
    fun waveStepsFollowTheRunningSync() {
        val steps = app("longhorn").waveSteps()
        assertEquals(listOf(-1, 0, 1, 2), steps.map { it.wave })
        assertEquals(listOf(WaveState.DONE, WaveState.DONE, WaveState.CURRENT, WaveState.PENDING), steps.map { it.state })
        assertTrue(steps.last().resources.single().hook)
    }

    @Test
    fun waveStepsWithoutARunningSync() {
        assertEquals(listOf(WaveState.FAILED), app("grafana").waveSteps().map { it.state })
        assertEquals(listOf(WaveState.PENDING), app("cert-manager").waveSteps().map { it.state })
        assertEquals(listOf(WaveState.DONE), app("cilium").waveSteps().map { it.state })
        assertEquals(emptyList<WaveStep>(), app("nightly-reports").waveSteps())
    }

    @Test
    fun likelyCausePrefersADownNode() {
        val worker = app("demo-worker")
        assertEquals(ArgoCause.NodeDown("worker-2", "worker-7d9"), worker.likelyCause(setOf("worker-2")))
        assertEquals(ArgoCause.PodStatus("worker-7d9", "CrashLoopBackOff"), worker.likelyCause(emptySet()))
        val noPods = worker.copy(unhealthyPods = emptyList())
        assertEquals(ArgoCause.Message("Deployment \"worker\" exceeded its progress deadline"), noPods.likelyCause(emptySet()))
        assertEquals(ArgoCause.Message("image: Required value"), app("grafana").likelyCause(emptySet()))
        assertNull(app("cilium").likelyCause(setOf("worker-2")))
        // A healthy app whose sync failed: its namespace's crashing pods belong to another app.
        val failedSync = worker.copy(health = "Healthy", healthMessage = "", operation = app("grafana").operation)
        assertEquals(ArgoCause.Message("image: Required value"), failedSync.likelyCause(setOf("worker-2")))
    }

    @Test
    fun matchesInventoryApps() {
        val grafana = InventoryApp(id = "grafana", name = "Grafana", icon = "grafana", namespaces = listOf("monitoring"))
        assertEquals(listOf("grafana"), argoAppsFor(grafana, status).map { it.name })
        // By name, when nothing has the icon.
        val reports = InventoryApp(id = "reports-job", name = "Nightly-Reports", namespaces = listOf("other"))
        assertEquals(listOf("nightly-reports"), argoAppsFor(reports, status).map { it.name })
        // By namespace, never for plumbing.
        val worker = InventoryApp(id = "busybox", name = "BusyBox", namespaces = listOf("demo"))
        assertEquals(listOf("demo-worker"), argoAppsFor(worker, status).map { it.name })
        assertEquals(emptyList<ArgoApp>(), argoAppsFor(worker.copy(system = true), status))
        assertEquals(emptyList<ArgoApp>(), argoAppsFor(InventoryApp(id = "redis", name = "Redis"), status))
    }

    @Test
    fun badgesOnlyCriticalOrOutOfSync() {
        val inventory = listOf(
            InventoryApp(id = "grafana", name = "Grafana", icon = "grafana"),
            InventoryApp(id = "cert-manager", name = "cert-manager", icon = "cert-manager"),
            InventoryApp(id = "cilium", name = "Cilium", icon = "cilium"),
        )
        assertEquals(mapOf("grafana" to ServiceHealth.CRITICAL, "cert-manager" to ServiceHealth.WARNING), status.inventoryBadges(inventory))
    }

    @Test
    fun inventoryHintAndSyncOptions() {
        assertTrue(Inventory(apps = listOf(InventoryApp(id = ARGO_CD_CATALOG_ID, name = "Argo CD"))).hasArgoCD)
        assertFalse(Inventory(apps = listOf(InventoryApp(id = "argo-workflows", name = "Argo Workflows"))).hasArgoCD)
        val options = ArgoSyncOptions(prune = true, resources = listOf(ArgoResourceRef("apps", "Deployment", "ns", "web")), historyId = 3)
        val encoded = TalosJson.encodeToString(ArgoSyncOptions.serializer(), options)
        assertTrue(encoded, encoded.contains("\"prune\":true") && encoded.contains("\"historyId\":3") && encoded.contains("\"kind\":\"Deployment\""))
        assertEquals(2, ArgoOperation(wave = 0, waves = listOf(-1, 0, 1)).waveIndex)
        assertEquals(0.5f, ArgoOperation(done = 2, total = 4).progress)
    }
}
