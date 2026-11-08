import XCTest
@testable import IchorCore

final class ClusterUpdatesTests: XCTestCase {
    // The shape of demoClusterUpdates() (go/ichorgo/updates_demo.go), cut down.
    private let json = """
        {"at":1767323045000,
         "images":[
          {"repo":"ghcr.io/stefanprodan/podinfo","tag":"6.7.0","digest":"","workloads":[{"kind":"Deployment","namespace":"demo","name":"podinfo","containers":["podinfo"]}],
           "owner":{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"podinfo"},
           "current":"6.7.0","patch":"6.7.1","minor":"6.9.2","shape":"x.y.z","source":"ghcr.io",
           "edit":{"op":"image","kind":"Deployment","name":"podinfo","container":"podinfo","image":"ghcr.io/stefanprodan/podinfo:6.7.1","path":""}},
          {"repo":"ghcr.io/example-org/worker","tag":"2.3.0","digest":"","workloads":null,"owner":{"tool":"argocd","kind":"Application","namespace":"argocd","name":"hello-ichor"},
           "current":"2.3.0","patch":"2.3.1","automatedBy":"argocd-image-updater","edit":null},
          {"repo":"busybox","tag":"latest","digest":"","workloads":[],"owner":null,"current":"latest","reason":"not a version","edit":null},
          {"repo":"nginx","tag":"1.27.0","digest":"","workloads":[],"owner":null,"current":"1.27.0","edit":null}
         ],
         "charts":[
          {"name":"ingress-nginx","version":"4.12.1","repoURL":"https://kubernetes.github.io/ingress-nginx",
           "owner":{"tool":"flux","kind":"HelmRelease","namespace":"ingress-nginx","name":"ingress-nginx"},
           "current":"4.12.1","patch":"4.12.4","minor":"4.13.3","source":"kubernetes.github.io",
           "edit":{"op":"path","path":"spec.chart.spec.version","value":"4.12.4"}}
         ],
         "skipped":null,"lookups":{"queried":8,"failed":1}}
        """

    private func updates() throws -> ClusterUpdates { try TalosJSON.decode(ClusterUpdates.self, from: json) }

    func testSectionsSplitWhatToBumpFromWhatCannotBeCompared() throws {
        let s = updateSections(try updates())
        XCTAssertEqual(s.images.map(\.name), ["ghcr.io/stefanprodan/podinfo", "ghcr.io/example-org/worker"])
        XCTAssertEqual(s.charts.map(\.name), ["ingress-nginx"])
        XCTAssertEqual(s.notComparable.map(\.name), ["busybox"])
        XCTAssertEqual(s.outdated, 2)
        XCTAssertEqual(try updates().lookups, UpdateLookups(queried: 8, failed: 1))
    }

    func testAnAutomatedImageShowsItsBadgeAndNoChip() throws {
        let worker = updateSections(try updates()).images[1]
        XCTAssertEqual(worker.automation, .argoImageUpdater)
        XCTAssertTrue(worker.candidates.isEmpty)
        XCTAssertFalse(worker.editable)
    }

    func testChipsAreTheCandidatesInStepOrder() throws {
        let podinfo = updateSections(try updates()).images[0]
        XCTAssertEqual(
            podinfo.candidates,
            [UpdateCandidate(step: .patch, version: "6.7.1"), UpdateCandidate(step: .minor, version: "6.9.2")])
        XCTAssertEqual(podinfo.newest, "6.9.2")
        XCTAssertTrue(podinfo.editable)
    }

    func testTheItemSentBackCarriesTheVersionPicked() throws {
        let podinfo = updateSections(try updates()).images[0]
        let item = try XCTUnwrap(
            try JSONSerialization.jsonObject(with: Data(updateItemJSON(podinfo, version: "6.9.2").utf8))
                as? [String: Any])
        XCTAssertEqual(item["version"] as? String, "6.9.2")
        XCTAssertEqual((item["owner"] as? [String: Any])?["tool"] as? String, "flux")
        XCTAssertEqual((item["edit"] as? [String: Any])?["op"] as? String, "image")
    }

