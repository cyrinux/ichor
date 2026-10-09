import XCTest
@testable import IchorCore

final class KubeBrowserTests: XCTestCase {
    private let resources = [
        KubeAPIResource(group: "cert-manager.io", resource: "certificates", kind: "Certificate", shortNames: ["cert", "certs"]),
        KubeAPIResource(resource: "pods", kind: "Pod", verbs: ["get", "list", "update"], shortNames: ["po"]),
        KubeAPIResource(group: "apps", resource: "deployments", kind: "Deployment", shortNames: ["deploy"]),
        KubeAPIResource(resource: "nodes", kind: "Node", namespaced: false, shortNames: ["no"]),
        KubeAPIResource(group: "batch", resource: "jobs", kind: "Job"),
        KubeAPIResource(group: "networking.k8s.io", resource: "ingresses", kind: "Ingress", shortNames: ["ing"]),
        KubeAPIResource(group: "argoproj.io", resource: "applications", kind: "Application", shortNames: ["app"]),
    ]

    func testDecodesResources() throws {
        let list = try TalosJSON.decode(KubeAPIResourceList.self, from: """
        {"resources":[{"group":"","version":"v1","resource":"secrets","kind":"Secret","namespaced":true,
          "verbs":["get","list","update"],"shortNames":null,"categories":[]}],"failed":["metrics.k8s.io/v1beta1"]}
        """)
        XCTAssertEqual(list.resources.count, 1)
        let secret = list.resources[0]
        XCTAssertTrue(secret.isSecret)
        XCTAssertTrue(secret.canUpdate)
        XCTAssertEqual(secret.shortNames, [])
        XCTAssertEqual(secret.groupVersion, "v1")
        XCTAssertEqual(list.failed, ["metrics.k8s.io/v1beta1"])
        XCTAssertEqual(try TalosJSON.decode(KubeAPIResourceList.self, from: "{}"), KubeAPIResourceList())
        XCTAssertEqual(KubeAPIResource(group: "apps", resource: "deployments", kind: "Deployment").groupVersion, "apps/v1")
        XCTAssertFalse(KubeAPIResource(resource: "events", kind: "Event", verbs: ["list", "update"]).canUpdate)
    }

    func testGroupsCoreAndAppsFirstThenBuiltInThenOthers() {
        let groups = groupAPIResources(resources).map(\.group)
        XCTAssertEqual(groups, ["", "apps", "batch", "networking.k8s.io", "argoproj.io", "cert-manager.io"])
        XCTAssertEqual(groupAPIResources(resources)[0].resources.map(\.kind), ["Node", "Pod"])
    }

    func testSearchesKindResourceShortNameAndGroup() {
        XCTAssertEqual(groupAPIResources(resources, query: "deploy").flatMap(\.resources).map(\.kind), ["Deployment"])
        XCTAssertEqual(groupAPIResources(resources, query: "ING").flatMap(\.resources).map(\.kind), ["Ingress"])
        XCTAssertEqual(groupAPIResources(resources, query: "certs").flatMap(\.resources).map(\.kind), ["Certificate"])
        XCTAssertEqual(groupAPIResources(resources, query: "argoproj").flatMap(\.resources).map(\.kind), ["Application"])
        XCTAssertEqual(groupAPIResources(resources, query: "po").flatMap(\.resources).map(\.kind), ["Pod"])
        XCTAssertEqual(groupAPIResources(resources, query: "  ").flatMap(\.resources).count, resources.count)
        XCTAssertTrue(groupAPIResources(resources, query: "zzz").isEmpty)
    }

