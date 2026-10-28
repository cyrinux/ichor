import XCTest
@testable import IchorCore

/// The facade contract (CYR-440): each golden fixture Go writes (go/ichorgo/testdata/contract,
/// `just contract-update`) must decode into its model, and every key Go wrote must be one the
/// model asks for (KeyRecorder). A renamed Go json tag shows up here as an unread key, instead
/// of a silent default in the app. `ignored` lists the keys the app does not read on purpose.
final class ContractTests: XCTestCase {
    private struct Row {
        let check: (Data, KeyRecorder) throws -> Void
        var ignored: Set<String> = []
    }

    private static func row<T: Decodable>(_ type: T.Type, ignored: Set<String> = [], _ assert: @escaping (T) -> Void = { _ in }) -> Row {
        Row(check: { data, recorder in
            // The app's own decoder must take it too.
            assert(try TalosJSON.decode(type, from: String(decoding: data, as: UTF8.self)))
            _ = try recorder.decode(type, from: data)
        }, ignored: ignored)
    }

    private let rows: [String: Row] = [
        "cluster-overview": ContractTests.row(ClusterOverview.self) { XCTAssertEqual($0.context, "demo"); XCTAssertFalse($0.nodes.isEmpty) },
        "kube-pods": ContractTests.row(KubePageWire<KubePod>.self) { XCTAssertFalse($0.items.isEmpty) },
        "kube-workloads": ContractTests.row(KubeWorkloadList.self),
        "kube-cronjobs": ContractTests.row(KubePageWire<KubeCronJob>.self) { XCTAssertFalse($0.items.isEmpty) },
        "kube-nodes": ContractTests.row(KubeNodesOverview.self),
        "argocd": ContractTests.row(ArgoStatus.self),
        "flux": ContractTests.row(FluxStatus.self),
        "alerts": ContractTests.row(AMAlerts.self),
        "checkup": ContractTests.row(CheckupReport.self),
        "inventory": ContractTests.row(ClusterInventory.self),
        "kubespan": ContractTests.row(KubeSpanOverview.self),
        "topology": ContractTests.row(ClusterTopology.self),
    ]

    /// go/ichorgo/testdata/contract, from where `swift test` (ios/IchorCore) or test-linux.sh
    /// (which copies the tests elsewhere first) runs.
    private func fixtureDir() throws -> URL {
        let candidates = [
            URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent("../../go/ichorgo/testdata/contract"),
            URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../../go/ichorgo/testdata/contract"),
        ]
        var isDir: ObjCBool = false
        guard let dir = candidates.first(where: { FileManager.default.fileExists(atPath: $0.path, isDirectory: &isDir) && isDir.boolValue }) else {
            throw XCTSkip("go/ichorgo/testdata/contract not found from \(FileManager.default.currentDirectoryPath)")
        }
        return dir.standardizedFileURL
    }

    func testEveryFixtureHasARow() throws {
        let names = try FileManager.default.contentsOfDirectory(atPath: fixtureDir().path)
            .filter { $0.hasSuffix(".json") }.map { String($0.dropLast(5)) }
        XCTAssertEqual(Set(names), Set(rows.keys), "fixtures without a row here (or rows without a fixture)")
    }

    func testEveryKeyGoWritesIsOneTheModelReads() throws {
        let dir = try fixtureDir()
        var problems: [String] = []
        for (name, row) in rows.sorted(by: { $0.key < $1.key }) {
            let data = try Data(contentsOf: dir.appendingPathComponent("\(name).json"))
            let recorder = KeyRecorder()
            do {
                try row.check(data, recorder)
            } catch {
                problems.append("\(name): does not decode: \(error)")
                continue
            }
            let written = KeyRecorder.paths(of: try JSONSerialization.jsonObject(with: data))
            let unread = written.subtracting(recorder.paths).filter { path in
                !row.ignored.contains { path == $0 || path.hasPrefix($0 + ".") || path.hasPrefix($0 + "[]") }
            }
            problems += unread.sorted().map { "\(name): \($0)" }
        }
        XCTAssertTrue(problems.isEmpty, "keys Go writes that the Swift models do not read:\n" + problems.joined(separator: "\n"))
    }

