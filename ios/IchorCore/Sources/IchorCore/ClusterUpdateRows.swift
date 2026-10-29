import Foundation

// The Updates screen's rows, built from ClusterUpdates. Same rules as Android
// (updateSections in model/ClusterUpdates.kt).

/// A step to a newer version.
public enum UpdateStep: String, Hashable, Sendable {
    case patch, minor, major
}

/// One candidate chip: the step and its version.
public struct UpdateCandidate: Hashable, Sendable {
    public let step: UpdateStep
    public let version: String
}

/// What already bumps a row: shown as a badge, with no chips.
public enum UpdateAutomation: Hashable, Sendable {
    case fluxImage, argoImageUpdater, semverRange, bot

    public init?(automatedBy: String) {
        switch automatedBy {
        case "flux-image-automation": self = .fluxImage
        case "argocd-image-updater": self = .argoImageUpdater
        case "flux-semver": self = .semverRange
        case "bot": self = .bot
        default: return nil
        }
    }
}

/// One row of the Updates screen, an image or a chart: name (the image repository or the
/// chart's name), running, the candidates, the automation that bumps it, why it is not
/// comparable, and the item JSON sent back to UpdateEditTarget.
public struct UpdateRow: Hashable, Identifiable, Sendable {
    public let id: String
    public let chart: Bool
    public let name: String
    public let running: String
    public let candidates: [UpdateCandidate]
    public let automation: UpdateAutomation?
    public let reason: String
    public let owner: UpdateOwner?
    public let editable: Bool
    public let source: String
    /// The item as ClusterUpdates gave it, as JSON.
    public let item: String

    /// The newest candidate, for "running → newest".
    public var newest: String { candidates.last?.version ?? "" }
}

/// The screen's three sections.
public struct UpdateSections: Hashable, Sendable {
    public let images: [UpdateRow]
    public let charts: [UpdateRow]
    public let notComparable: [UpdateRow]

    /// The rows with a newer version (the GitOps card's count).
    public var outdated: Int { (images + charts).filter { !$0.candidates.isEmpty }.count }
}

private func candidates(patch: String, minor: String, major: String) -> [UpdateCandidate] {
    [(UpdateStep.patch, patch), (.minor, minor), (.major, major)]
        .filter { !$0.1.isEmpty }
        .map { UpdateCandidate(step: $0.0, version: $0.1) }
}

private func itemJSON<T: Encodable>(_ value: T) -> String {
    (try? TalosJSON.encode(value)) ?? "{}"
}

extension UpdateImage {
    var row: UpdateRow {
        let automation = UpdateAutomation(automatedBy: automatedBy)
        return UpdateRow(
            id: "image:\(repo):\(tag):\(digest)", chart: false, name: repo,
            running: tag.isEmpty ? String(digest.prefix(19)) : tag,
            // An automated image shows its badge, not chips: something else bumps it.
            candidates: automation == nil ? candidates(patch: patch, minor: minor, major: major) : [],
            automation: automation, reason: reason, owner: owner, editable: edit != nil && automation == nil,
            source: source, item: itemJSON(self))
    }
}

extension UpdateChart {
    var row: UpdateRow {
        let automation = UpdateAutomation(automatedBy: automatedBy)
        return UpdateRow(
            id: "chart:\(name):\(owner?.namespace ?? "")/\(owner?.name ?? "")", chart: true, name: name, running: version,
            candidates: automation == nil ? candidates(patch: patch, minor: minor, major: major) : [],
            automation: automation, reason: reason, owner: owner, editable: edit != nil && automation == nil,
            source: source.isEmpty ? repoURL : source, item: itemJSON(self))
    }
}

/// updates in sections: images and charts with a newer version or a badge, then the ones not
/// comparable (a reason, no candidate); up to date rows are left out. Go's order is kept.
public func updateSections(_ updates: ClusterUpdates) -> UpdateSections {
    let images = updates.images.map(\.row)
    let charts = updates.charts.map(\.row)
    let shown = { (r: UpdateRow) in !r.candidates.isEmpty || r.automation != nil }
    return UpdateSections(images: images.filter(shown), charts: charts.filter(shown),
                          notComparable: (images + charts).filter { !shown($0) && !$0.reason.isEmpty })
}

/// UpdateEditTarget's itemJSON: row's item with the version of the chip tapped.
public func updateItemJSON(_ row: UpdateRow, version: String) -> String {
    guard var object = (try? JSONSerialization.jsonObject(with: Data(row.item.utf8))) as? [String: Any] else { return "{}" }
    object["version"] = version
    guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys]) else { return "{}" }
    return String(decoding: data, as: UTF8.self)
}

/// The pull request's title (English: text of the repository): "Update podinfo to 6.7.1".
public func updateProposalTitle(_ row: UpdateRow, version: String) -> String {
    "Update \(row.name.split(separator: "/").last.map(String.init) ?? row.name) to \(version)"
}

/// The pull request's body: what changes and where the version comes from.
public func updateProposalBody(_ row: UpdateRow, version: String) -> String {
    var body = "Updates `\(row.name)` from `\(row.running)` to `\(version)`."
    if !row.source.isEmpty { body += "\n\nSource: \(row.source)" }
    return body + "\n\nProposed from the Updates screen of Ichor."
}

/// The quick edit target makes for row at version, titled and described for its pull request;
/// nil when it has none (a note says why).
public func updateQuickEdit(_ target: UpdateEditTarget, row: UpdateRow, version: String) -> QuickEdit? {
    guard let e = target.edit else { return nil }
    let title = updateProposalTitle(row, version: version)
    let body = updateProposalBody(row, version: version)
    let label = row.name.split(separator: "/").last.map(String.init) ?? row.name
    func nonEmpty(_ s: String) -> String? { s.isEmpty ? nil : s }
    switch (e.op, e.kustomizeImage.isEmpty) {
    case ("path", _):
        return QuickEdit(op: .path, documentIndex: e.documentIndex, kind: nonEmpty(e.kind), name: nonEmpty(e.name), label: label,
                         path: e.path, value: e.value, title: title, body: body)
    case (_, false):
        return QuickEdit(op: .image, documentIndex: e.documentIndex, image: e.image, kustomizeImage: e.kustomizeImage, label: label,
                         title: title, body: body)
    default:
        return QuickEdit(op: .image, documentIndex: e.documentIndex, kind: nonEmpty(e.kind), name: nonEmpty(e.name),
                         container: e.container, image: e.image, label: label, title: title, body: body)
    }
}
