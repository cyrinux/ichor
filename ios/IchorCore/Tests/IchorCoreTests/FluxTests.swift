import XCTest
@testable import IchorCore

final class FluxTests: XCTestCase {
    // Shaped like KubeFlux's demo answer (go/ichorgo/kube_flux_demo.go): a Kustomization failing
    // its health checks, a HelmRelease whose upgrade failed (stalled), one reconciling towards a
    // new chart version, a Kustomization applying a new revision, a suspended release, and a
    // Helm repository that cannot be fetched.
    private let json = #"""
    {"installed":true,"version":"v2.7.0","helmError":"","sourcesError":"","apps":[
     {"kind":"Kustomization","namespace":"flux-system","name":"apps","level":"critical","ready":"False","reason":"HealthCheckFailed",
      "message":"health check failed after 5m0s","reconciling":false,"pending":false,"stalled":false,"suspended":false,
      "owner":{"kind":"Kustomization","namespace":"flux-system","name":"flux-system"},
      "source":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"},"sourceURL":"ssh://git@git.example.com/homelab/fleet.git",
      "path":"./apps/homelab","chart":"","chartVersion":"","targetNamespace":"","interval":"10m","prune":true,
      "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","attemptedRevision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d",
      "dependsOn":["flux-system/infra-controllers"],"failures":0,
      "conditions":[{"type":"Ready","status":"False","reason":"HealthCheckFailed","message":"health check failed after 5m0s","at":1000},
                    {"type":"Healthy","status":"False","reason":"HealthCheckFailed","message":"Deployment/demo/worker status: 'Failed'","at":1000}],
      "resources":[{"group":"","kind":"Service","namespace":"demo","name":"worker"},{"group":"apps","kind":"Deployment","namespace":"demo","name":"worker"},
                   {"group":"","kind":"ConfigMap","namespace":"demo","name":"worker-config"},{"group":"apps","kind":"Deployment","namespace":"demo","name":"hello-ichor"}],
      "history":[],"reconciledAt":1000,
      "unhealthyPods":[{"namespace":"demo","name":"worker-7f9c","status":"CrashLoopBackOff","healthy":false,"ready":0,"containers":1,"restarts":14,"node":"worker-2"}]},
     {"kind":"HelmRelease","namespace":"ingress-nginx","name":"ingress-nginx","level":"critical","icon":"ingress-nginx","ready":"False","reason":"RetriesExceeded",
      "message":"Failed to upgrade after 3 attempt(s): context deadline exceeded","stalled":true,
      "owner":{"kind":"Kustomization","namespace":"flux-system","name":"infra-controllers"},
      "source":{"kind":"HelmRepository","namespace":"flux-system","name":"ingress-nginx"},"sourceURL":"https://kubernetes.github.io/ingress-nginx",
      "chart":"ingress-nginx","chartVersion":"4.13.3","interval":"30m","revision":"4.12.1","attemptedRevision":"4.13.3","failures":3,
      "conditions":[{"type":"Stalled","status":"True","reason":"RetriesExceeded","message":"Failed to upgrade after 3 attempt(s)","at":2000}],
      "resources":[],"history":[{"version":4,"chartVersion":"4.13.3","appVersion":"1.13.3","status":"failed","deployedAt":3000},
                                {"version":3,"chartVersion":"4.12.1","appVersion":"1.12.1","status":"deployed","deployedAt":2000}]},
     {"kind":"Kustomization","namespace":"flux-system","name":"infra-controllers","level":"warning","ready":"Unknown","reason":"Progressing",
      "message":"Reconciliation in progress","reconciling":true,"source":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"},
      "owner":{"kind":"Kustomization","namespace":"flux-system","name":"flux-system"},
      "path":"./infrastructure/controllers","revision":"main@sha1:91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e",
      "attemptedRevision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"},
     {"kind":"HelmRelease","namespace":"monitoring","name":"kube-prometheus-stack","level":"warning","icon":"prometheus","ready":"Unknown",
      "reason":"Progressing","message":"Running 'upgrade' action with timeout of 10m0s","reconciling":true,
      "source":{"kind":"HelmRepository","namespace":"flux-system","name":"prometheus-community"},
      "chart":"kube-prometheus-stack","chartVersion":"77.x","revision":"77.5.0","attemptedRevision":"77.6.1"},
     {"kind":"Kustomization","namespace":"flux-system","name":"flux-system","level":"ok","icon":"flux","ready":"True","reason":"ReconciliationSucceeded",
      "message":"Applied revision: main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","owner":null,
      "source":{"kind":"GitRepository","namespace":"flux-system","name":"flux-system"},"path":"./clusters/homelab",
      "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","attemptedRevision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"},
     {"kind":"HelmRelease","namespace":"demo","name":"podinfo","level":"ok","ready":"True","reason":"UpgradeSucceeded",
      "owner":{"kind":"Kustomization","namespace":"flux-system","name":"apps"},
      "source":{"kind":"OCIRepository","namespace":"flux-system","name":"podinfo"},"chart":"podinfo","revision":"6.9.2","attemptedRevision":"6.9.2"},
     {"kind":"HelmRelease","namespace":"cache","name":"redis","level":"idle","icon":"redis","ready":"True","reason":"UpgradeSucceeded","suspended":true,
      "source":{"kind":"HelmRepository","namespace":"flux-system","name":"bitnami"},"chart":"redis","chartVersion":"21.2.x","revision":"21.2.5",
      "futureField":1}],
     "sources":[
      {"kind":"HelmRepository","namespace":"flux-system","name":"ingress-nginx","level":"critical","url":"https://kubernetes.github.io/ingress-nginx",
       "ref":"","revision":"sha256:7e1a0f4c2b9d","ready":"False","reason":"IndexationFailed","message":"failed to fetch Helm repository index",
       "reconciling":false,"pending":false,"suspended":false,"interval":"1h","fetchedAt":1000,"apps":1},
      {"kind":"GitRepository","namespace":"flux-system","name":"flux-system","level":"ok","url":"ssh://git@git.example.com/homelab/fleet.git",
       "ref":"main","revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","ready":"True","reason":"Succeeded","interval":"1m","fetchedAt":5000,"apps":3},
      {"kind":"OCIRepository","namespace":"flux-system","name":"podinfo","level":"ok","url":"oci://ghcr.io/stefanprodan/charts/podinfo","ref":"6.9.x",
       "revision":"6.9.2@sha256:3b1f9c0a8e7d","ready":"True","apps":1},
      {"kind":"HelmRepository","namespace":"flux-system","name":"bitnami","level":"ok","url":"oci://registry-1.docker.io/bitnamicharts","interval":"1h","apps":1}]}
    """#

    private var status: FluxStatus { get throws { try TalosJSON.decode(FluxStatus.self, from: json) } }

    private func app(_ name: String) throws -> FluxApp {
        try XCTUnwrap(try status.apps.first { $0.name == name })
    }

    func testDecodesTheGoAnswer() throws {
        let s = try status
        XCTAssertTrue(s.installed)
        XCTAssertEqual(s.version, "v2.7.0")
        XCTAssertEqual(s.apps.count, 7)
        XCTAssertEqual(s.sources.count, 4)
        let apps = try app("apps")
        XCTAssertEqual(apps.id, "Kustomization/flux-system/apps")
        XCTAssertEqual(apps.level, .critical)
        XCTAssertEqual(apps.owner, FluxRef(kind: "Kustomization", namespace: "flux-system", name: "flux-system"))
        XCTAssertEqual(apps.dependsOn, ["flux-system/infra-controllers"])
        XCTAssertEqual(apps.conditions.map(\.type), ["Ready", "Healthy"])
        XCTAssertEqual(apps.unhealthyPods.first?.node, "worker-2")
        XCTAssertEqual(apps.resources.count, 4)
        let nginx = try app("ingress-nginx")
        XCTAssertTrue(nginx.stalled)
        XCTAssertEqual(nginx.failures, 3)
        XCTAssertEqual(nginx.history.map(\.id), [4, 3])
        XCTAssertNil(try app("flux-system").owner)
        XCTAssertTrue(try app("redis").suspended)
        XCTAssertEqual(s.sources[3].ready, "")
        XCTAssertEqual(s.sources[3].fetchedAt, 0)
    }

    func testNotInstalledAnswer() throws {
        let none = try TalosJSON.decode(FluxStatus.self, from: #"{"installed":false}"#)
        XCTAssertFalse(none.installed)
        XCTAssertTrue(none.apps.isEmpty)
        XCTAssertEqual(none.worst, .ok)
        XCTAssertTrue(none.allCalm)
        XCTAssertFalse(none.anyBusy)
    }

    func testStates() throws {
        XCTAssertEqual(try app("apps").state, .failing)
        XCTAssertEqual(try app("ingress-nginx").state, .failing)
        XCTAssertEqual(try app("infra-controllers").state, .reconciling)
        XCTAssertEqual(try app("flux-system").state, .ready)
        XCTAssertEqual(try app("redis").state, .suspended)
        XCTAssertEqual(try status.sources[0].state, .failing)
        XCTAssertEqual(try status.sources[1].state, .ready)
    }

    func testWhatAnObjectAllows() throws {
        let nginx = try app("ingress-nginx")
        XCTAssertTrue(nginx.canForce)
        XCTAssertTrue(nginx.canReset)
        XCTAssertTrue(nginx.canReconcileWithSource)
        let apps = try app("apps")
        XCTAssertFalse(apps.canForce, "a Kustomization cannot be forced")
        XCTAssertFalse(apps.canReset)
        XCTAssertTrue(apps.canReconcile)
        let redis = try app("redis")
        XCTAssertFalse(redis.canReconcile, "suspended: resume first")
        XCTAssertFalse(redis.canReconcileWithSource)
        XCTAssertFalse(redis.canForce)
        XCTAssertTrue(try status.sources[0].canReconcile)
        XCTAssertEqual(FluxAction.reconcileWithSource.rawValue, "reconcileWithSource")
        XCTAssertEqual(FluxAction.allCases.map(\.rawValue), ["reconcile", "reconcileWithSource", "suspend", "resume", "force", "reset"])
    }

    func testCountsAndFilters() throws {
        let s = try status
        XCTAssertEqual(s.count(.all), 7)
        XCTAssertEqual(s.count(.failing), 2)
        XCTAssertEqual(s.count(.reconciling), 2)
        XCTAssertEqual(s.count(.suspended), 1)
        XCTAssertEqual(s.count(.kustomizations), 3)
        XCTAssertEqual(s.count(.helmReleases), 4)
        XCTAssertEqual(s.stateCounts.map(\.state), [.ready, .reconciling, .suspended, .failing])
        XCTAssertEqual(s.stateCounts.map(\.count), [2, 2, 1, 2])
        XCTAssertEqual(s.failing.map(\.name), ["apps", "ingress-nginx"])
        XCTAssertEqual(s.failingSources.map(\.name), ["ingress-nginx"])
        XCTAssertEqual(s.worst, .critical)
        XCTAssertFalse(s.allCalm)

        XCTAssertEqual(filterFluxApps(s.apps, filter: .helmReleases, query: "").map(\.name),
                       ["ingress-nginx", "kube-prometheus-stack", "podinfo", "redis"])
        XCTAssertEqual(filterFluxApps(s.apps, filter: .all, query: "fleet.git").map(\.name), ["apps"])
        XCTAssertEqual(filterFluxApps(s.apps, filter: .all, query: "INFRASTRUCTURE").map(\.name), ["infra-controllers"])
        XCTAssertTrue(filterFluxApps(s.apps, filter: .suspended, query: "podinfo").isEmpty)
        XCTAssertEqual(filterFluxSources(s.sources, query: "oci://").map(\.name), ["bitnami", "podinfo"])
        XCTAssertEqual(filterFluxSources(s.sources, query: "").first?.name, "ingress-nginx", "failing first")
    }

    func testPolling() throws {
        XCTAssertTrue(try status.anyBusy, "two apps reconcile")
        let calm = try TalosJSON.decode(FluxStatus.self, from: #"""
        {"installed":true,"apps":[{"kind":"Kustomization","name":"a","level":"ok"}],
         "sources":[{"kind":"GitRepository","name":"g","level":"ok"}]}
        """#)
        XCTAssertFalse(calm.anyBusy)
        XCTAssertTrue(calm.allCalm)
        let pending = try TalosJSON.decode(FluxStatus.self, from: #"""
        {"installed":true,"apps":[{"kind":"Kustomization","name":"a","level":"ok"}],
         "sources":[{"kind":"GitRepository","name":"g","level":"ok","pending":true}]}
        """#)
        XCTAssertTrue(pending.anyBusy, "a requested reconcile of a source waits")
        XCTAssertEqual(pending.sources[0].state, .reconciling)
    }

    func testRelations() throws {
        let s = try status
        let root = try app("flux-system")
        XCTAssertEqual(s.children(of: root).map(\.name), ["apps", "infra-controllers"])
        XCTAssertEqual(s.children(of: try app("apps")).map(\.name), ["podinfo"])
        XCTAssertTrue(s.children(of: try app("redis")).isEmpty)
        XCTAssertEqual(s.source(of: root)?.ref, "main")
        XCTAssertEqual(s.app(kind: "HelmRelease", namespace: "demo", name: "podinfo")?.chart, "podinfo")
        XCTAssertNil(s.app(kind: "Kustomization", namespace: "demo", name: "podinfo"))
    }

    func testLabels() throws {
        let apps = try app("apps")
        XCTAssertEqual(apps.revisionLabel, "main@4be1d0c")
        XCTAssertEqual(apps.origin, "./apps/homelab")
        XCTAssertFalse(apps.revisionDiffers)
        XCTAssertEqual(apps.summary, "health check failed after 5m0s")
        XCTAssertEqual(apps.resourcesByKind.map(\.kind), ["ConfigMap", "Deployment", "Service"])
        XCTAssertEqual(apps.resourcesByKind[1].resources.map(\.name), ["hello-ichor", "worker"])
        let nginx = try app("ingress-nginx")
        XCTAssertEqual(nginx.revisionLabel, "ingress-nginx@4.12.1")
        XCTAssertEqual(nginx.origin, "ingress-nginx@4.13.3")
        XCTAssertTrue(nginx.revisionDiffers)
        XCTAssertTrue(try app("infra-controllers").revisionDiffers)
        XCTAssertEqual(try app("podinfo").origin, "podinfo")
        XCTAssertEqual(try app("ingress-nginx").iconApp.icon, "ingress-nginx")
        XCTAssertNil(try app("apps").iconApp.icon)

        XCTAssertEqual(fluxShortRevision("main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"), "main@4be1d0c")
        XCTAssertEqual(fluxShortRevision("sha256:7e1a0f4c2b9d"), "7e1a0f4")
        XCTAssertEqual(fluxShortRevision("6.9.2@sha256:3b1f9c0a8e7d"), "6.9.2@3b1f9c0")
        XCTAssertEqual(fluxShortRevision("4.12.1"), "4.12.1")
        XCTAssertEqual(fluxShortRevision(""), "")
        XCTAssertEqual(try status.sources[2].revisionLabel, "6.9.2@3b1f9c0")
    }

    func testInventoryHint() {
        XCTAssertTrue(fluxHinted(ClusterInventory(apps: [InventoryApp(id: "flux", name: "Flux")])))
        XCTAssertFalse(fluxHinted(ClusterInventory(apps: [InventoryApp(id: "argo-cd", name: "Argo CD")])))
    }
}