    func testTheRecorderSeesARenamedKey() throws {
        struct Model: Decodable { let name: String; let size: Int? }
        let recorder = KeyRecorder()
        let data = Data(#"{"name":"a","sise":1}"#.utf8)
        _ = try recorder.decode(Model.self, from: data)
        let written = KeyRecorder.paths(of: try JSONSerialization.jsonObject(with: data))
        XCTAssertEqual(written.subtracting(recorder.paths), ["sise"])
    }
}

/// A `Decoder` over a JSONSerialization tree that records every key a model asks for (decode,
/// decodeIfPresent, contains, nested containers), as dotted paths with `[]` for array items. The
/// facade contract test (ContractTests) compares them with the keys a fixture holds: a key Go
/// writes that no model asks for is a renamed or forgotten field. Swift has no "unknown key
/// fails" mode, and most models are Decodable only, so the keys asked are the strictness.
final class KeyRecorder {
    private(set) var paths: Set<String> = []

    func record(_ path: [CodingKey], _ key: CodingKey) { paths.insert(KeyRecorder.path(path + [key])) }

    static func path(_ keys: [CodingKey]) -> String {
        keys.map { $0.intValue == nil ? $0.stringValue : "[]" }.joined(separator: ".").replacingOccurrences(of: ".[]", with: "[]")
    }

    /// `json`'s key paths, in the same form (array items under `[]`, all of them).
    static func paths(of json: Any, at prefix: String = "") -> Set<String> {
        switch json {
        case let object as [String: Any]:
            return object.reduce(into: Set<String>()) { out, entry in
                let at = prefix.isEmpty ? entry.key : "\(prefix).\(entry.key)"
                out.insert(at)
                out.formUnion(paths(of: entry.value, at: at))
            }
        case let array as [Any]:
            return array.reduce(into: Set<String>()) { $0.formUnion(paths(of: $1, at: prefix + "[]")) }
        default:
            return []
        }
    }

    /// Decodes `type` from `data` through the recording decoder.
    func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        let tree = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        return try T(from: ContractDecoder(value: tree, codingPath: [], recorder: self))
    }
}

private struct ContractIndexKey: CodingKey {
    var stringValue: String
    var intValue: Int?
    init(stringValue: String) { self.stringValue = stringValue }
    init(intValue: Int) { stringValue = "\(intValue)"; self.intValue = intValue }
}

private struct ContractDecoder: Decoder {
    let value: Any
    let codingPath: [CodingKey]
    let recorder: KeyRecorder
    var userInfo: [CodingUserInfoKey: Any] { [:] }

    func container<Key: CodingKey>(keyedBy type: Key.Type) throws -> KeyedDecodingContainer<Key> {
        guard let object = value as? [String: Any] else { throw contractMismatch([String: Any].self, codingPath) }
        return KeyedDecodingContainer(ContractKeyed<Key>(object: object, codingPath: codingPath, recorder: recorder))
    }

    func unkeyedContainer() throws -> UnkeyedDecodingContainer {
        guard let array = value as? [Any] else { throw contractMismatch([Any].self, codingPath) }
        return ContractUnkeyed(array: array, codingPath: codingPath, recorder: recorder)
    }

    func singleValueContainer() throws -> SingleValueDecodingContainer {
        ContractSingle(value: value, codingPath: codingPath, recorder: recorder)
    }
}

private func contractMismatch(_ type: Any.Type, _ path: [CodingKey]) -> DecodingError {
    .typeMismatch(type, .init(codingPath: path, debugDescription: "not a \(type)"))
}

/// `value` as `T`: JSONSerialization gives NSNumber for numbers and booleans.
private func contractUnbox<T: Decodable>(_ value: Any, as type: T.Type, path: [CodingKey], recorder: KeyRecorder) throws -> T {
    if value is NSNull { throw DecodingError.valueNotFound(type, .init(codingPath: path, debugDescription: "null")) }
    let number = value as? NSNumber
    let out: Any? = switch type {
    case is String.Type: value as? String
    case is Bool.Type: (value as? Bool) ?? number?.boolValue
    case is Double.Type: number?.doubleValue
    case is Float.Type: number?.floatValue
    case is Int.Type: number?.intValue
    case is Int8.Type: number?.int8Value
    case is Int16.Type: number?.int16Value
    case is Int32.Type: number?.int32Value
    case is Int64.Type: number?.int64Value
    case is UInt.Type: number?.uintValue
    case is UInt8.Type: number?.uint8Value
    case is UInt16.Type: number?.uint16Value
    case is UInt32.Type: number?.uint32Value
    case is UInt64.Type: number?.uint64Value
    default: try T(from: ContractDecoder(value: value, codingPath: path, recorder: recorder))
    }
    guard let typed = out as? T else { throw contractMismatch(type, path) }
    return typed
}

