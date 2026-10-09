import SwiftUI
import IchorCore

/// Before against after on one scale: the solid bar is the new value, the dashed outline the old
/// one, both as fractions of the larger. Reads the same way for requests and for costs.
struct CastAIBeforeAfterBar: View {
    let before: Double
    let after: Double
    var height: CGFloat = 6
    /// The value a full bar stands for; the larger of the two when nil.
    var scale: Double?

    var body: some View {
        let full = scale ?? max(before, after)
        GeometryReader { geo in
            let width = geo.size.width
            ZStack(alignment: .leading) {
                Capsule().fill(Color(.tertiarySystemFill)).frame(height: height)
                if full > 0 && after > 0 {
                    Capsule().fill(Color.accentColor)
                        .frame(width: max(fraction(after, of: full) * width, height), height: height)
                }
                if full > 0 && before > 0 {
                    Capsule()
                        .stroke(Color.secondary, style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                        .frame(width: max(fraction(before, of: full) * width - 2, height), height: height + 2)
                        .padding(.leading, 1)
                }
            }
            .frame(height: height + 4)
        }
        .frame(height: height + 4)
        .accessibilityHidden(true)
    }

    private func fraction(_ value: Double, of full: Double) -> CGFloat {
        CGFloat(min(max(value / full, 0), 1))
    }
}

/// "CPU  [bar]  500m → 120m": one resource's before and after on a row.
struct CastAIResourceRow: View {
    let label: String
    let before: Double
    let after: Double
    let text: String

