import SwiftUI
import IchorCore

// How Flux states look everywhere in the app: glyphs, colors and labels, what each action
// does in words, and the small pieces the Flux screens share. The bar and badges are Argo
// CD's (ArgoStyle.swift).

extension FluxState {
    var label: String {
        switch self {
        case .ready: String(localized: "Ready")
        case .reconciling: String(localized: "Reconciling")
        case .suspended: String(localized: "Suspended")
        case .failing: String(localized: "Failing")
        }
    }

    var color: Color {
        switch self {
        case .ready: .green
        case .reconciling: .blue
        case .suspended: .secondary
        case .failing: .red
        }
    }

    var symbol: String {
        switch self {
        case .ready: "checkmark.circle.fill"
        case .reconciling: "arrow.triangle.2.circlepath"
        case .suspended: "pause.circle.fill"
        case .failing: "xmark.octagon.fill"
        }
    }
}

extension FluxFilter {
    var label: String {
        switch self {
        case .all: String(localized: "All")
        case .failing: String(localized: "Failing")
        case .reconciling: String(localized: "Reconciling")
        case .suspended: String(localized: "Suspended")
        // Kubernetes kinds: never translated.
        case .kustomizations: "Kustomizations"
        case .helmReleases: "HelmReleases"
        }
    }

    var dot: Color? {
        switch self {
        case .all, .kustomizations, .helmReleases: nil
        case .failing: .red
        case .reconciling: .blue
        case .suspended: .secondary
        }
    }
}

/// The SF Symbol of a Flux kind.
func fluxKindSymbol(_ kind: String) -> String {
    switch kind {
    case "Kustomization": "square.stack.3d.up"
    case "HelmRelease": "shippingbox"
    case "GitRepository": "arrow.triangle.branch"
    case "OCIRepository": "cube"
    case "HelmRepository": "books.vertical"
    case "Bucket": "externaldrive"
    default: "questionmark.square"
    }
}

extension FluxApp {
    /// "Applied by Kustomization flux-system/apps: …", nil when nothing applies it from Git.
    var ownerNotice: String? {
        owner.map { String(localized: "Applied from Git by \($0.label): if its manifest sets suspend, Flux puts that back on the next reconcile.") }
    }
}

extension FluxAction {
    /// The confirmation's title.
    func title(for name: String) -> String {
        switch self {
        case .reconcile: String(localized: "Reconcile \(name)?")
        case .reconcileWithSource: String(localized: "Reconcile \(name) with its source?")
        case .suspend: String(localized: "Suspend \(name)?")
        case .resume: String(localized: "Resume \(name)?")
        case .force: String(localized: "Force an upgrade of \(name)?")
        case .reset: String(localized: "Reset the failures of \(name)?")
        }
    }

    /// The confirmation's button.
    var label: String {
        switch self {
        case .reconcile: String(localized: "Reconcile")
        case .reconcileWithSource: String(localized: "Reconcile with source")
        case .suspend: String(localized: "Suspend")
        case .resume: String(localized: "Resume")
        case .force: String(localized: "Force upgrade")
        case .reset: String(localized: "Reset failures")
        }
    }

    var symbol: String {
        switch self {
        case .reconcile: "arrow.clockwise"
        case .reconcileWithSource: "arrow.clockwise.circle"
        case .suspend: "pause.circle"
        case .resume: "play.circle"
        case .force: "bolt.circle"
        case .reset: "arrow.uturn.backward.circle"
        }
    }

    /// What the action does to name, in words.
    func explanation(for name: String) -> String {
        switch self {
        case .reconcile:
            String(localized: "Flux applies \(name) now from the revision its source already fetched, instead of waiting for its interval.")
        case .reconcileWithSource:
            String(localized: "Flux fetches the source first (a new commit or chart), then applies \(name) from it.")
        case .suspend:
            String(localized: "Flux stops reconciling \(name): changes in Git are not applied and drift is not corrected until you resume it.")
        case .resume:
            String(localized: "Flux reconciles \(name) again at once, then at every interval.")
        case .force:
            String(localized: "The helm-controller runs a Helm upgrade of \(name) now, even with nothing changed: its pods may restart.")
        case .reset:
            String(localized: "Flux forgets the failed attempts of \(name) and retries the install or upgrade from scratch, with its retries counted again.")
        }
    }
}

/// The ready glyph of a Flux object, colored, pulsing while it reconciles.
struct FluxGlyph: View {
    let state: FluxState
    var font: Font = .subheadline

    var body: some View {
        Image(systemName: state.symbol)
            .foregroundStyle(state.color)
            .symbolEffect(.pulse, isActive: state == .reconciling)
            .font(font)
            .accessibilityLabel(Text(verbatim: state.label))
    }
}

/// The apps' states as one bar, proportional to their counts.
struct FluxStateBar: View {
    let counts: [(state: FluxState, count: Int)]
    var height: CGFloat = 8

    var body: some View {
        SegmentBar(segments: counts.map { (color: $0.state.color, count: $0.count, label: $0.state.label) }, height: height)
    }
}

/// "⧉ apps": the Kustomization that applies an object from Git.
struct FluxOwnerBadge: View {
    let owner: FluxRef

    var body: some View {
        Label { Text(verbatim: owner.name).lineLimit(1) } icon: { Image(systemName: fluxKindSymbol(owner.kind)) }
            .labelStyle(.titleAndIcon)
            .font(.caption2.weight(.medium))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .foregroundStyle(.secondary)
            .background(Color(.tertiarySystemFill), in: Capsule())
            .accessibilityLabel(Text("Applied by \(owner.label)"))
    }
}
