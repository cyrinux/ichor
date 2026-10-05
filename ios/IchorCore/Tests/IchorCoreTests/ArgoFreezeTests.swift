import XCTest
@testable import IchorCore

final class ArgoFreezeTests: XCTestCase {
    // Shaped like KubeArgoCD's demo answer (go/ichorgo/kube_argocd_demo.go), trimmed: the demo
    // namespace frozen by Ichor, an ended freeze, and a nightly window from Git.
    private let json = #"""
    {"installed":true,"apps":[
     {"namespace":"argocd","name":"demo-worker","project":"apps","destination":{"namespace":"demo"},
      "autoSync":{"enabled":true,"selfHeal":true},
      "resources":[{"group":"apps","kind":"Deployment","namespace":"demo","name":"worker","sync":"OutOfSync"},
                   {"kind":"Service","namespace":"demo","name":"worker","sync":"Synced"}],
      "freeze":{"project":"apps","until":5000,"manualSync":true,"byIchor":true,"windows":["a1"]}},
     {"namespace":"argocd","name":"hello-ichor","project":"apps","destination":{"namespace":"demo"},
      "freeze":{"project":"apps","until":5000,"manualSync":true,"byIchor":true,"windows":["a1"]}},
     {"namespace":"argocd","name":"nightly-reports","project":"apps","destination":{"namespace":"reports"}},
     {"namespace":"argocd","name":"cilium","project":"infra","destination":{"namespace":"kube-system"},
      "autoSync":{"enabled":true,"selfHeal":true},"resources":[{"group":"apps","kind":"DaemonSet","namespace":"kube-system","name":"cilium","sync":"Synced"}],
      "freeze":null},
     {"namespace":"argocd","name":"crds","project":"infra","destination":{}}],
     "projects":[
      {"namespace":"argocd","name":"apps","syncWindows":2,"managedBy":"projects","managedServerSide":true,"windows":[
        {"id":"a1","kind":"deny","schedule":"8 14 5 10 *","duration":"61m","timeZone":"UTC","namespaces":["demo"],"manualSync":true,
         "active":true,"start":1000,"end":5000,"apps":2,"ichor":{"reason":"hotfix","createdAt":1000,"expiresAt":5000}},
        {"id":"b2","kind":"deny","schedule":"30 10 5 10 *","duration":"31m","applications":["nightly-reports"],"manualSync":true,
         "active":false,"start":9000,"end":9500,"apps":1,"ichor":{"createdAt":100,"expiresAt":200,"expired":true}}]},
      {"namespace":"argocd","name":"infra","syncWindows":1,"windows":[
        {"id":"c3","kind":"deny","schedule":"0 22 * * *","duration":"8h","applications":["*"],"active":false,"start":7000,"end":8000,"apps":2},
        {"id":"d4","kind":"allow","schedule":"bad","duration":"1h","error":"bad schedule","clusters":null}]}]}
    """#

    private var status: ArgoStatus { try! JSONDecoder().decode(ArgoStatus.self, from: Data(json.utf8)) }
    private func app(_ name: String) -> ArgoApp { status.apps.first { $0.name == name }! }

    func testDecodesWindowsAndFreezes() {
        let apps = status.projects.first { $0.name == "apps" }!
        XCTAssertEqual(apps.managedBy, "projects")
        XCTAssertTrue(apps.managedServerSide)
        XCTAssertEqual(apps.windows.count, 2)
        XCTAssertTrue(apps.windows[0].isDeny && apps.windows[0].active)
        XCTAssertEqual(apps.windows[0].ichor?.reason, "hotfix")
        XCTAssertEqual(apps.windows[0].endsAt, 5000)
        // An ended Ichor freeze ends at its expiry, not at next year's occurrence.
        XCTAssertEqual(apps.windows[1].endsAt, 200)
        XCTAssertEqual(app("demo-worker").freeze?.until, 5000)
        XCTAssertNil(app("cilium").freeze)
        XCTAssertEqual(status.projects[1].windows[1].clusters, [])
    }

    func testTargetsAndOptionsByScope() {
        let worker = app("demo-worker")
        XCTAssertEqual(status.freezeTargets(around: worker, scope: .app).map(\.name), ["demo-worker"])
        XCTAssertEqual(status.freezeTargets(around: worker, scope: .namespace).map(\.name), ["demo-worker", "hello-ichor"])
        XCTAssertEqual(status.freezeTargets(around: worker, scope: .project).map(\.name), ["demo-worker", "hello-ichor", "nightly-reports"])
        XCTAssertFalse(FreezeScope.namespace.available(for: app("crds")))
        XCTAssertTrue(FreezeScope.project.available(for: app("crds")))
        XCTAssertEqual(freezeOptions(for: worker, scope: .app, minutes: 60, manualSync: true, reason: " fix "),
                       ArgoFreezeOptions(applications: ["demo-worker"], minutes: 60, manualSync: true, reason: "fix"))
        XCTAssertEqual(freezeOptions(for: worker, scope: .namespace, minutes: 15, manualSync: false, reason: "").namespaces, ["demo"])
        XCTAssertEqual(freezeOptions(for: worker, scope: .project, minutes: 15, manualSync: false, reason: "").applications, ["*"])
        XCTAssertEqual(ArgoFreezeOptions(minutes: 60, window: "a1").json,
                       #"{"applications":[],"fromGit":false,"manualSync":false,"minutes":60,"namespaces":[],"reason":"","window":"a1"}"#)
    }

    func testWindowSectionsAndFreezes() {
        let sections = status.windowSections()
        XCTAssertEqual(sections[.active]?.map(\.window.id), ["a1"])
        // By next start, the unreadable one last.
        XCTAssertEqual(sections[.upcoming]?.map(\.window.id), ["c3", "d4"])
        XCTAssertEqual(sections[.expired]?.map(\.window.id), ["b2"])
        XCTAssertEqual(status.activeFreezes.map(\.window.id), ["a1"])
        XCTAssertEqual(status.runningIchorFreezes.map(\.window.id), ["a1"])
        XCTAssertEqual(status.projectsToClear.map(\.name), ["apps"])
        XCTAssertEqual(status.freezeWindows(of: app("demo-worker")).map(\.id), ["argocd/apps/a1"])
        XCTAssertTrue(status.freezeWindows(of: app("cilium")).isEmpty)
        XCTAssertEqual(app("demo-worker").drifted.map(\.name), ["worker"])
        XCTAssertTrue(ArgoFilter.frozen.matches(app("hello-ichor")))
        XCTAssertFalse(ArgoFilter.frozen.matches(app("cilium")))
    }

    func testSelfHealingOwner() {
        XCTAssertEqual(status.selfHealingOwner(kind: "DaemonSet", namespace: "kube-system", name: "cilium")?.name, "cilium")
        XCTAssertNil(status.selfHealingOwner(kind: "Deployment", namespace: "kube-system", name: "cilium"))
        // Frozen already: a scale by hand stays.
        XCTAssertNil(status.selfHealingOwner(kind: "Deployment", namespace: "demo", name: "worker"))
    }

    func testMinutesUntilMorning() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Paris")!
        func at(_ s: String) -> Date { ISO8601DateFormatter().date(from: s)! }
        XCTAssertEqual(minutesUntil(hour: 9, from: at("2026-10-05T06:00:00Z"), calendar: calendar), 60)
        // Past 09:00 (or within 5 minutes of it): tomorrow's.
        XCTAssertEqual(minutesUntil(hour: 9, from: at("2026-10-05T08:00:00Z"), calendar: calendar), 23 * 60)
        XCTAssertEqual(minutesUntil(hour: 9, from: at("2026-10-05T06:58:00Z"), calendar: calendar), 24 * 60 + 2)
        XCTAssertEqual(minutesUntil(hour: 9, from: at("2026-10-05T06:00:30Z"), calendar: calendar), 60)
    }
}