    func testAnImageTargetBecomesAnImageEditNamedAfterTheUpdate() throws {
        let podinfo = updateSections(try updates()).images[0]
        let target = try TalosJSON.decode(
            UpdateEditTarget.self,
            from: """
                {"app":{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"podinfo"},
                 "source":{"repoURL":"ssh://git@git.example.com/homelab/fleet.git","path":"apps/podinfo","kind":"kustomize","browsable":true},
                 "sourceIndex":0,"path":"apps/podinfo/deployment.yaml","line":17,"documentIndex":0,
                 "edit":{"op":"image","documentIndex":0,"kind":"Deployment","name":"podinfo","container":"podinfo","image":"ghcr.io/stefanprodan/podinfo:6.7.1"}}
                """)
        XCTAssertEqual(target.line, 17)
        XCTAssertEqual(
            target.app?.ref, GitOpsAppRef.flux(kind: "Kustomization", namespace: "flux-system", name: "podinfo"))
        let edit = try XCTUnwrap(updateQuickEdit(target, row: podinfo, version: "6.7.1"))
        XCTAssertEqual(edit.op, .image)
        XCTAssertEqual(edit.container, "podinfo")
        XCTAssertEqual(quickEditTitle(edit), "Update podinfo to 6.7.1")
        XCTAssertTrue(edit.body.contains("`6.7.0` to `6.7.1`"))
    }

    func testAChartTargetIsAPathEdit() throws {
        let chart = updateSections(try updates()).charts[0]
        let target = try TalosJSON.decode(
            UpdateEditTarget.self,
            from: """
                {"app":{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"apps"},"path":"apps/ingress.yaml",
                 "edit":{"op":"path","documentIndex":0,"kind":"HelmRelease","name":"ingress-nginx","path":"spec.chart.spec.version","value":"4.13.3"}}
                """)
        let edit = try XCTUnwrap(updateQuickEdit(target, row: chart, version: "4.13.3"))
        let wire = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(edit.json().utf8)) as? [String: Any])
        XCTAssertEqual(wire["op"] as? String, "path")
        XCTAssertEqual(wire["path"] as? String, "spec.chart.spec.version")
        XCTAssertEqual(wire["value"] as? String, "4.13.3")
        XCTAssertEqual(wire["kind"] as? String, "HelmRelease")
        XCTAssertNil(wire["title"])
        XCTAssertEqual(quickEditTitle(edit), "Update ingress-nginx to 4.13.3")
    }

    func testAPrefillPicksItsKustomizeEntryOrContainer() {
        let targets = QuickEditTargets(
            documents: [
                QuickEditDocument(
                    index: 0, kind: "Deployment", name: "api",
                    containers: [QuickEditContainer(name: "api", image: "ghcr.io/example-org/api:2.0.0")])
            ],
            kustomizeImages: [
                QuickEditKustomizeImage(documentIndex: 1, name: "ghcr.io/example-org/api", newTag: "2.0.0")
            ]
        ).images
        let kustomize = QuickEdit(
            op: .image, documentIndex: 1, image: "2.1.0", kustomizeImage: "ghcr.io/example-org/api", label: "api")
        XCTAssertTrue(quickPrefillTarget(targets, edit: kustomize)?.isKustomize == true)
        XCTAssertEqual(quickPrefillTag(kustomize), "2.1.0")
        let plain = QuickEdit(
            op: .image, documentIndex: 0, kind: "Deployment", name: "api", container: "api",
            image: "ghcr.io/example-org/api:2.1.0", label: "api")
        XCTAssertEqual(quickPrefillTarget(targets, edit: plain)?.place, .container(documentIndex: 0, name: "api"))
        XCTAssertEqual(quickPrefillTag(plain), "2.1.0")
        XCTAssertEqual(plain.titled("Update api to 2.1.0", body: "b").title, "Update api to 2.1.0")
    }

    func testNoEditNoQuickEdit() throws {
        let chart = updateSections(try updates()).charts[0]
        let target = try TalosJSON.decode(
            UpdateEditTarget.self, from: #"{"note":"no Argo CD or Flux app declares HelmRelease x in Git"}"#)
        XCTAssertNil(updateQuickEdit(target, row: chart, version: "4.12.4"))
        XCTAssertEqual(target.note, "no Argo CD or Flux app declares HelmRelease x in Git")
    }
}
