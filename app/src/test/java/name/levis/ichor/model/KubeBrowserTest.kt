package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeBrowserTest {
    private fun res(kind: String, group: String = "", resource: String = kind.lowercase() + "s", short: List<String> = emptyList()) =
        ApiResource(group = group, version = "v1", resource = resource, kind = kind, namespaced = true, shortNames = short)

    private val resources = listOf(
        res("Certificate", "cert-manager.io"),
        res("Deployment", "apps", short = listOf("deploy")),
        res("Pod", short = listOf("po")),
        res("Ingress", "networking.k8s.io", resource = "ingresses", short = listOf("ing")),
        res("Job", "batch"),
        res("HorizontalPodAutoscaler", "autoscaling", short = listOf("hpa")),
        res("ConfigMap", short = listOf("cm")),
        res("Application", "argoproj.io"),
        res("StorageClass", "storage.k8s.io"),
    )

    @Test
    fun groupsPutCoreAndBuiltInsBeforeCrds() {
        val groups = groupResources(resources, "").map { it.group }
        assertEquals(listOf("", "apps", "batch", "networking.k8s.io", "autoscaling", "storage.k8s.io", "argoproj.io", "cert-manager.io"), groups)
        assertEquals(listOf("ConfigMap", "Pod"), groupResources(resources, "").first().resources.map { it.kind })
    }

    @Test
    fun searchMatchesKindResourceGroupAndExactShortName() {
        assertEquals(listOf("Deployment"), groupResources(resources, "deploy").flatMap { it.resources }.map { it.kind })
        assertEquals(listOf("HorizontalPodAutoscaler"), groupResources(resources, "HPA").flatMap { it.resources }.map { it.kind })
        assertEquals(listOf("Application"), groupResources(resources, "argoproj").flatMap { it.resources }.map { it.kind })
        assertEquals(listOf("Ingress"), groupResources(resources, "ingresses").flatMap { it.resources }.map { it.kind })
        assertTrue(groupResources(resources, "nothing-like-it").isEmpty())
    }

    @Test
    fun builtInGroups() {
        assertTrue(isBuiltInGroup(""))
        assertTrue(isBuiltInGroup("policy"))
        assertTrue(isBuiltInGroup("rbac.authorization.k8s.io"))
        assertFalse(isBuiltInGroup("cilium.io"))
    }

    @Test
    fun editableNeedsTheUpdateVerb() {
        assertTrue(ApiResource(verbs = listOf("get", "list", "update")).editable)
        assertFalse(ApiResource(verbs = listOf("get", "list")).editable)
    }

    private val columns = listOf(
        ResourceColumn("Name", 0, "string"),
        ResourceColumn("Ready", 0, "string"),
        ResourceColumn("Status", 0, "string"),
        ResourceColumn("Age", 0, "date"),
        ResourceColumn("IP", 1, "string"),
        ResourceColumn("Nominated Node", 1, "string"),
    )

    @Test
    fun rowFieldsSkipOwnColumnsEmptyValuesAndWideOnesUnlessAsked() {
        val row = ResourceRow("web-1", "default", listOf("web-1", "1/1", "Running", "3d", "10.0.0.4", "<none>"), columns = columns)
        assertEquals(listOf(RowField("Ready", "1/1"), RowField("Status", "Running")), rowFields(row, wide = false))
        assertEquals(listOf(RowField("Ready", "1/1"), RowField("Status", "Running"), RowField("IP", "10.0.0.4")), rowFields(row, wide = true))
        assertTrue(hasWideColumns(columns))
        assertFalse(hasWideColumns(columns.filter { it.priority == 0 }))
    }

    @Test
    fun rowFieldsToleratesMissingCells() {
        val row = ResourceRow("a", cells = listOf("a", "0/1"), columns = columns)
        assertEquals(listOf(RowField("Ready", "0/1")), rowFields(row, wide = true))
    }

    @Test
    fun statusCellsAreToned() {
        assertEquals(CellTone.OK, cellTone("Status", "Running"))
        assertEquals(CellTone.BAD, cellTone("STATUS", "CrashLoopBackOff"))
        assertEquals(CellTone.WARN, cellTone("Phase", "Pending"))
        assertEquals(CellTone.WARN, cellTone("Status", "Init:0/2"))
        assertEquals(CellTone.NONE, cellTone("Ready", "Running"))
        assertEquals(CellTone.NONE, cellTone("Status", "Whatever"))
    }

    @Test
    fun pageDecodesWithItsColumnsAndEndsWithoutContinue() {
        val json = """{"columns":[{"name":"Name","priority":0,"type":"string"}],"rows":[{"name":"a","cells":["a"],"created":10}],"continue":"","remaining":0}"""
        val page = TalosJson.decodeFromString(ResourcePageJson.serializer(), json).toPage()
        assertTrue(page.complete)
        assertEquals("a", page.items.single().name)
        assertEquals("Name", page.items.single().columns.single().name)
        val more = ResourcePageJson(continueToken = "next", remaining = 12).toPage()
        assertFalse(more.complete)
        assertEquals("next", more.continueToken)
    }

    @Test
    fun rowsFilterOnNameNamespaceAndCells() {
        val rows = listOf(ResourceRow("web", "shop", listOf("web", "Running")), ResourceRow("db", "data", listOf("db", "Pending")))
        assertEquals(listOf("db"), rows.filteredRows("pend").map { it.name })
        assertEquals(listOf("web"), rows.filteredRows("SHOP").map { it.name })
        assertEquals(2, rows.filteredRows(" ").size)
    }

    @Test
    fun editPreviewClassifiesDiffLines() {
        val diff = "--- stored\n+++ edited\n@@ -1,3 +1,3 @@\n metadata:\n-  replicas: 1\n+  replicas: 2\n"
        val lines = KubeEditPreview(changed = true, diff = diff).lines
        assertEquals(
            listOf(DiffLine.Kind.HUNK, DiffLine.Kind.CONTEXT, DiffLine.Kind.REMOVED, DiffLine.Kind.ADDED),
            lines.map { it.kind },
        )
        assertEquals("  replicas: 2", lines.last().text)
        assertTrue(KubeEditPreview().lines.isEmpty())
    }

    @Test
    fun containerPortsAreReadFromThePodYaml() {
        val yaml = """
            apiVersion: v1
            kind: Pod
            spec:
              containers:
              - name: web
                image: nginx
                ports:
                - containerPort: 8080
                  name: http
                  protocol: TCP
                - containerPort: 53
                  protocol: UDP
                - name: metrics
                  containerPort: 9090
              - name: sidecar
                ports:
                  - containerPort: 8080
                  - containerPort: 15000
                env:
                - name: PORT
                  value: "1"
            status:
              phase: Running
        """.trimIndent()
        assertEquals(
            listOf(ContainerPort(8080, "http"), ContainerPort(9090, "metrics"), ContainerPort(15000)),
            containerPorts(yaml),
        )
        assertTrue(containerPorts("kind: Pod\nspec:\n  containers:\n  - name: a\n").isEmpty())
    }

    @Test
    fun forwardUrlIsHttpOnLoopback() {
        assertEquals("http://127.0.0.1:43123", forwardUrl("127.0.0.1:43123"))
    }

    @Test
    fun helmStatusesAndSearch() {
        assertEquals(CellTone.OK, helmStatusTone("deployed"))
        assertEquals(CellTone.BAD, helmStatusTone("failed"))
        assertEquals(CellTone.WARN, helmStatusTone("pending-upgrade"))
        assertEquals(CellTone.NONE, helmStatusTone("superseded"))
        val releases = listOf(HelmRelease("traefik", "kube-system", chart = "traefik"), HelmRelease("db", "data", chart = "postgresql"))
        assertEquals(listOf("db"), releases.filteredReleases("postgres").map { it.name })
    }

    @Test
    fun followBufferKeepsTheNewestLinesWithStableNumbers() {
        val buffer = FollowBuffer().append(listOf("a", "b", "c"), cap = 4).append(listOf("d", "e"), cap = 4)
        assertEquals(listOf("b", "c", "d", "e"), buffer.lines.map { it.text })
        assertEquals(listOf(1L, 2L, 3L, 4L), buffer.lines.map { it.seq })
        assertEquals(5L, buffer.nextSeq)
        assertEquals("b\nc\nd\ne", buffer.text)
        assertEquals(buffer, buffer.append(emptyList()))
        assertEquals(MAX_FOLLOW_LINES, FollowBuffer().append(List(MAX_FOLLOW_LINES + 10) { "x" }).lines.size)
    }

    @Test
    fun helmRollbackPlanDecodesWithDefaults() {
        val json = """
            {"namespace":"web","name":"site","from":3,"to":2,"fromChart":"site-1.1.0","toChart":"site-1.0.0",
             "fromAppVersion":"1.1","toAppVersion":"1.0","future":true,
             "changes":[{"action":"update","kind":"Deployment","namespace":"web","name":"site","error":"","extra":1},
                        {"action":"delete","kind":"ConfigMap","namespace":"web","name":"new","error":"forbidden"}],
             "unchanged":4,"fluxOwner":"flux-system/site","blockers":null}
        """.trimIndent()
        val plan = TalosJson.decodeFromString(HelmRollbackPlan.serializer(), json)
        assertEquals(3, plan.from)
        assertEquals(2, plan.to)
        assertEquals(listOf("update", "delete"), plan.changes.map { it.action })
        assertEquals("forbidden", plan.changes[1].error)
        assertEquals(4, plan.unchanged)
        assertEquals("flux-system/site", plan.fluxOwner)
        assertTrue(plan.canRun)
        assertFalse(plan.copy(blockers = listOf("pending")).canRun)
        assertEquals(HelmRollbackPlan(), TalosJson.decodeFromString(HelmRollbackPlan.serializer(), "{}"))
    }

    @Test
    fun deletePreviewDecodesAndAsksForTheName() {
        val json = """{"protected":true,"reason":"needed","clusterScoped":false,"finalizers":["example.com/hold"],
            "dependents":[{"kind":"Pod","namespace":"shop","name":"api-1"}],"moreDependents":2,"resourceVersion":"7"}"""
        val p = TalosJson.decodeFromString(KubeDeletePreview.serializer(), json)
        assertTrue(p.isProtected)
        assertTrue(p.needsTypedName)
        assertEquals(listOf(KubeDeleteDependent("Pod", "shop", "api-1")), p.dependents)
        assertEquals("7", p.resourceVersion)

        val plain = TalosJson.decodeFromString(KubeDeletePreview.serializer(), "{}")
        assertFalse(plain.needsTypedName)
        assertTrue(plain.copy(clusterScoped = true).needsTypedName)
        assertEquals(listOf("Background", "Foreground", "Orphan"), DeletePropagation.entries.map { it.api })
    }
}
