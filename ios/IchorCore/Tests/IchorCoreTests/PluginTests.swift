import XCTest
@testable import IchorCore

final class PluginTests: XCTestCase {
    /// A golden render tree of the Go core (go/plugin/testdata/render), read in place so the
    /// two never drift: by this file's path (macOS `swift test`), else from the package
    /// directory test-linux.sh runs in (it compiles a copy of this file elsewhere).
    private func golden(_ name: String) throws -> PluginTree {
        let relative = "go/plugin/testdata/render/\(name).json"
        let byFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent(relative)
        let byDirectory = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
            .appendingPathComponent("../../\(relative)").standardizedFileURL
        let url = try XCTUnwrap([byFile, byDirectory].first { FileManager.default.fileExists(atPath: $0.path) },
                                "\(relative) not found")
        return try TalosJSON.decode(PluginTree.self, from: String(decoding: try Data(contentsOf: url), as: UTF8.self))
    }

    private func sections(_ json: String) throws -> [PluginSection] {
        try TalosJSON.decode([PluginSection].self, from: json)
    }

    // MARK: - Golden trees

    func testDecodesKubeGreenCluster() throws {
        let tree = try golden("kube-green-cluster")
        XCTAssertEqual(tree.plugin.id, "kube-green")
        XCTAssertEqual(tree.plugin.category, pluginCategoryDataServices)
        XCTAssertTrue(tree.active)
        XCTAssertEqual(tree.serviceHealth, .warning)
        XCTAssertEqual(tree.summary.line, "2 sleep schedules · 1 asleep · 1 warnings")
        XCTAssertEqual(tree.errorLines, ["secrets: Kubernetes API: permission denied: forbidden"])
        let list = try XCTUnwrap(tree.cards.first)
        XCTAssertEqual(list.rows.map(\.title), ["weekend", "nightly"])
        let weekend = list.rows[0]
        XCTAssertEqual(weekend.ref?.key, "kube-green.com/sleepinfos/demo-lab/weekend")
        XCTAssertTrue(weekend.ref?.isKubeGreenSleepInfo == true)
        XCTAssertEqual(weekend.status?.text, "Endormi")
        XCTAssertEqual(weekend.trailing, "never")
        XCTAssertEqual(weekend.serviceHealth, .warning)
        XCTAssertEqual(weekend.subtitleLine(), "demo-lab · asleep until changed by hand")
        XCTAssertEqual(weekend.subtitleLine(showSubtitle: false), "asleep until changed by hand")
        XCTAssertTrue(weekend.hasDetail)
    }

    func testDecodesKubeGreenSamplesWithTheStateSecretRelation() throws {
        let tree = try golden("kube-green-samples")
        XCTAssertEqual(tree.detectedVia, "demo")
        let weekend = try XCTUnwrap(tree.cards.first?.rows.first)
        let relations = weekend.detail.compactMap { section -> [PluginItem]? in
            if case .relations(let items) = section { return items }
            return nil
        }.flatMap { $0 }
        let secret = try XCTUnwrap(relations.first)
        XCTAssertEqual(secret.label, "State secret")
        XCTAssertEqual(secret.target, "demo-lab/sleepinfo-weekend")
        let resource = try XCTUnwrap(secret.ref).apiResource(kind: "Secret")
        XCTAssertEqual(resource.group, "")
        XCTAssertEqual(resource.version, "v1")
        XCTAssertEqual(resource.resource, "secrets")
        XCTAssertTrue(resource.namespaced)
    }

    func testNamespaceRowsHaveNoDot() throws {
        let tree = try golden("kube-green-samples")
        let namespaces = try XCTUnwrap(tree.cards.first { $0.id == "namespaces" })
        XCTAssertEqual(namespaces.rows.map(\.title), ["demo-lab", "demo-shop"])
        XCTAssertFalse(namespaces.rows.contains { $0.showsDot })
        XCTAssertFalse(namespaces.rows.contains { $0.hasDetail })
        // Rows without objects are never grouped.
        XCTAssertFalse(namespaces.groupsByNamespace)
        XCTAssertEqual(namespaces.rowGroups.count, 1)
    }

    func testWidgetsDecodeEveryPrimitive() throws {
        let tree = try golden("widgets-fr")
        XCTAssertEqual(tree.plugin.category, "workloads")
        XCTAssertEqual(tree.serviceHealth, .critical)
        XCTAssertEqual(tree.errorLines.count, 2)
        XCTAssertTrue(tree.errorLines[1].hasPrefix("fields.widget.broken: "))
        XCTAssertEqual(tree.findings.first?.ref?.name, "beta")
        let types = Set(tree.cards.flatMap(\.rows).flatMap(\.detail).map(\.typeName))
        XCTAssertEqual(types, ["gauge", "text", "conditions", "table", "keyValues", "relations"])
    }