private struct ContractKeyed<Key: CodingKey>: KeyedDecodingContainerProtocol {
    let object: [String: Any]
    let codingPath: [CodingKey]
    let recorder: KeyRecorder

    var allKeys: [Key] { object.keys.compactMap(Key.init(stringValue:)) }

    func contains(_ key: Key) -> Bool {
        recorder.record(codingPath, key)
        return object[key.stringValue] != nil
    }

    private func value(_ key: Key) throws -> Any {
        recorder.record(codingPath, key)
        guard let value = object[key.stringValue] else {
            throw DecodingError.keyNotFound(key, .init(codingPath: codingPath, debugDescription: "missing"))
        }
        return value
    }

    func decodeNil(forKey key: Key) throws -> Bool { try value(key) is NSNull }

    func decode<T: Decodable>(_ type: T.Type, forKey key: Key) throws -> T { try decode(type, key) }

    private func decode<T: Decodable>(_ type: T.Type, _ key: Key) throws -> T {
        try contractUnbox(try value(key), as: type, path: codingPath + [key], recorder: recorder)
    }

    // Each primitive spelled out: the protocol requires them one by one.
    func decode(_ type: Bool.Type, forKey key: Key) throws -> Bool { try decode(type as Bool.Type, key) }
    func decode(_ type: String.Type, forKey key: Key) throws -> String { try decode(type as String.Type, key) }
    func decode(_ type: Double.Type, forKey key: Key) throws -> Double { try decode(type as Double.Type, key) }
    func decode(_ type: Float.Type, forKey key: Key) throws -> Float { try decode(type as Float.Type, key) }
    func decode(_ type: Int.Type, forKey key: Key) throws -> Int { try decode(type as Int.Type, key) }
    func decode(_ type: Int8.Type, forKey key: Key) throws -> Int8 { try decode(type as Int8.Type, key) }
    func decode(_ type: Int16.Type, forKey key: Key) throws -> Int16 { try decode(type as Int16.Type, key) }
    func decode(_ type: Int32.Type, forKey key: Key) throws -> Int32 { try decode(type as Int32.Type, key) }
    func decode(_ type: Int64.Type, forKey key: Key) throws -> Int64 { try decode(type as Int64.Type, key) }
    func decode(_ type: UInt.Type, forKey key: Key) throws -> UInt { try decode(type as UInt.Type, key) }
    func decode(_ type: UInt8.Type, forKey key: Key) throws -> UInt8 { try decode(type as UInt8.Type, key) }
    func decode(_ type: UInt16.Type, forKey key: Key) throws -> UInt16 { try decode(type as UInt16.Type, key) }
    func decode(_ type: UInt32.Type, forKey key: Key) throws -> UInt32 { try decode(type as UInt32.Type, key) }
    func decode(_ type: UInt64.Type, forKey key: Key) throws -> UInt64 { try decode(type as UInt64.Type, key) }

    func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type, forKey key: Key) throws -> KeyedDecodingContainer<NestedKey> {
        try ContractDecoder(value: try value(key), codingPath: codingPath + [key], recorder: recorder).container(keyedBy: type)
    }

    func nestedUnkeyedContainer(forKey key: Key) throws -> UnkeyedDecodingContainer {
        try ContractDecoder(value: try value(key), codingPath: codingPath + [key], recorder: recorder).unkeyedContainer()
    }

    func superDecoder() throws -> Decoder { ContractDecoder(value: object, codingPath: codingPath, recorder: recorder) }

    func superDecoder(forKey key: Key) throws -> Decoder {
        ContractDecoder(value: try value(key), codingPath: codingPath + [key], recorder: recorder)
    }
}

private struct ContractUnkeyed: UnkeyedDecodingContainer {
    let array: [Any]
    let codingPath: [CodingKey]
    let recorder: KeyRecorder
    var currentIndex = 0