    func testDecodesPage() throws {
        let page = try TalosJSON.decode(KubeResourcePage.self, from: """
        {"columns":[{"name":"Name","priority":0,"type":"string"},{"name":"Ready","priority":0,"type":"string"},
          {"name":"IP","priority":1,"type":"string"},{"name":"Age","priority":0,"type":"string"}],
         "rows":[{"name":"web-1","namespace":"apps","cells":["web-1","1/1","10.0.0.4","3d"],"created":1790000000},
                 {"name":"web-2","cells":["web-2","0/1","<none>",""],"created":0,"deleting":true}],
         "continue":"6162","remaining":12}
        """)
        XCTAssertEqual(page.rows.map(\.id), ["apps/web-1", "/web-2"])
        XCTAssertTrue(page.rows[1].deleting)
        XCTAssertEqual(page.page.continueToken, "6162")
        XCTAssertFalse(page.page.complete)
        XCTAssertEqual(page.page.remaining, 12)

        let last = try TalosJSON.decode(KubeResourcePage.self, from: #"{"columns":[],"rows":null,"continue":"","remaining":0}"#)
        XCTAssertTrue(last.page.complete)
        XCTAssertEqual(last.page.items, [])
    }

    func testColumnSelection() {
        let columns = [
            KubeResourceColumn(name: "Name"), KubeResourceColumn(name: "Namespace"), KubeResourceColumn(name: "Ready"),
            KubeResourceColumn(name: "Status"), KubeResourceColumn(name: "IP", priority: 1), KubeResourceColumn(name: "Age"),
        ]
        XCTAssertEqual(browserColumnIndices(columns, wide: false), [2, 3])
        XCTAssertEqual(browserColumnIndices(columns, wide: true), [2, 3, 4])
        XCTAssertTrue(hasWideColumns(columns))
        XCTAssertFalse(hasWideColumns(Array(columns.prefix(4))))

        let row = KubeResourceRow(name: "web", cells: ["web", "apps", "1/1", "", "<none>", "3d"])
        let cells = browserCells(row, columns: columns, indices: browserColumnIndices(columns, wide: true))
        XCTAssertEqual(cells.map(\.column.name), ["Ready"])
        XCTAssertEqual(cells.map(\.value), ["1/1"])
        // Fewer cells than columns: never out of range.
        XCTAssertTrue(browserCells(KubeResourceRow(name: "x", cells: ["x"]), columns: columns, indices: [2, 3]).isEmpty)
    }

    func testDateCells() {
        let now = Date(timeIntervalSince1970: 1_790_000_000)
        XCTAssertEqual(browserCellAge("2026-09-21T14:13:20Z", type: "date", now: now), 0)
        XCTAssertEqual(browserCellAge("2026-09-21T13:13:20Z", type: "date", now: now), 3_600)
        XCTAssertNil(browserCellAge("5d", type: "date", now: now))
        XCTAssertNil(browserCellAge("2026-09-21T13:13:20Z", type: "string", now: now))
    }

    func testFilterRows() {
        let rows = [KubeResourceRow(name: "web", namespace: "apps", cells: ["web", "Running"]),
                    KubeResourceRow(name: "db", namespace: "data", cells: ["db", "CrashLoopBackOff"])]
        XCTAssertEqual(filterResourceRows(rows, query: "crash").map(\.name), ["db"])
        XCTAssertEqual(filterResourceRows(rows, query: "APPS").map(\.name), ["web"])
        XCTAssertEqual(filterResourceRows(rows, query: "").count, 2)
    }

    func testTones() {
        XCTAssertEqual(kubeCellTone(column: "Status", value: "Running"), .good)
        XCTAssertEqual(kubeCellTone(column: "STATUS", value: "CrashLoopBackOff"), .bad)
        XCTAssertEqual(kubeCellTone(column: "Phase", value: "Pending"), .warn)
        XCTAssertEqual(kubeCellTone(column: "Status", value: "Init:0/2"), .warn)
        XCTAssertEqual(kubeCellTone(column: "Status", value: "Init:Error"), .bad)
        XCTAssertEqual(kubeCellTone(column: "Ready", value: "2/2"), .good)
        XCTAssertEqual(kubeCellTone(column: "Ready", value: "1/2"), .warn)
        XCTAssertEqual(kubeCellTone(column: "Ready", value: "0/0"), .neutral)
        XCTAssertEqual(kubeCellTone(column: "Ready", value: "True"), .good)
        XCTAssertEqual(kubeCellTone(column: "Image", value: "Running"), .neutral)
        XCTAssertEqual(kubeStatusTone("deployed"), .good)
        XCTAssertEqual(kubeStatusTone("pending-upgrade"), .warn)
        XCTAssertEqual(kubeStatusTone("whatever"), .neutral)
    }

    func testEditPreviewClassifiesLines() throws {
        let preview = try TalosJSON.decode(KubeEditPreview.self, from: """
        {"changed":true,"diff":"--- stored\\n+++ edited\\n@@ -1,3 +1,3 @@\\n spec:\\n-  replicas: 1\\n+  replicas: 2\\n+  paused: true\\n"}
        """)
        XCTAssertTrue(preview.changed)
        XCTAssertEqual(preview.lines.map(\.kind), [.hunk, .context, .removed, .added, .added])
        XCTAssertEqual(preview.lines[2].text, "  replicas: 1")
        XCTAssertEqual(preview.counts.added, 2)
        XCTAssertEqual(preview.counts.removed, 1)
        XCTAssertEqual(try TalosJSON.decode(KubeEditPreview.self, from: "{}"), KubeEditPreview())
        XCTAssertTrue(KubeEditPreview().lines.isEmpty)
    }

    func testHiddenSecretValues() {
        XCTAssertTrue(hasHiddenSecretValues("data:\n  password: <hidden, 12 characters>\n"))
        XCTAssertFalse(hasHiddenSecretValues("data:\n  password: cGFzcw==\n"))
    }

    func testEditConflict() {
        XCTAssertTrue(isKubeEditConflict("the object changed since it was opened: reload it and edit again"))
        XCTAssertFalse(isKubeEditConflict("Kubernetes API: permission denied"))
    }

    func testResourceRowNamespacesSortedWithoutClusterScoped() {
        let rows = [KubeResourceRow(name: "a", namespace: "web"), KubeResourceRow(name: "b"),
                    KubeResourceRow(name: "c", namespace: "apps"), KubeResourceRow(name: "d", namespace: "web")]
        XCTAssertEqual(resourceRowNamespaces(rows), ["apps", "web"])
        XCTAssertEqual(resourceRowNamespaces([]), [])
    }

    func testDecodesDeletePreview() throws {
        let preview = try TalosJSON.decode(KubeDeletePreview.self, from: """
        {"protected":true,"reason":"needed","clusterScoped":false,"finalizers":["example.com/hold"],
         "dependents":[{"kind":"Pod","namespace":"shop","name":"api-1"}],"moreDependents":2,"resourceVersion":"7"}
        """)
        XCTAssertTrue(preview.isProtected)
        XCTAssertTrue(preview.needsTypedName)
        XCTAssertEqual(preview.dependents, [KubeDeleteDependent(kind: "Pod", namespace: "shop", name: "api-1")])
        XCTAssertEqual(preview.moreDependents, 2)
        XCTAssertEqual(preview.resourceVersion, "7")

        let plain = try TalosJSON.decode(KubeDeletePreview.self, from: "{}")
        XCTAssertEqual(plain, KubeDeletePreview())
        XCTAssertFalse(plain.needsTypedName)
        XCTAssertTrue(KubeDeletePreview(clusterScoped: true).needsTypedName)
        XCTAssertEqual(KubeDeletePropagation.allCases.map(\.rawValue), ["Background", "Foreground", "Orphan"])
        XCTAssertTrue(isKubeDeleteConflict("the object changed since you looked at it: review it again before deleting"))
    }

    func testDecodesScale() throws {
        let list = try TalosJSON.decode(KubeAPIResourceList.self, from: """
        {"resources":[{"group":"argoproj.io","version":"v1alpha1","resource":"rollouts","kind":"Rollout","scalable":true},
          {"group":"","version":"v1","resource":"pods","kind":"Pod"}]}
        """)
        XCTAssertTrue(list.resources[0].scalable)
        XCTAssertFalse(list.resources[1].scalable)
        XCTAssertEqual(list.resources[0].scaleResource, "rollouts/scale")
        XCTAssertEqual(KubeAPIResource(group: "batch", resource: "jobs", kind: "Job", scalable: true).scaleResource, "jobs")

        let job = try TalosJSON.decode(KubeObjectScale.self, from: #"{"replicas":4,"current":1,"field":"parallelism"}"#)
        XCTAssertEqual(job, KubeObjectScale(replicas: 4, current: 1, field: "parallelism"))
        XCTAssertTrue(job.isParallelism)
        XCTAssertFalse(try TalosJSON.decode(KubeObjectScale.self, from: "{}").isParallelism)
    }
}
