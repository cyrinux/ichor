import XCTest
@testable import IchorCore

final class YamlCursorTests: XCTestCase {
    private let yaml = """
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: web
      labels:
        app.kubernetes.io/name: web
    spec:
      replicas: 2
      template:
        spec:
          containers:
          - name: app
            image: example/app:1
            ports:
              - containerPort: 80
                protocol: TCP
            env:
            - name: MODE
              value: "fast"
          # a comment: not a key
          volumes: []
    ---
    kind: ConfigMap
    """

    /// The cursor just after the first `marker` in `text`.
    private func at(_ marker: String, in text: String? = nil) -> YamlCursor {
        let text = text ?? yaml
        let range = text.range(of: marker)!
        return yamlCursorAt(text, utf16Offset: range.upperBound.utf16Offset(in: text))
    }

    private func offset(of marker: String, in text: String) -> Int {
        text.range(of: marker)!.lowerBound.utf16Offset(in: text)
    }

    func testNestedMaps() {
        XCTAssertEqual(at("name: we").fieldPath, "metadata.name")
        XCTAssertEqual(at("replicas").fieldPath, "spec.replicas")
        XCTAssertEqual(at("apiVersion").fieldPath, "apiVersion")
        XCTAssertEqual(at("io/name").fieldPath, "metadata.labels.app.kubernetes.io/name")
    }

    func testListItems() {
        XCTAssertEqual(at("- name: ap").fieldPath, "spec.template.spec.containers.name")
        XCTAssertEqual(at("image").fieldPath, "spec.template.spec.containers.image")
        XCTAssertEqual(at("containerPort").fieldPath, "spec.template.spec.containers.ports.containerPort")
        XCTAssertEqual(at("protocol").fieldPath, "spec.template.spec.containers.ports.protocol")
        XCTAssertEqual(at("value").fieldPath, "spec.template.spec.containers.env.value")
        XCTAssertEqual(at("volumes").fieldPath, "spec.template.spec.volumes")
    }

    func testBlockKeyAddsInside() {
        let template = at("template")
        XCTAssertTrue(template.opensBlock)
        XCTAssertEqual(template.addPath, "spec.template")
        XCTAssertEqual(template.addColumn, 4)

        let image = at("image")
        XCTAssertFalse(image.opensBlock)
        XCTAssertEqual(image.addPath, "spec.template.spec.containers")
        XCTAssertEqual(image.addColumn, 8)
    }

    func testBlankLineCountsFromTheCursor() {
        let text = "spec:\n  template:\n    \n"
        let blank = offset(of: "    \n", in: text)
        let cursor = yamlCursorAt(text, utf16Offset: blank + 4)
        XCTAssertNil(cursor.key)
        XCTAssertEqual(cursor.fieldPath, "spec.template")
        XCTAssertEqual(yamlCursorAt(text, utf16Offset: blank + 2).addPath, "spec")
    }

    func testStopsAtDocumentStartAndSkipsComments() {
        XCTAssertEqual(at("kind: C").fieldPath, "kind")
        XCTAssertEqual(at("# a comm").fieldPath, "spec.template.spec")
    }

    func testQuotedKeysCrlfAndWideCharacters() {
        let text = "data:\r\n  \"a.key\": x\r\n"
        XCTAssertEqual(yamlCursorAt(text, utf16Offset: offset(of: "x", in: text)).fieldPath, "data.a.key")

        // UTF-16 offsets, as UITextView counts: an emoji is two units.
        let wide = "metadata:\n  annotations:\n    note: 🚀🚀\n  name: web\n"
        XCTAssertEqual(yamlCursorAt(wide, utf16Offset: offset(of: "web", in: wide)).fieldPath, "metadata.name")
    }

    func testInsertBelowAKeyOrOnABlankLine() {
        let text = "spec:\n  replicas: 2\n"
        let below = insertYamlField(text, utf16Offset: offset(of: "2", in: text), name: "paused", listItem: false)
        XCTAssertEqual(below.text, "spec:\n  replicas: 2\n  paused: \n")
        XCTAssertEqual(below.cursor, offset(of: "paused: ", in: below.text) + 8)

        let inside = insertYamlField(text, utf16Offset: 2, name: "selector", listItem: false)
        XCTAssertEqual(inside.text, "spec:\n  selector: \n  replicas: 2\n")

        let blank = "spec:\n  containers:\n    \n"
        let item = insertYamlField(blank, utf16Offset: offset(of: "    \n", in: blank) + 4, name: "name", listItem: true)
        XCTAssertEqual(item.text, "spec:\n  containers:\n    - name: \n")
        XCTAssertEqual(item.cursor, offset(of: "name: ", in: item.text) + 6)
    }

    func testDecodeExplainJSON() throws {
        let json = #"{"path":"spec.strategy","type":"DeploymentStrategy","description":"How.","required":false,"children":[{"name":"type","type":"string","description":"Type.","required":false,"enum":["Recreate","RollingUpdate"]}]}"#
        let explain = try TalosJSON.decode(KubeExplain.self, from: json)
        XCTAssertEqual(explain.children.first?.enumValues, ["Recreate", "RollingUpdate"])
        XCTAssertTrue(explain.enumValues.isEmpty)
        XCTAssertFalse(explain.isList)
    }
}
