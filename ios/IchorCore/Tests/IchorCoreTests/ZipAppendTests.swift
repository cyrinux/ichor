import XCTest
@testable import IchorCore

final class ZipAppendTests: XCTestCase {
    private let date = Date(timeIntervalSince1970: 1_800_000_000) // 2027-01-15 08:00:00 UTC
    private let utc = TimeZone(identifier: "UTC")!

    /// A zip with no entry: the end record alone.
    private let empty = Data([0x50, 0x4B, 0x05, 0x06] + [UInt8](repeating: 0, count: 18))

    /// A zip Python's zipfile wrote: "a.txt" holding "hi" (stored), and the comment "c".
    private let python = Data(base64Encoded: "UEsDBBQAAAAAAAAAIVCsKpPYAgAAAAIAAAAFAAAAYS50eHRoaVBLAQIUAxQAAAAAAAAAIVCsKpPYAgAAAAIAAAAFAAAAAAAAAAAAAACAAQAAAABhLnR4dFBLBQYAAAAAAQABADMAAAAlAAAAAQBj")!

    private func applied(_ zip: Data, _ plan: ZipAppendPlan) -> Data {
        var out = Data(zip.prefix(plan.truncateAt))
        out.append(plan.tail)
        return out
    }

    private func u16(_ d: Data, _ i: Int) -> Int { Int(d[d.startIndex + i]) | Int(d[d.startIndex + i + 1]) << 8 }
    private func u32(_ d: Data, _ i: Int) -> Int { u16(d, i) | u16(d, i + 2) << 16 }

    func testCRC32() {
        XCTAssertEqual(crc32(Data("123456789".utf8)), 0xCBF4_3926)
        XCTAssertEqual(crc32(Data()), 0)
    }

    func testDosDateTime() {
        let (time, day) = dosDateTime(date, timeZone: utc)
        XCTAssertEqual(Int(day) >> 9, 2027 - 1980)
        XCTAssertEqual((Int(day) >> 5) & 0xF, 1)
        XCTAssertEqual(Int(day) & 0x1F, 15)
        XCTAssertEqual(Int(time) >> 11, 8)
        XCTAssertEqual(Int(time) & 0x1F, 0)
    }

    func testAddsToAnEmptyZip() throws {
        let content = Data(#"{"format":1}"#.utf8)
        let plan = try XCTUnwrap(zipAppendPlan(empty, name: "cluster/history.json", content: content, date: date, timeZone: utc))
        XCTAssertEqual(plan.truncateAt, 0)
        let zip = applied(empty, plan)
        let end = zip.count - 22
        XCTAssertEqual(u32(zip, end), 0x0605_4B50)
        XCTAssertEqual(u16(zip, end + 10), 1)
        let cdOffset = u32(zip, end + 16)
        XCTAssertEqual(cdOffset, 30 + 20 + content.count)
        XCTAssertEqual(u32(zip, cdOffset), 0x0201_4B50)
        XCTAssertEqual(u32(zip, 0), 0x0403_4B50)
        XCTAssertEqual(UInt32(u32(zip, 14)), crc32(content))
        XCTAssertEqual(zip.subdata(in: 50 ..< 50 + content.count), content)
    }

    func testAddsAfterTheEntriesAndKeepsTheComment() throws {
        let plan = try XCTUnwrap(zipAppendPlan(python, name: "b.json", content: Data("{}".utf8), date: date, timeZone: utc))
        let zip = applied(python, plan)
        XCTAssertEqual(zip.prefix(plan.truncateAt), python.prefix(plan.truncateAt)) // a.txt untouched
        let end = zip.count - 23
        XCTAssertEqual(u32(zip, end), 0x0605_4B50)
        XCTAssertEqual(u16(zip, end + 10), 2)
        XCTAssertEqual(u16(zip, end + 20), 1)
        XCTAssertEqual(zip.last, UInt8(ascii: "c"))
        // Both names are listed now: adding either again is refused.
        XCTAssertNil(zipAppendPlan(zip, name: "a.txt", content: Data(), date: date))
        XCTAssertNil(zipAppendPlan(zip, name: "b.json", content: Data(), date: date))
        XCTAssertNotNil(zipAppendPlan(zip, name: "c.json", content: Data(), date: date))
    }

    func testLeavesWhatIsNotAPlainZip() {
        XCTAssertNil(zipAppendPlan(Data("not a zip at all, just text".utf8), name: "x", content: Data(), date: date))
        XCTAssertNil(zipAppendPlan(Data(), name: "x", content: Data(), date: date))
        // An end record whose directory points past it.
        var broken = [UInt8](empty)
        broken[16] = 9
        XCTAssertNil(zipAppendPlan(Data(broken), name: "x", content: Data(), date: date))
        // Zip64: the entry count says "see the zip64 record".
        var zip64 = [UInt8](empty)
        zip64[10] = 0xFF
        zip64[11] = 0xFF
        XCTAssertNil(zipAppendPlan(Data(zip64), name: "x", content: Data(), date: date))
    }
}
