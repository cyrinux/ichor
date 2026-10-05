import XCTest
@testable import IchorCore

final class ArgoCDTests: XCTestCase {
    // Shaped like KubeArgoCD's demo answer (go/ichorgo/kube_argocd_demo.go): a degraded app
    // whose pod crash-loops on a node that is down, a failed sync on an owned app, a sync
    // running through its waves, a drifting app with auto-sync paused and a prune pending.
    private let json = #"""
    {"installed":true,"version":"v3.4.5","appSetsError":"","apps":[
     {"namespace":"argocd","name":"demo-worker","project":"apps","owner":null,"level":"critical","health":"Degraded",
      "healthMessage":"Deployment \"worker\" exceeded its progress deadline","sync":"Synced","revision":"4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d",
      "refreshing":"","sources":[{"repo":"https://git.example.com/homelab/gitops.git","path":"apps/worker","chart":"","targetRevision":"main"}],
      "destination":{"server":"https://kubernetes.default.svc","name":"","namespace":"demo"},"autoSync":{"enabled":true,"prune":false,"selfHeal":true},
      "syncOptions":["CreateNamespace=true"],
      "operation":{"phase":"Succeeded","message":"successfully synced","startedAt":1000,"finishedAt":41000,"initiatedBy":"automated","revision":"4be1d0c",
       "retryCount":0,"dryRun":false,"done":3,"total":3,"wave":0,"waves":[0],"failed":[]},
      "conditions":[],"resources":[
       {"group":"","kind":"ConfigMap","namespace":"demo","name":"worker-config","sync":"Synced","health":"","healthMessage":"","wave":0,"hook":false,"prune":false,"syncResult":"Synced"},
       {"group":"apps","kind":"Deployment","namespace":"demo","name":"worker","sync":"Synced","health":"Degraded","wave":0,"syncResult":"Synced"}],
      "history":[{"id":12,"revision":"4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","targetRevision":"main","chart":"","deployedAt":5000,"initiatedBy":"automated"}],
      "images":["busybox:1.37"],"externalURLs":[],"reconciledAt":9000,
      "unhealthyPods":[{"namespace":"demo","name":"worker-7f9c","status":"CrashLoopBackOff","healthy":false,"ready":0,"containers":1,"restarts":14,"node":"worker-2","owner":"ReplicaSet/worker-7f","created":1,"images":["busybox:1.37"]}]},
     {"namespace":"argocd","name":"grafana","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"critical","icon":"grafana",
      "health":"Healthy","sync":"OutOfSync","revision":"8.5.2","sources":[{"repo":"https://grafana.github.io/helm-charts","chart":"grafana","targetRevision":"8.6.0"}],
      "destination":{"namespace":"monitoring"},"autoSync":{"enabled":true,"prune":true},"syncOptions":["ServerSideApply=true"],
      "operation":{"phase":"Failed","message":"one or more objects failed to apply","startedAt":1000,"finishedAt":2000,"done":3,"total":4,"waves":[0],
       "failed":[{"kind":"Deployment","namespace":"monitoring","name":"grafana","message":"spec.template.spec.containers[0].image: Required value"}]},
      "conditions":[{"type":"SyncError","message":"Failed sync attempt to 8.6.0"}],
      "resources":[{"kind":"Service","namespace":"monitoring","name":"grafana","sync":"Synced","health":"Healthy","syncResult":"Synced"},
                   {"group":"apps","kind":"Deployment","namespace":"monitoring","name":"grafana","sync":"OutOfSync","health":"Healthy","syncResult":"SyncFailed"}],
      "history":[{"id":31,"revision":"8.5.2","targetRevision":"8.5.2","chart":"grafana","deployedAt":7000}]},
     {"namespace":"argocd","name":"longhorn","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"warning","icon":"longhorn",
      "health":"Progressing","healthMessage":"Waiting for rollout to finish","sync":"OutOfSync","revision":"1.12.0",
      "sources":[{"repo":"https://charts.longhorn.io","chart":"longhorn","targetRevision":"1.12.1"}],"destination":{"namespace":"longhorn-system"},
      "autoSync":{"enabled":true,"prune":true},"syncOptions":["ServerSideApply=true"],
      "operation":{"phase":"Running","message":"waiting for healthy state","startedAt":1000,"done":5,"total":9,"wave":1,"waves":[-1,0,1,2],"failed":[]},
      "resources":[
       {"group":"apiextensions.k8s.io","kind":"CustomResourceDefinition","name":"volumes.longhorn.io","sync":"Synced","wave":-1,"syncResult":"Synced"},
       {"kind":"ConfigMap","namespace":"longhorn-system","name":"longhorn-default-setting","sync":"Synced","wave":0,"syncResult":"Synced"},
       {"group":"apps","kind":"DaemonSet","namespace":"longhorn-system","name":"longhorn-manager","sync":"OutOfSync","health":"Progressing","wave":1},
       {"group":"apps","kind":"Deployment","namespace":"longhorn-system","name":"longhorn-ui","sync":"OutOfSync","wave":2},
       {"group":"batch","kind":"Job","namespace":"longhorn-system","name":"longhorn-post-upgrade","sync":"OutOfSync","wave":2,"hook":true}]},
     {"namespace":"argocd","name":"cert-manager","project":"infra","level":"warning","icon":"cert-manager","health":"Healthy","sync":"OutOfSync",
      "revision":"v1.18.2","sources":[{"repo":"https://charts.jetstack.io","chart":"cert-manager","targetRevision":"v1.19.0"}],
      "destination":{"namespace":"cert-manager"},"syncOptions":["CreateNamespace=true","ServerSideApply=true"],
      "operation":{"phase":"Succeeded","done":5,"total":5,"waves":[0],"failed":[]},
      "resources":[
       {"group":"apps","kind":"Deployment","namespace":"cert-manager","name":"cert-manager","sync":"OutOfSync","health":"Healthy","wave":0},
       {"kind":"ConfigMap","namespace":"cert-manager","name":"cert-manager-legacy","sync":"OutOfSync","prune":true}],
      "history":[{"id":9,"revision":"v1.18.2","chart":"cert-manager","deployedAt":3000},{"id":8,"revision":"v1.18.1","chart":"cert-manager","deployedAt":2000}]},
     {"namespace":"argocd","name":"cilium","project":"infra","owner":{"kind":"ApplicationSet","name":"infra"},"level":"ok","icon":"cilium",
      "health":"Healthy","sync":"Synced","revision":"1.18.2","sources":[{"chart":"cilium","targetRevision":"1.18.2"}],"destination":{"namespace":"kube-system"},
      "autoSync":{"enabled":true},"resources":[{"kind":"ConfigMap","namespace":"kube-system","name":"cilium-config","sync":"Synced"}]},
     {"namespace":"argocd","name":"nightly-reports","project":"apps","level":"idle","health":"Suspended","sync":"Synced","destination":{"namespace":"reports"},
      "autoSync":{"enabled":true},"futureField":1}],
     "appSets":[{"namespace":"argocd","name":"infra","level":"critical","apps":4,"conditions":[
       {"type":"ErrorOccurred","status":"False","message":"Successfully generated parameters"},
       {"type":"ResourcesUpToDate","status":"True","message":"ApplicationSet up to date"}]},
      {"namespace":"argocd","name":"broken","level":"critical","apps":0,"conditions":[{"type":"ErrorOccurred","status":"True","message":"repo not found"}]}],
     "projects":[{"namespace":"argocd","name":"apps","description":"Self-hosted applications","syncWindows":0},
                 {"namespace":"argocd","name":"infra","description":"Cluster infrastructure","syncWindows":1}]}
    """#

    private var status: ArgoStatus { get throws { try TalosJSON.decode(ArgoStatus.self, from: json) } }

    private func app(_ name: String) throws -> ArgoApp {
        try XCTUnwrap(try status.apps.first { $0.name == name })
    }

    func testDecodesTheGoAnswer() throws {
        let s = try status
        XCTAssertTrue(s.installed)
        XCTAssertEqual(s.version, "v3.4.5")
        XCTAssertEqual(s.apps.count, 6)
        let worker = try app("demo-worker")
        XCTAssertEqual(worker.level, .critical)
        XCTAssertEqual(worker.health, .degraded)
        XCTAssertNil(worker.owner)
        XCTAssertNil(worker.resources[0].health)
        XCTAssertEqual(worker.resources[1].health, .degraded)
        XCTAssertEqual(worker.unhealthyPods.first?.node, "worker-2")
        XCTAssertEqual(worker.id, "argocd/demo-worker")
        let grafana = try app("grafana")
        XCTAssertEqual(grafana.owner, ArgoOwner(kind: "ApplicationSet", name: "infra"))
        XCTAssertEqual(grafana.operation?.phase, .failed)
        XCTAssertTrue(grafana.conditions[0].isError)
        let reports = try app("nightly-reports")
        XCTAssertEqual(reports.level, .idle)
        XCTAssertNil(reports.operation)
        XCTAssertEqual(s.appSets.map(\.error), [nil, "repo not found"])
        XCTAssertEqual(s.projects[1].syncWindows, 1)
    }

    func testCustomIconReachesTheTile() throws {
        let app = try TalosJSON.decode(ArgoApp.self, from: #"{"name":"homelab","iconUrl":"https://a.example/h.png"}"#)
        XCTAssertEqual(app.iconURL, "https://a.example/h.png")
        XCTAssertEqual(app.iconApp.iconSource(remoteIcons: true), .url(URL(string: "https://a.example/h.png")!))
        XCTAssertEqual(app.iconApp.iconSource(remoteIcons: false), .monogram)
    }

    func testNotInstalledAnswer() throws {
        let none = try TalosJSON.decode(ArgoStatus.self, from: #"{"installed":false}"#)
        XCTAssertFalse(none.installed)
        XCTAssertTrue(none.apps.isEmpty)
        XCTAssertEqual(none.worst, .ok)
        XCTAssertTrue(none.allCalm)
    }

    func testWhatAnAppAllows() throws {
        let worker = try app("demo-worker")
        XCTAssertTrue(worker.canChangeSpec)
        XCTAssertFalse(worker.canRollback, "auto-sync on")
        let cert = try app("cert-manager")
        XCTAssertTrue(cert.canRollback)
        XCTAssertTrue(cert.canSync)
        let longhorn = try app("longhorn")
        XCTAssertTrue(longhorn.isRunning)
        XCTAssertFalse(longhorn.canSync)
        XCTAssertTrue(longhorn.canTerminate)
        XCTAssertFalse(longhorn.canChangeSpec)
        XCTAssertFalse(longhorn.canRollback)
        XCTAssertTrue(try app("grafana").lastSyncFailed)
    }

    func testCountsAndFilters() throws {
        let s = try status
        XCTAssertEqual(s.count(.all), 6)
        XCTAssertEqual(s.count(.degraded), 1)
        XCTAssertEqual(s.count(.outOfSync), 3)
        XCTAssertEqual(s.count(.progressing), 1)
        XCTAssertEqual(s.count(.syncing), 1)
        XCTAssertEqual(s.count(.autoSyncOff), 1)
        XCTAssertEqual(s.count(.failed), 1)
        XCTAssertEqual(s.running.map(\.name), ["longhorn"])
        XCTAssertEqual(s.problems.map(\.name), ["demo-worker", "grafana", "cert-manager", "longhorn"])
        XCTAssertEqual(s.worst, .critical)
        XCTAssertFalse(s.allCalm)
        XCTAssertEqual(s.healthCounts.map(\.health), [.healthy, .progressing, .suspended, .degraded])
        XCTAssertEqual(s.healthCounts.map(\.count), [3, 1, 1, 1])

        XCTAssertEqual(filterArgoApps(s.apps, filter: .outOfSync, query: "").map(\.name), ["grafana", "cert-manager", "longhorn"])
        XCTAssertEqual(filterArgoApps(s.apps, filter: .all, query: "jetstack").map(\.name), ["cert-manager"])
        XCTAssertEqual(filterArgoApps(s.apps, filter: .all, query: "MONITORING").map(\.name), ["grafana"])
        XCTAssertTrue(filterArgoApps(s.apps, filter: .failed, query: "cilium").isEmpty)
        XCTAssertEqual(syncAllCandidates(s.apps).map(\.name), ["grafana", "cert-manager"], "longhorn is already syncing")
    }

    func testGrouping() throws {
        let s = try status
        let byProject = groupArgoApps(s.apps, by: .project)
        XCTAssertEqual(byProject.map(\.title), ["apps", "infra"])
        let bySet = groupArgoApps(s.apps, by: .appSet)
        XCTAssertEqual(bySet.map(\.title), ["infra", ""], "apps without an ApplicationSet last")
        XCTAssertEqual(bySet[0].apps.map(\.name), ["grafana", "longhorn", "cilium"])
        XCTAssertEqual(groupArgoApps(s.apps, by: .none).map(\.apps.count), [6])
        XCTAssertEqual(groupArgoApps(s.apps, by: .namespace).first?.title, "demo")
        XCTAssertEqual(s.apps(of: s.appSets[0]).map(\.name), ["grafana", "longhorn", "cilium"])
        XCTAssertEqual(s.apps(of: s.projects[0]).count, 2)
    }

    func testWaveStepsDuringASync() throws {
        let steps = try app("longhorn").waveSteps
        XCTAssertEqual(steps.map(\.wave), [-1, 0, 1, 2])
        XCTAssertEqual(steps.map(\.state), [.done, .done, .current, .pending])
        XCTAssertEqual(steps[3].total, 1, "the hook is not counted")
        XCTAssertTrue(steps[3].hasHooks)
        XCTAssertEqual(steps[0].done, 1)
    }

    func testWaveStepsAtRest() throws {
        XCTAssertEqual(try app("grafana").waveSteps.map(\.state), [.failed])
        XCTAssertEqual(try app("cert-manager").waveSteps.map(\.state), [.pending])
        XCTAssertEqual(try app("cilium").waveSteps.map(\.state), [.done])
    }

    func testLikelyCause() throws {
        let worker = try app("demo-worker")
        XCTAssertEqual(worker.likelyCause(downNodes: ["worker-2"]), .nodeDown(node: "worker-2", pod: "worker-7f9c"))
        XCTAssertEqual(worker.likelyCause(downNodes: []), .pod(name: "worker-7f9c", status: "CrashLoopBackOff", node: "worker-2"))
        XCTAssertEqual(try app("grafana").likelyCause(downNodes: []), .message("spec.template.spec.containers[0].image: Required value"))
        XCTAssertEqual(try app("longhorn").likelyCause(downNodes: []), .message("Waiting for rollout to finish"))
        XCTAssertNil(try app("cert-manager").likelyCause(downNodes: []), "drifting only: nothing more to say")
        XCTAssertNil(try app("cilium").likelyCause(downNodes: ["worker-2"]))
    }

    func testLabels() throws {
        XCTAssertEqual(try app("demo-worker").revisionLabel, "4be1d0c")
        XCTAssertEqual(try app("grafana").revisionLabel, "grafana@8.5.2")
        XCTAssertEqual(try app("demo-worker").history[0].label, "4be1d0c")
        XCTAssertEqual(try app("grafana").history[0].label, "grafana 8.5.2")
        XCTAssertEqual(try app("demo-worker").deployedAt, 5000)
        XCTAssertEqual(try app("cert-manager").pruneCandidates.map(\.name), ["cert-manager-legacy"])
        XCTAssertEqual(shortRevision("v1.18.2"), "v1.18.2")
        XCTAssertEqual(try app("grafana").iconApp.icon, "grafana")
        XCTAssertNil(try app("demo-worker").iconApp.icon)
    }

    func testSyncOptionsJSON() throws {
        let defaults = ArgoSyncOptions(defaultsFor: try app("cert-manager"))
        XCTAssertTrue(defaults.serverSideApply)
        XCTAssertFalse(defaults.prune)
        XCTAssertFalse(defaults.applyOutOfSyncOnly)
        var opts = ArgoSyncOptions(prune: true)
        opts.resources = [ArgoResourceRef(group: "apps", kind: "Deployment", namespace: "a", name: "b")]
        XCTAssertEqual(opts.json, #"{"applyOutOfSyncOnly":false,"dryRun":false,"force":false,"historyId":0,"prune":true,"replace":false,"#
            + #""resources":[{"group":"apps","kind":"Deployment","name":"b","namespace":"a"}],"serverSideApply":false}"#)
        XCTAssertEqual(ArgoAction.hardRefresh.rawValue, "hardRefresh")
    }

    func testInventoryMatching() throws {
        let s = try status
        let grafana = InventoryApp(id: "grafana", name: "Grafana", icon: "grafana", namespaces: ["monitoring"])
        XCTAssertEqual(argoApps(for: grafana, in: s).map(\.name), ["grafana"])
        // By name: the Application has no icon.
        let worker = InventoryApp(id: "demo-worker", name: "Demo worker", namespaces: [])
        XCTAssertEqual(argoApps(for: worker, in: s).map(\.name), ["demo-worker"])
        // By namespace, worst first; never for system apps.
        let busybox = InventoryApp(id: "busybox", name: "BusyBox", namespaces: ["demo", "reports"])
        XCTAssertEqual(argoApps(for: busybox, in: s).map(\.name), ["demo-worker", "nightly-reports"])
        let system = InventoryApp(id: "coredns", name: "CoreDNS", system: true, namespaces: ["kube-system"])
        XCTAssertTrue(argoApps(for: system, in: s).isEmpty)
        XCTAssertTrue(argoNeedsBadge(argoApps(for: grafana, in: s)))
        XCTAssertFalse(argoNeedsBadge(argoApps(for: InventoryApp(id: "cilium", name: "Cilium", icon: "cilium"), in: s)))

        XCTAssertTrue(argoCDHinted(ClusterInventory(apps: [InventoryApp(id: "argo-cd", name: "Argo CD")])))
        XCTAssertFalse(argoCDHinted(ClusterInventory(apps: [grafana])))
    }
}
