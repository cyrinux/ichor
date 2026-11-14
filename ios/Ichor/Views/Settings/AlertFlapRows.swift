import SwiftUI
import IchorCore

/// The default rule's "Fold flapping alerts into the digest": a switch, then the opens in 24 h it
/// starts from (2 to 10), as Android's FlapRule.
struct FlapRuleRows: View {
    let rule: AlertRule
    let onChange: (Int?) -> Void

    var body: some View {
        let opens = rule.flapOpens.flatMap { $0 > 0 ? $0 : nil }
        RuleToggle(
            title: String(localized: "Fold flapping alerts into the digest"),
            hint: String(
                localized:
                    "A warning that fires and recovers again and again in a day goes into the digest, marked as flapping. Critical alerts still notify every time."
            ),
            isOn: opens != nil
        ) { on in
            onChange(on ? alertFlapDefault : nil)
        }
        if let opens { FlapStepper(opens: opens, onChange: onChange) }
    }
}

/// A cluster's override: as the default, off, or on from its own opens in 24 h.
struct ClusterFlapRows: View {
    let rule: AlertRule
    let asDefault: String
    let off: String
    let onChange: (Int?) -> Void

    var body: some View {
        let opens = rule.flapOpens
        RuleSegments(
            title: String(localized: "Fold flapping alerts into the digest"),
            labels: [asDefault, off, String(localized: "alertRules.on", defaultValue: "On")],
            selected: opens.map { $0 > 0 ? 2 : 1 } ?? 0
        ) { index in
            switch index {
            case 0: onChange(nil)
            case 1: onChange(0)
            default: onChange(opens.flatMap { $0 > 0 ? $0 : nil } ?? alertFlapDefault)
            }
        }
        if let opens, opens > 0 { FlapStepper(opens: opens, onChange: onChange) }
    }
}

private struct FlapStepper: View {
    let opens: Int
    let onChange: (Int?) -> Void

    private var clamped: Int { min(max(opens, alertFlapRange.lowerBound), alertFlapRange.upperBound) }

    var body: some View {
        Stepper(value: Binding(get: { clamped }, set: { onChange($0) }), in: alertFlapRange) {
            Text("From \(opens) opens in 24 hours")
        }
    }
}