    var count: Int? { array.count }
    var isAtEnd: Bool { currentIndex >= array.count }

    init(array: [Any], codingPath: [CodingKey], recorder: KeyRecorder) {
        self.array = array
        self.codingPath = codingPath
        self.recorder = recorder
    }

    private mutating func next() throws -> (Any, [CodingKey]) {
        guard !isAtEnd else { throw DecodingError.valueNotFound(Any.self, .init(codingPath: codingPath, debugDescription: "end of array")) }
        defer { currentIndex += 1 }
        return (array[currentIndex], codingPath + [ContractIndexKey(intValue: currentIndex)])
    }

    mutating func decodeNil() throws -> Bool {
        guard !isAtEnd, array[currentIndex] is NSNull else { return false }
        currentIndex += 1
        return true
    }

    mutating func decode<T: Decodable>(_ type: T.Type) throws -> T { try decodeNext(type) }

    private mutating func decodeNext<T: Decodable>(_ type: T.Type) throws -> T {
        let (value, path) = try next()
        return try contractUnbox(value, as: type, path: path, recorder: recorder)
    }

    mutating func decode(_ type: Bool.Type) throws -> Bool { try decodeNext(type) }
    mutating func decode(_ type: String.Type) throws -> String { try decodeNext(type) }
    mutating func decode(_ type: Double.Type) throws -> Double { try decodeNext(type) }
    mutating func decode(_ type: Float.Type) throws -> Float { try decodeNext(type) }
    mutating func decode(_ type: Int.Type) throws -> Int { try decodeNext(type) }
    mutating func decode(_ type: Int8.Type) throws -> Int8 { try decodeNext(type) }
    mutating func decode(_ type: Int16.Type) throws -> Int16 { try decodeNext(type) }
    mutating func decode(_ type: Int32.Type) throws -> Int32 { try decodeNext(type) }
    mutating func decode(_ type: Int64.Type) throws -> Int64 { try decodeNext(type) }
    mutating func decode(_ type: UInt.Type) throws -> UInt { try decodeNext(type) }
    mutating func decode(_ type: UInt8.Type) throws -> UInt8 { try decodeNext(type) }
    mutating func decode(_ type: UInt16.Type) throws -> UInt16 { try decodeNext(type) }
    mutating func decode(_ type: UInt32.Type) throws -> UInt32 { try decodeNext(type) }
    mutating func decode(_ type: UInt64.Type) throws -> UInt64 { try decodeNext(type) }

    mutating func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type) throws -> KeyedDecodingContainer<NestedKey> {
        let (value, path) = try next()
        return try ContractDecoder(value: value, codingPath: path, recorder: recorder).container(keyedBy: type)
    }

    mutating func nestedUnkeyedContainer() throws -> UnkeyedDecodingContainer {
        let (value, path) = try next()
        return try ContractDecoder(value: value, codingPath: path, recorder: recorder).unkeyedContainer()
    }

    mutating func superDecoder() throws -> Decoder {
        let (value, path) = try next()
        return ContractDecoder(value: value, codingPath: path, recorder: recorder)
    }
}

private struct ContractSingle: SingleValueDecodingContainer {
    let value: Any
    let codingPath: [CodingKey]
    let recorder: KeyRecorder

    func decodeNil() -> Bool { value is NSNull }

    func decode<T: Decodable>(_ type: T.Type) throws -> T {
        try contractUnbox(value, as: type, path: codingPath, recorder: recorder)
    }

    func decode(_ type: Bool.Type) throws -> Bool { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: String.Type) throws -> String { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Double.Type) throws -> Double { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Float.Type) throws -> Float { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Int.Type) throws -> Int { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Int8.Type) throws -> Int8 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Int16.Type) throws -> Int16 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Int32.Type) throws -> Int32 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: Int64.Type) throws -> Int64 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: UInt.Type) throws -> UInt { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: UInt8.Type) throws -> UInt8 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: UInt16.Type) throws -> UInt16 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: UInt32.Type) throws -> UInt32 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
    func decode(_ type: UInt64.Type) throws -> UInt64 { try contractUnbox(value, as: type, path: codingPath, recorder: recorder) }
}