    func testGauge() throws {
        let tree = try golden("widgets-fr")
        let gauges = tree.cards[0].rows[2].detail.compactMap { section -> PluginGauge? in
            if case .gauge(let g) = section { return g }
            return nil
        }
        XCTAssertEqual(gauges.first?.label, "Utilisé")
        XCTAssertEqual(gauges.first?.text, "1000 MiB / 1 GiB")
        XCTAssertEqual(try XCTUnwrap(gauges.first?.fraction), 1048576000.0 / 1073741824.0, accuracy: 1e-9)
        XCTAssertEqual(gauges.last?.fraction ?? 0, 0.2, accuracy: 1e-9)
        let noMax = try sections(#"[{"type":"gauge","label":"Size","value":5,"text":"5"}]"#)
        guard case .gauge(let g) = noMax[0] else { return XCTFail("not a gauge") }
        XCTAssertNil(g.fraction)
        let over = try sections(#"[{"type":"gauge","value":15,"max":10}]"#)
        guard case .gauge(let full) = over[0] else { return XCTFail("not a gauge") }
        XCTAssertEqual(full.fraction, 1)
    }

    func testConditions() throws {
        let tree = try golden("widgets-fr")
        guard case .conditions(let items) = tree.cards[0].rows[0].detail[3] else { return XCTFail("not conditions") }
        XCTAssertEqual(items.first?.type, "Ready")
        XCTAssertEqual(items.first?.reason, "DiskFull")
        XCTAssertEqual(items.first?.tone, "critical")
        guard case .conditions(let ok) = tree.cards[0].rows[2].detail[3] else { return XCTFail("not conditions") }
        XCTAssertEqual(ok.first?.lastTransition, 1_792_058_400_000)
    }

    func testTable() throws {
        let tree = try golden("widgets-fr")
        guard case .table(let table) = tree.cards[0].rows[0].detail[4] else { return XCTFail("not a table") }
        XCTAssertEqual(table.columns, ["Namespace", "Widgets"])
        XCTAssertEqual(table.rows, [["demo-lab", "1"], ["demo-shop", "2"]])
        XCTAssertEqual(table.column(5), "")
    }

    func testTextAndKeyValues() throws {
        let tree = try golden("widgets-fr")
        let detail = tree.cards[0].rows[0].detail
        guard case .text(let text) = detail[2] else { return XCTFail("not a text") }
        XCTAssertEqual(text.text, "beta has 1 replicas")
        XCTAssertTrue(text.mono)
        XCTAssertEqual(text.tone, "info")
        guard case .keyValues(let items) = detail[5] else { return XCTFail("not keyValues") }
        XCTAssertEqual(items.map(\.label), ["Broken", "Ready"])
        XCTAssertEqual(items[0].value, "")
        XCTAssertEqual(items[1].value, "non")
        XCTAssertTrue(items[1].mono)
    }

    func testBadges() throws {
        let tree = try golden("kube-green-cluster")
        guard case .badges(let items) = tree.cards[0].rows[0].detail[1] else { return XCTFail("not badges") }
        XCTAssertEqual(items.map(\.badgeText), ["Deployments", "StatefulSets", "CronJobs"])
        XCTAssertEqual(items.map(\.tone), ["ok", "ok", "idle"])
        XCTAssertEqual(tree.summary.badges.map(\.text), ["1 asleep", "1 warnings"])
    }

    func testActionsStayOffWithTheirReason() throws {
        let tree = try golden("kube-green-cluster")
        let actions = tree.cards[0].rows.map { row -> PluginActionItem? in
            for section in row.detail { if case .actions(let items) = section { return items.first } }
            return nil
        }
        let wake = try XCTUnwrap(actions[0])
        XCTAssertEqual(wake.id, "wakeNow")
        XCTAssertEqual(wake.confirm, "plain")
        XCTAssertTrue(wake.usable)
        // No KubePluginAction in this build: an enabled action is still off, said as such.
        XCTAssertEqual(wake.note, .unavailable)
        let awake = try XCTUnwrap(actions[1])
        XCTAssertFalse(awake.usable)
        XCTAssertEqual(awake.note, .reason("not asleep"))
        let refused = try sections(#"[{"type":"actions","items":[{"id":"a","label":"A","enabled":true,"allowed":false,"params":[{"name":"n","label":"N","type":"enum","values":["x","y"]}]}]}]"#)
        guard case .actions(let items) = refused[0] else { return XCTFail("not actions") }
        XCTAssertFalse(items[0].usable)
        XCTAssertEqual(items[0].note, .notAllowed)
        XCTAssertEqual(items[0].params.first?.values, ["x", "y"])
    }

    // MARK: - Tolerant sections

    func testUnknownSectionTypeIsSkipped() throws {
        let decoded = try sections(#"[{"type":"sparkline","points":[1,2]},{"type":"text","text":"still here"}]"#)
        XCTAssertEqual(decoded.first, .unknown("sparkline"))
        guard case .text(let t) = decoded[1] else { return XCTFail("the next section is lost") }
        XCTAssertEqual(t.text, "still here")
    }

    func testNonObjectSectionIsSkipped() throws {
        let decoded = try sections(#"["text", 3, null, {"type":"badges","items":[{"label":"B"}]}]"#)
        XCTAssertEqual(decoded.prefix(3), [.unknown(""), .unknown(""), .unknown("")])
        guard case .badges(let items) = decoded[3] else { return XCTFail("the next section is lost") }
        XCTAssertEqual(items.first?.label, "B")
    }

    func testKnownTypeWithBadFieldsIsSkipped() throws {
        let decoded = try sections(#"""
        [{"type":"gauge","value":"half"},
         {"type":"keyValues","items":"none"},
         {"type":"table","rows":[[1,2]]},
         {"type":"keyValues","items":[{"label":"Kept","value":"yes"}]}]
        """#)
        XCTAssertEqual(decoded.prefix(3), [.unknown("gauge"), .unknown("keyValues"), .unknown("table")])
        guard case .keyValues(let items) = decoded[3] else { return XCTFail("the next section is lost") }
        XCTAssertEqual(items.first?.value, "yes")
    }

    func testRowWithOnlyUnknownSectionsDoesNotExpand() throws {
        let row = try TalosJSON.decode(PluginRow.self, from: #"{"title":"x","detail":[{"type":"future"}]}"#)
        XCTAssertFalse(row.hasDetail)
        XCTAssertFalse(row.showsDot)
        XCTAssertEqual(row.serviceHealth, .unknown)
        XCTAssertEqual(row.expandKey(card: "c", index: 2), "c|2:x")
    }

    // MARK: - Helpers

    func testGroupsByNamespaceInFirstSeenOrder() throws {
        let tree = try golden("widgets-fr")
        let card = tree.cards[0]
        XCTAssertTrue(card.groupsByNamespace)
        let groups = card.rowGroups
        XCTAssertEqual(groups.map(\.namespace), ["demo-shop", "demo-lab"])
        XCTAssertEqual(groups[0].rows.map(\.row.title), ["beta", "alpha"])
        XCTAssertEqual(groups[0].rows.map(\.index), [0, 2])
        // Cluster-scoped objects (gadgets) stay in one group.
        let gadgets = try XCTUnwrap(tree.cards.first { $0.id == "gadgets" })
        XCTAssertFalse(gadgets.groupsByNamespace)
        // One namespace: no group title needed.
        let one = try TalosJSON.decode(PluginCard.self, from: #"{"id":"c","rows":[{"ref":{"namespace":"a","name":"x"}},{"ref":{"namespace":"a","name":"y"}}]}"#)
        XCTAssertFalse(one.groupsByNamespace)
    }

    func testShownCardsDropEmptyOnesWithoutText() throws {
        let tree = try TalosJSON.decode(PluginTree.self, from: #"{"cards":[{"id":"a","rows":[]},{"id":"b","empty":"None.","rows":[]},{"id":"c"}]}"#)
        XCTAssertEqual(tree.shownCards.map(\.id), ["b"])
        XCTAssertFalse(tree.active)
        XCTAssertEqual(tree.plugin.category, pluginCategoryDataServices)
    }

    func testPluginHealth() {
        XCTAssertEqual(pluginHealth("ok"), .ok)
        XCTAssertEqual(pluginHealth("idle"), .idle)
        XCTAssertEqual(pluginHealth("info"), .idle)
        XCTAssertEqual(pluginHealth("warning"), .warning)
        XCTAssertEqual(pluginHealth("critical"), .critical)
        XCTAssertEqual(pluginHealth("purple"), .unknown)
        XCTAssertEqual(pluginHealth(""), .unknown)
    }

    func testNativeScreenAllowList() {
        XCTAssertEqual(nativePluginScreen(" native:argocd "), "argocd")
        XCTAssertNil(nativePluginScreen(""))
        XCTAssertNil(nativePluginScreen("native:"))
        XCTAssertNil(nativePluginScreen("argocd"))
        XCTAssertEqual(PluginNativeScreen(screen: "native:argocd"), .argoCD)
        XCTAssertEqual(PluginNativeScreen(screen: "native:cluster-lifecycle"), .clusterLifecycle)
        XCTAssertEqual(PluginNativeScreen(screen: "native:longhorn"), .dataService(.longhorn))
        XCTAssertEqual(PluginNativeScreen(screen: "native:kube-green"), .dataService(.kubeGreen))
        // Anything else is the generic screen: a plugin can never send the app elsewhere.
        XCTAssertNil(PluginNativeScreen(screen: "native:settings"))
        XCTAssertNil(PluginNativeScreen(screen: "native:https://example.org"))
        XCTAssertNil(PluginNativeScreen(screen: ""))
    }

    func testDetectSplitsTabsAndTiles() throws {
        let detect = try TalosJSON.decode(PluginDetect.self, from: #"""
        {"items":[
          {"id":"kube-green","name":"kube-green","icon":"kube-green","category":"data-services","active":true,"detectedVia":"api",
           "summary":{"title":"2 sleep schedules","health":"warning","badges":[{"label":"asleep","value":"1","tone":"info"}]}},
          {"id":"pg","name":"PG","category":"data-services","screen":"native:cloudnative-pg","active":true,"summary":null},
          {"id":"widgets","name":"Widgets","category":"networking","active":true},
          {"id":"off","name":"Off","category":"data-services","active":false}]}
        """#)
        XCTAssertEqual(detect.active.map(\.id), ["kube-green", "pg", "widgets"])
        XCTAssertEqual(detect.dataServicePlugins.map(\.id), ["kube-green", "pg"])
        XCTAssertEqual(detect.dataServiceTabs.map(\.id), ["kube-green"])
        XCTAssertEqual(detect.homeTiles.map(\.id), ["widgets"])
        XCTAssertEqual(detect.items[0].summary?.line, "2 sleep schedules · 1 asleep")
        XCTAssertEqual(detect.items[0].summary?.serviceHealth, .warning)
        XCTAssertEqual(detect.items[0].dataServiceKind, .kubeGreen)
        XCTAssertNil(detect.items[1].summary)
        XCTAssertEqual(detect.items[1].nativeScreen, .dataService(.cnpg))
        XCTAssertEqual(detect.items[2].toolFamily, .networking)
        let tabs = DataServicesTab.tabs(detected: [.longhorn], plugins: detect)
        XCTAssertEqual(tabs, [.native(.longhorn), .plugin(id: "kube-green", name: "kube-green")])
        XCTAssertEqual(tabs.map(\.id), ["longhorn", "plugin:kube-green"])
        XCTAssertEqual(DataServicesTab.tabs(detected: [], plugins: nil), [])
    }

    func testToolFamilies() {
        XCTAssertEqual(PluginToolFamily(category: "compute"), .workloads)
        XCTAssertEqual(PluginToolFamily(category: "networking"), .networking)
        XCTAssertEqual(PluginToolFamily(category: "security"), .observe)
        XCTAssertEqual(PluginToolFamily(category: "anything"), .browse)
    }

    func testCatalogKinds() throws {
        let catalog = try TalosJSON.decode(PluginCatalog.self, from: #"""
        {"items":[{"id":"kube-green","name":"kube-green","category":"data-services",
          "resources":[{"group":"kube-green.com","version":"v1alpha1","resource":"sleepinfos","kind":"SleepInfo"},
                       {"group":"","version":"v1","resource":"secrets","kind":"Secret"}],
          "actions":[{"id":"wakeNow","label":"Wake up now","verb":"patch"}]}]}
        """#)
        let secret = PluginRef(group: "", version: "v1", resource: "secrets", namespace: "demo-lab", name: "s")
        XCTAssertEqual(catalog.kindOf("kube-green", secret), "Secret")
        XCTAssertEqual(catalog.kindOf("kube-green", PluginRef(group: "kube-green.com", resource: "sleepinfos")), "SleepInfo")
        XCTAssertEqual(catalog.kindOf("other", secret), "")
        XCTAssertEqual(catalog.kindOf("kube-green", PluginRef(resource: "pods")), "")
        let clusterScoped = PluginRef(group: "widgets.example.com", version: "", resource: "gadgets", name: "hub").apiResource(kind: "")
        XCTAssertFalse(clusterScoped.namespaced)
        XCTAssertEqual(clusterScoped.version, "v1")
    }

    func testReloadResetsOnAnotherScopeAndRefreshesInPlaceOtherwise() {
        let first = PluginReload(scope: "ctx-a#1", bump: 0)
        XCTAssertEqual(first.change(from: nil), .reset)
        XCTAssertEqual(first.change(from: first), .none)
        XCTAssertEqual(PluginReload(scope: "ctx-a#1", bump: 1).change(from: first), .refresh)
        XCTAssertEqual(PluginReload(scope: "ctx-b#1", bump: 0).change(from: first), .reset)
        XCTAssertEqual(PluginReload(scope: "ctx-a#2", bump: 3).change(from: first), .reset)
    }
}

private extension PluginSection {
    var typeName: String {
        switch self {
        case .keyValues: "keyValues"
        case .badges: "badges"
        case .gauge: "gauge"
        case .conditions: "conditions"
        case .relations: "relations"
        case .table: "table"
        case .text: "text"
        case .actions: "actions"
        case .unknown(let type): "unknown:\(type)"
        }
    }
}
