import XCTest
@testable import IchorCore

final class GitOpsQuickEditTests: XCTestCase {
    private func object(_ json: String) throws -> [String: Any] {
        try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }

    func testDecodesTheTargets() throws {
        let targets = try TalosJSON.decode(QuickEditTargets.self, from: #"""
        {"documents":[{"index":0,"kind":"Deployment","name":"podinfo","replicas":2,"scalable":true,
          "containers":[{"name":"podinfo","image":"ghcr.io/example-org/podinfo:6.7.1","line":21}]},
         {"index":1,"kind":"CronJob","name":"cleanup","scalable":false,
          "containers":[{"name":"job","image":"ghcr.io/example-org/tools:1.0","line":40}]},
         {"index":2,"kind":"Widget","name":"custom","replicas":4}],
         "kustomizeImages":null}
        """#)
        XCTAssertEqual(targets.documents.count, 3)
        XCTAssertEqual(targets.documents[0].replicas, 2)
        XCTAssertNil(targets.documents[1].replicas)
        XCTAssertFalse(targets.documents[1].scalable)
        // An older core without "scalable": spec.replicas says it.
        XCTAssertTrue(targets.documents[2].scalable)
        XCTAssertEqual(targets.kustomizeImages, [])
        XCTAssertEqual(targets.kustomizeReplicas, [])
        XCTAssertEqual(targets.replicas.map(\.label), ["Deployment podinfo", "Widget custom"])
        XCTAssertEqual(targets.replicas.map(\.current), [2, 4])
        XCTAssertEqual(targets.images.map(\.label), ["podinfo (Deployment podinfo)", "job (CronJob cleanup)"])
        XCTAssertFalse(targets.isEmpty)
        XCTAssertTrue(try TalosJSON.decode(QuickEditTargets.self, from: "{}").isEmpty)
    }

    func testAKustomizationEntryReplacesTheContainer() throws {
        let targets = try TalosJSON.decode(QuickEditTargets.self, from: #"""
        {"documents":[{"index":1,"kind":"Deployment","name":"app","scalable":true,
          "containers":[{"name":"app","image":"ghcr.io/example-org/app:1.0.0","overriddenBy":"ghcr.io/example-org/app"},
                        {"name":"sidecar","image":"ghcr.io/example-org/proxy:2.1"}]}],
         "kustomizeImages":[{"documentIndex":0,"name":"ghcr.io/example-org/app","newTag":"1.4.0","line":7},
          {"documentIndex":0,"name":"nginx","newName":"registry.example.com:5000/mirror/nginx"}],
         "kustomizeReplicas":[{"documentIndex":0,"name":"app","count":3,"line":12}]}
        """#)
        let images = targets.images
        XCTAssertEqual(images.map(\.label), ["app", "nginx", "sidecar"])
        XCTAssertEqual(images.map(\.image),
                       ["ghcr.io/example-org/app:1.4.0", "registry.example.com:5000/mirror/nginx", "ghcr.io/example-org/proxy:2.1"])
        XCTAssertTrue(images[0].isKustomize)
        XCTAssertFalse(images[2].isKustomize)

        let edit = images[0].edit(tag: " 1.3.2 ")
        let json = try object(try edit.json())
        XCTAssertEqual(json["op"] as? String, "image")
        XCTAssertEqual(json["documentIndex"] as? Int, 0)
        XCTAssertEqual(json["kustomizeImage"] as? String, "ghcr.io/example-org/app")
        XCTAssertEqual(json["image"] as? String, "1.3.2")
        XCTAssertNil(json["container"])
        XCTAssertEqual(quickEditTitle(edit), "Bump app to 1.3.2")

        let replicas = targets.replicas
        XCTAssertEqual(replicas.map(\.label), ["app", "Deployment app"])
        XCTAssertTrue(replicas[0].isKustomize)
        XCTAssertEqual(replicas[0].current, 3)
        let scale = try object(try replicas[0].edit(replicas: 5).json())
        XCTAssertEqual(scale["kustomizeReplica"] as? String, "app")
        XCTAssertEqual(scale["replicas"] as? Int, 5)
        XCTAssertEqual(quickEditTitle(replicas[0].edit(replicas: 5)), "Scale app to 5")
    }

    func testEncodesTheContainerEdits() throws {
        let document = QuickEditDocument(index: 1, kind: "Deployment", name: "podinfo", replicas: 2,
                                         containers: [QuickEditContainer(name: "podinfo", image: "ghcr.io/example-org/podinfo:6.7.1")])
        let targets = QuickEditTargets(documents: [document])
        let image = try object(try XCTUnwrap(targets.images.first).edit(tag: "6.7.0").json())
        XCTAssertEqual(image["op"] as? String, "image")
        XCTAssertEqual(image["documentIndex"] as? Int, 1)
        XCTAssertEqual(image["container"] as? String, "podinfo")
        XCTAssertEqual(image["image"] as? String, "ghcr.io/example-org/podinfo:6.7.0")
        // The workload the edit means, so the core can find it if the file moved.
        XCTAssertEqual(image["kind"] as? String, "Deployment")
        XCTAssertEqual(image["name"] as? String, "podinfo")
        // Only the keys the Go core knows: it refuses unknown ones.
        XCTAssertEqual(Set(image.keys), ["op", "documentIndex", "kind", "name", "container", "image"])

        let scale = try object(try XCTUnwrap(targets.replicas.first).edit(replicas: 3).json())
        XCTAssertEqual(Set(scale.keys), ["op", "documentIndex", "kind", "name", "replicas"])
        XCTAssertEqual(scale["op"] as? String, "replicas")
        XCTAssertEqual(scale["kind"] as? String, "Deployment")
        XCTAssertEqual(scale["name"] as? String, "podinfo")
        XCTAssertEqual(scale["replicas"] as? Int, 3)

        // A target built without its workload sends neither key.
        let bare = QuickEditImageTarget(place: .container(documentIndex: 0, name: "app"), label: "app", image: "app:1")
        XCTAssertEqual(Set(try object(try bare.edit(tag: "2").json()).keys), ["op", "documentIndex", "container", "image"])
    }

    func testKustomizeEditsCarryNoWorkload() throws {
        let targets = QuickEditTargets(kustomizeImages: [QuickEditKustomizeImage(name: "ghcr.io/example-org/app", newTag: "1")],
                                       kustomizeReplicas: [QuickEditKustomizeReplica(name: "app", count: 2)])
        let image = try object(try XCTUnwrap(targets.images.first).edit(tag: "2").json())
        XCTAssertEqual(Set(image.keys), ["op", "documentIndex", "kustomizeImage", "image"])
        let scale = try object(try XCTUnwrap(targets.replicas.first).edit(replicas: 3).json())
        XCTAssertEqual(Set(scale.keys), ["op", "documentIndex", "kustomizeReplica", "replicas"])
    }

    func testDigestEntriesAndUnnamedContainersAreNotOffered() throws {
        let targets = try TalosJSON.decode(QuickEditTargets.self, from: #"""
        {"documents":[{"index":0,"kind":"Deployment","name":"web","scalable":true,
          "containers":[{"name":"","image":"ghcr.io/example-org/web:1"},{"name":"web","image":"ghcr.io/example-org/web:1"}]}],
         "kustomizeImages":[{"documentIndex":1,"name":"ghcr.io/example-org/pinned","digest":"sha256:abc"},
          {"documentIndex":1,"name":"ghcr.io/example-org/api","newTag":"2"}]}
        """#)
        XCTAssertEqual(targets.kustomizeImages[0].digest, "sha256:abc")
        XCTAssertEqual(targets.kustomizeImages[1].digest, "")
        XCTAssertEqual(targets.images.map(\.label), ["api", "web"])
    }

    func testImageTagsAreValidated() {
        XCTAssertTrue(isValidImageTag("6.7.0"))
        XCTAssertTrue(isValidImageTag("v1.2.3-rc.1_build"))
        XCTAssertTrue(isValidImageTag(" 1.0 "))
        XCTAssertTrue(isValidImageTag(String(repeating: "a", count: 128)))
        XCTAssertFalse(isValidImageTag(String(repeating: "a", count: 129)))
        XCTAssertFalse(isValidImageTag(""))
        XCTAssertFalse(isValidImageTag("a/b"))
        XCTAssertFalse(isValidImageTag("1 2"))
        XCTAssertFalse(isValidImageTag(".hidden"))
        XCTAssertFalse(isValidImageTag("-x"))
        XCTAssertFalse(isValidImageTag("é1"))
        XCTAssertFalse(isValidImageTag("sha256:" + String(repeating: "0", count: 64)))
    }

    func testTitles() throws {
        let targets = QuickEditTargets(documents: [QuickEditDocument(index: 0, kind: "StatefulSet", name: "redis")])
        let redis = try XCTUnwrap(targets.replicas.first)
        XCTAssertEqual(redis.current, 1)
        XCTAssertEqual(quickEditTitle(redis.edit(replicas: 5)), "Scale redis to 5")
        let target = QuickEditImageTarget(place: .container(documentIndex: 0, name: "redis"), label: "redis",
                                          image: "docker.io/library/redis:7.2@sha256:abc")
        XCTAssertEqual(quickEditTitle(target.edit(tag: "7.4")), "Bump redis to 7.4")
        XCTAssertEqual(target.edit(tag: "7.4").image, "docker.io/library/redis:7.4")
    }

    func testAKeepEditSendsTheLiveChangesOfItsObject() throws {
        let resource = try TalosJSON.decode(KubeDiffResource.self, from: #"""
        {"kind":"Deployment","name":"web","change":"changed",
         "liveChanges":[{"path":["spec","replicas"],"label":"spec.replicas","wanted":"2","live":"5","type":"int"}]}
        """#)
        XCTAssertEqual(resource.liveChanges, [DiffLiveChange(path: ["spec", "replicas"], label: "spec.replicas", wanted: "2", live: "5", type: "int")])
        XCTAssertEqual(try TalosJSON.decode(KubeDiffResource.self, from: #"{"kind":"Service","name":"web","change":"changed"}"#).liveChanges, [])

        let edit = QuickEdit.keep(documentIndex: 1, resource: resource)
        XCTAssertEqual(quickEditTitle(edit), "Keep live changes of Deployment web")
        let json = try object(try edit.json())
        XCTAssertEqual(Set(json.keys), ["op", "documentIndex", "kind", "name", "changes"])
        XCTAssertEqual(json["op"] as? String, "keep")
        XCTAssertEqual(json["documentIndex"] as? Int, 1)
        let change = try XCTUnwrap((json["changes"] as? [[String: Any]])?.first)
        XCTAssertEqual(Set(change.keys), ["path", "label", "wanted", "live", "type"])

        let result = try TalosJSON.decode(QuickEditResult.self, from: #"{"content":"x","changed":true,"skipped":["a: not set in this file"]}"#)
        XCTAssertEqual(result.skipped, ["a: not set in this file"])
    }

    func testDecodesTheResult() throws {
        let result = try TalosJSON.decode(QuickEditResult.self, from: #"{"content":"kind: Deployment\n","changed":true,"line":27}"#)
        XCTAssertTrue(result.changed)
        XCTAssertEqual(result.line, 27)
        XCTAssertFalse(try TalosJSON.decode(QuickEditResult.self, from: #"{"content":"x"}"#).changed)
    }

    func testImageParts() {
        XCTAssertEqual(imageRepository("ghcr.io/example-org/app:1.2"), "ghcr.io/example-org/app")
        XCTAssertEqual(imageTag("ghcr.io/example-org/app:1.2"), "1.2")
        XCTAssertEqual(imageRepository("registry.example.com:5000/app"), "registry.example.com:5000/app")
        XCTAssertEqual(imageTag("registry.example.com:5000/app"), "")
        XCTAssertEqual(imageTag("registry.example.com:5000/app:v3@sha256:abc"), "v3")
        XCTAssertEqual(imageTag("nginx@sha256:abc"), "")
        XCTAssertEqual(imageWithTag("nginx", tag: "1.27"), "nginx:1.27")
        XCTAssertEqual(shortImageName("ghcr.io/example-org/app:1.2"), "app")
    }

    func testTagSuggestions() {
        let image = "ghcr.io/example-org/podinfo:6.7.1"
        let both = quickEditTagSuggestions(for: image,
                                           history: ["ghcr.io/example-org/podinfo:6.7.1", "ghcr.io/example-org/podinfo:6.7.0"],
                                           live: ["ghcr.io/example-org/other:1.0", "ghcr.io/example-org/podinfo:6.6.9"])
        XCTAssertEqual(both, QuickEditTagSuggestions(previous: "6.7.0", running: "6.6.9"))
        let same = quickEditTagSuggestions(for: image, history: ["ghcr.io/example-org/podinfo:6.7.0"],
                                           live: ["ghcr.io/example-org/podinfo:6.7.0"])
        XCTAssertEqual(same, QuickEditTagSuggestions(previous: "6.7.0"))
        XCTAssertTrue(quickEditTagSuggestions(for: image, history: [], live: [image]).isEmpty)
    }
}
