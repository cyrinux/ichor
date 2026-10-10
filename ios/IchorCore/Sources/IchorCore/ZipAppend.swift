import Foundation

// One file added to a zip the Go core wrote (the support bundle gets the history export): the
// entries stay where they are, only the central directory moves after the new one. Stored, not
// deflated (a few KiB of JSON). Zip64 archives are left alone.

/// How to add the entry: cut the archive at `truncateAt`, then write `tail` (the new entry, the
/// central directory with it, the end record).
public struct ZipAppendPlan: Equatable, Sendable {
    public let truncateAt: Int
    public let tail: Data
}

/// The plan to add `name` holding `content` to `zip`; nil when `zip` is not a plain zip (no end
/// record, zip64, a central directory that does not add up) or already has `name`.
public func zipAppendPlan(_ zip: Data, name: String, content: Data, date: Date,
                          timeZone: TimeZone = .current) -> ZipAppendPlan? {
    let bytes = [UInt8](zip.suffix(min(zip.count, 22 + 0xFFFF)))
    let base = zip.count - bytes.count
    guard let end = endRecord(bytes) else { return nil }
    let entries = Int(u16(bytes, end + 10))
    let cdSize = Int(u32(bytes, end + 12))
    let cdOffset = Int(u32(bytes, end + 16))
    let commentLength = Int(u16(bytes, end + 20))
    // Zip64 (or a multi-disk archive): not ours to touch.
    guard u16(bytes, end + 4) == 0, u16(bytes, end + 6) == 0, entries < 0xFFFF, cdSize < 0xFFFF_FFFF, cdOffset < 0xFFFF_FFFF,
          cdOffset + cdSize == base + end, end + 22 + commentLength <= bytes.count else { return nil }
    let directory = [UInt8](zip[zip.startIndex + cdOffset ..< zip.startIndex + cdOffset + cdSize])
    guard let names = centralNames(directory, count: entries), !names.contains(name) else { return nil }

    let nameBytes = [UInt8](name.utf8)
    let crc = crc32(content)
    let (time, day) = dosDateTime(date, timeZone: timeZone)
    var local: [UInt8] = []
    put32(&local, 0x0403_4B50)
    put16(&local, 20) // version needed
    put16(&local, 0x0800) // UTF-8 name
    put16(&local, 0) // stored
    put16(&local, time)
    put16(&local, day)
    put32(&local, crc)
    put32(&local, UInt32(content.count))
    put32(&local, UInt32(content.count))
    put16(&local, UInt16(nameBytes.count))
    put16(&local, 0)
    local += nameBytes

    var central: [UInt8] = []
    put32(&central, 0x0201_4B50)
    put16(&central, 0x031E) // made by: Unix, 3.0
    put16(&central, 20)
    put16(&central, 0x0800)
    put16(&central, 0)
    put16(&central, time)
    put16(&central, day)
    put32(&central, crc)
    put32(&central, UInt32(content.count))
    put32(&central, UInt32(content.count))
    put16(&central, UInt16(nameBytes.count))
    put16(&central, 0) // extra
    put16(&central, 0) // comment
    put16(&central, 0) // disk
    put16(&central, 0) // internal attributes
    put32(&central, 0o100644 << 16) // a regular file, rw-r--r--
    put32(&central, UInt32(cdOffset))
    central += nameBytes

    let newOffset = cdOffset + local.count + content.count
    let newSize = cdSize + central.count
    guard newOffset + newSize < 0xFFFF_FFFF, nameBytes.count < 0xFFFF else { return nil }
    var record: [UInt8] = []
    put32(&record, 0x0605_4B50)
    put16(&record, 0)
    put16(&record, 0)
    put16(&record, UInt16(entries + 1))
    put16(&record, UInt16(entries + 1))
    put32(&record, UInt32(newSize))
    put32(&record, UInt32(newOffset))
    put16(&record, UInt16(commentLength))
    record += bytes[(end + 22) ..< (end + 22 + commentLength)]

    var tail = Data(local)
    tail.append(content)
    tail.append(contentsOf: directory)
    tail.append(contentsOf: central)
    tail.append(contentsOf: record)
    return ZipAppendPlan(truncateAt: cdOffset, tail: tail)
}

/// The offset in `bytes` of the end of central directory record, searched from the end.
private func endRecord(_ bytes: [UInt8]) -> Int? {
    guard bytes.count >= 22 else { return nil }
    var i = bytes.count - 22
    while i >= 0 {
        if u32(bytes, i) == 0x0605_4B50 {
            // A zip64 locator right before it: a zip64 archive.
            if i >= 20, u32(bytes, i - 20) == 0x0706_4B50 { return nil }
            return i
        }
        i -= 1
    }
    return nil
}

/// The entry names of a central directory of `count` entries; nil when it does not parse.
private func centralNames(_ directory: [UInt8], count: Int) -> [String]? {
    var names: [String] = []
    var at = 0
    for _ in 0 ..< count {
        guard at + 46 <= directory.count, u32(directory, at) == 0x0201_4B50 else { return nil }
        let nameLength = Int(u16(directory, at + 28))
        let extra = Int(u16(directory, at + 30))
        let comment = Int(u16(directory, at + 32))
        guard at + 46 + nameLength <= directory.count else { return nil }
        names.append(String(decoding: directory[(at + 46) ..< (at + 46 + nameLength)], as: UTF8.self))
        at += 46 + nameLength + extra + comment
    }
    return at == directory.count ? names : nil
}

/// MS-DOS time and date of `date` in `timeZone` (2-second steps, from 1980).
func dosDateTime(_ date: Date, timeZone: TimeZone) -> (time: UInt16, date: UInt16) {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = timeZone
    let c = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
    let year: Int = max(1980, min(2107, c.year ?? 1980))
    let hour: Int = c.hour ?? 0
    let minute: Int = c.minute ?? 0
    let second: Int = c.second ?? 0
    let month: Int = c.month ?? 1
    let dayOfMonth: Int = c.day ?? 1
    let time = UInt16(hour << 11 | minute << 5 | second / 2)
    let day = UInt16((year - 1980) << 9 | month << 5 | dayOfMonth)
    return (time, day)
}

/// CRC-32 (IEEE), as zip uses it.
func crc32(_ data: Data) -> UInt32 {
    var crc: UInt32 = 0xFFFF_FFFF
    for byte in data {
        crc ^= UInt32(byte)
        for _ in 0 ..< 8 { crc = (crc & 1) == 1 ? (crc >> 1) ^ 0xEDB8_8320 : crc >> 1 }
    }
    return ~crc
}

private func u16(_ b: [UInt8], _ i: Int) -> UInt16 { UInt16(b[i]) | UInt16(b[i + 1]) << 8 }
private func u32(_ b: [UInt8], _ i: Int) -> UInt32 { UInt32(u16(b, i)) | UInt32(u16(b, i + 2)) << 16 }
private func put16(_ b: inout [UInt8], _ v: UInt16) { b += [UInt8(v & 0xFF), UInt8(v >> 8)] }
private func put32(_ b: inout [UInt8], _ v: UInt32) { put16(&b, UInt16(v & 0xFFFF)); put16(&b, UInt16(v >> 16)) }
