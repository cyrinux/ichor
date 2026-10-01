import XCTest
@testable import IchorCore

final class ChangelogTests: XCTestCase {
    private let json = """
    {"generated_at":"2026-10-01T20:00:00+00:00","releases":[
      {"version":"0.4.1","build_number":61,"published_at":"2026-10-01T18:25:00+00:00","sections":[
        {"kind":"new","title":"New","items":["Support bundle","Resources browser"]},
        {"kind":"security","title":"Security","items":["Hardened storage"]},
        {"kind":"fixed","title":"Fixed","items":[]}]},
      {"version":"0.4.0","build_number":52,"published_at":"2026-09-20T09:00:00+00:00","sections":[]},
      {"version":"0.3.0","build_number":37,"published_at":"","sections":[{"kind":"breaking","title":"Breaking","items":["New config format"]}]}]}
    """

    private var releases: [ChangelogRelease] { decodeChangelog(Data(json.utf8)).releases }

    func testDecoding() {
        XCTAssertEqual(releases.map(\.version), ["0.4.1", "0.4.0", "0.3.0"])
        XCTAssertEqual(releases[0].buildNumber, 61)
        // A section without items is dropped.
        XCTAssertEqual(releases[0].sections.map(\.kind), ["new", "security"])
        XCTAssertEqual(releases[0].sections[0].items, ["Support bundle", "Resources browser"])
        XCTAssertEqual(releases[1].sections, [])
        XCTAssertEqual(releases[0].published, Date(timeIntervalSince1970: 1_790_879_100))
        XCTAssertNil(releases[2].published)
    }

    func testMissingOrCorruptFile() {
        XCTAssertEqual(decodeChangelog(nil), Changelog())
        XCTAssertEqual(decodeChangelog(Data("not json".utf8)), Changelog())
        XCTAssertEqual(decodeChangelog(Data("{}".utf8)), Changelog())
        XCTAssertEqual(decodeChangelog(Data(#"{"releases":[{"version":"1.0.0"}]}"#.utf8)).releases, [ChangelogRelease(version: "1.0.0", buildNumber: 0)])
    }

    func testTitleByKind() {
        XCTAssertEqual(ChangelogSection(kind: "new", title: "x").englishTitle, "New")
        XCTAssertEqual(ChangelogSection(kind: "fixed").englishTitle, "Fixed")
        XCTAssertEqual(ChangelogSection(kind: "faster").englishTitle, "Faster")
        // The generator's own title for this kind is "Breaking".
        XCTAssertEqual(ChangelogSection(kind: "breaking", title: "Breaking").englishTitle, "Breaking changes")
        XCTAssertEqual(ChangelogSection(kind: "Breaking").knownKind, .breaking)
        // An unknown kind keeps the JSON title.
        XCTAssertNil(ChangelogSection(kind: "security", title: "Security").knownKind)
        XCTAssertEqual(ChangelogSection(kind: "security", title: "Security").englishTitle, "Security")
        XCTAssertEqual(ChangelogSection(kind: "security").englishTitle, "security")
    }

    func testReleasesSince() {
        XCTAssertEqual(releasesSince(releases, build: 37).map(\.version), ["0.4.1", "0.4.0"])
        XCTAssertEqual(releasesSince(releases, build: 60).map(\.version), ["0.4.1"])
        XCTAssertEqual(releasesSince(releases, build: 61), [])
        // Newest first whatever the file's order.
        XCTAssertEqual(releasesSince(releases.reversed(), build: 0).map(\.buildNumber), [61, 52, 37])
    }

    func testWhatsNew() {
        // First install: nothing.
        XCTAssertEqual(whatsNewReleases(previousBuild: nil, currentBuild: 61, releases: releases), [])
        // Same build, or a downgrade: nothing.
        XCTAssertEqual(whatsNewReleases(previousBuild: 61, currentBuild: 61, releases: releases), [])
        XCTAssertEqual(whatsNewReleases(previousBuild: 61, currentBuild: 52, releases: releases), [])
        // An update: every release since the previous build, maintenance ones included.
        XCTAssertEqual(whatsNewReleases(previousBuild: 37, currentBuild: 61, releases: releases).map(\.version), ["0.4.1", "0.4.0"])
        // Nothing newer in the bundled history, or no history at all.
        XCTAssertEqual(whatsNewReleases(previousBuild: 61, currentBuild: 70, releases: releases), [])
        XCTAssertEqual(whatsNewReleases(previousBuild: 37, currentBuild: 61, releases: []), [])
    }
}