    var body: some View {
        HStack(spacing: 8) {
            Text(verbatim: label).font(.caption2).foregroundStyle(.secondary).frame(width: 52, alignment: .leading)
            CastAIBeforeAfterBar(before: before, after: after)
            Text(verbatim: text).font(.caption2.monospaced()).lineLimit(1).frame(width: 132, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A headline number with its caption above and unit below.
struct CastAIMetricTile: View {
    let title: String
    let value: String
    let caption: String
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: title).font(.caption).foregroundStyle(.secondary)
            Text(verbatim: value).font(.title2.monospaced().weight(.medium)).foregroundStyle(color)
                .lineLimit(1).minimumScaleFactor(0.6)
            Text(verbatim: caption).font(.caption2).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// One part of a split bar: its count, colour and legend label.
struct CastAISplitPart: Identifiable {
    let count: Int
    let color: Color
    let label: String
    var id: String { label }
}

/// A stacked bar of parts by count, with a legend under it; empty parts are left out.
struct CastAISplitBar: View {
    let parts: [CastAISplitPart]

    var body: some View {
        let shown = parts.filter { $0.count > 0 }
        let total = shown.reduce(0) { $0 + $1.count }
        if total > 0 {
            VStack(alignment: .leading, spacing: 8) {
                GeometryReader { geo in
                    let gaps = CGFloat(shown.count - 1) * 2
                    HStack(spacing: 2) {
                        ForEach(shown) { part in
                            RoundedRectangle(cornerRadius: 2).fill(part.color)
                                .frame(width: max(0, (geo.size.width - gaps) * CGFloat(part.count) / CGFloat(total)))
                        }
                    }
                }
                .frame(height: 8)
                .accessibilityHidden(true)
                ChipFlow(spacing: 14) {
                    ForEach(shown) { part in
                        HStack(spacing: 6) {
                            RoundedRectangle(cornerRadius: 2).fill(part.color).frame(width: 8, height: 8).accessibilityHidden(true)
                            Text(verbatim: part.label).font(.caption)
                        }
                    }
                }
            }
        }
    }
}

/// An amber banner (something to keep an eye on, not broken) or a red one (the cause of a failure).
struct CastAIBanner: View {
    let text: String
    var bad = false

    var body: some View {
        let color: Color = bad ? .statusBad : .statusWarn
        HStack(alignment: .top, spacing: 8) {
            if bad { Image(systemName: "exclamationmark.triangle.fill").accessibilityHidden(true) }
            Text(verbatim: text).frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.callout)
        .foregroundStyle(color)
        .padding(12)
        .background(color.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
    }
}

/// A saving is good news, growth a caution, no change neutral.
func castAIDeltaColor(_ delta: Int64) -> Color {
    delta < 0 ? Color.statusOK : delta > 0 ? Color.statusWarn : Color.secondary
}

extension CastAIMode {
    /// nil when CAST AI does not say.
    var label: String? {
        switch self {
        case .immediate: return String(localized: "applied immediately")
        case .deferred: return String(localized: "applied on restart")
        case .unknown: return nil
        }
    }
}

extension CastAIReason {
    var label: String {
        switch self {
        case .vpa: String(localized: "not applied")
        case .hpa: String(localized: "HPA not synced")
        case .readOnly: String(localized: "read-only")
        }
    }
}

extension CastAIPlanState {
    var label: String {
        switch self {
        case .awaitingApproval: String(localized: "Awaiting approval")
        case .pending: String(localized: "Pending")
        case .running: String(localized: "Running")
        case .done: String(localized: "castai.plan.done", defaultValue: "Done")
        case .failed: String(localized: "Failed")
        case .skipped: String(localized: "Skipped")
        }
    }

    var color: Color {
        switch self {
        case .done: .statusOK
        case .failed: Color.statusBad
        case .running, .pending: .accentColor
        case .awaitingApproval: .statusWarn
        case .skipped: .secondary
        }
    }
}

extension CastAIPlanMode {
    /// nil for a mode this version does not know.
    var label: String? {
        switch self {
        case .full: return String(localized: "castai.mode.full", defaultValue: "full")
        case .deleteEmpty: return String(localized: "empty nodes")
        case .drainOnly: return String(localized: "drain only")
        case .other: return nil
        }
    }
}

extension CastAINodeStatus {
    var label: String {
        switch self {
        case .success: String(localized: "done")
        case .failed: String(localized: "failed")
        case .blocked: String(localized: "blocked")
        case .inProgress: String(localized: "in progress")
        case .pending: String(localized: "not started")
        }
    }

    var color: Color {
        switch self {
        case .success: .statusOK
        case .failed: Color.statusBad
        case .blocked: .statusWarn
        case .inProgress: .accentColor
        case .pending: .secondary
        }
    }
}

extension CastAIEventTone {
    var color: Color {
        switch self {
        case .ok: .statusOK
        case .bad: .statusBad
        case .warn: .statusWarn
        case .moving: .accentColor
        case .neutral: .secondary
        }
    }
}

extension CastAIPlan {
    /// "Timeout · while removing nodes", CAST AI's message when it gives no reason nor phase.
    var failureText: String {
        let phase: String = switch failurePhase {
        case "Deletion": String(localized: "while removing nodes")
        case "Creation": String(localized: "while adding nodes")
        default: failurePhase
        }
        let text = [failureReason, phase].filter { !$0.isEmpty }.joined(separator: " · ")
        return text.isEmpty ? message : text
    }

    /// "3 of 4 nodes removed · 1 of 1 added".
    var nodesText: String {
        [removing.isEmpty ? nil : String(localized: "\(String(removed)) of \(String(removing.count)) nodes removed"),
         adding.isEmpty ? nil : String(localized: "\(String(added)) of \(String(adding.count)) added")]
            .compactMap { $0 }.joined(separator: " · ")
    }

    /// "−$53/mo" saved, "$141 planned", or "$98 missed" for a failed plan (the nodes it left).
    var savingText: String {
        switch planState {
        case .done:
            let saved = savedMonthly ?? 0
            // A plan that ended up costing more is said so, not shown as a negative saving.
            return saved < 0 ? String(localized: "+\(formatMoney(-saved, currency: currency))/mo")
                : String(localized: "−\(formatMoney(saved, currency: currency))/mo")
        case .failed:
            return String(localized: "\(castAIApprox(formatMoney(missedMonthly, currency: currency), estimated: missedEstimated)) missed")
        default:
            return String(localized: "\(formatMoney(plannedMonthly, currency: currency)) planned")
        }
    }

    var savingColor: Color {
        switch planState {
        case .done: (savedMonthly ?? 0) < 0 ? Color.statusBad : Color.statusOK
        case .failed: Color.statusBad
        default: Color.secondary
        }
    }
}

/// "18:29": a time of day, as Android's short time format.
func castAITime(_ millis: Int64) -> String {
    Date(epochMillis: millis).formatted(date: .omitted, time: .shortened)
}

/// "1m 35s": a duration in the user's language.
func castAIDuration(_ seconds: Int64) -> String {
    Duration.seconds(seconds).formatted(.units(allowed: [.days, .hours, .minutes, .seconds], width: .narrow, maximumUnitCount: 2))
}
