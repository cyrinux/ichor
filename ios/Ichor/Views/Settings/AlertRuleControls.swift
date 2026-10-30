import SwiftUI
import IchorCore

// The controls the alert rules share between their default (AlertRulesSection) and a cluster's
// overrides (AlertRulesClusterSheet), as Android's ui/settings/AlertRuleControls.kt.

/// "All", "Warnings" or "Critical" (keys of their own: the plain words are worded for other
/// screens in some languages).
func alertSeverityLabel(_ severity: String) -> String {
    switch severity {
    case alertSeverityWarning: String(localized: "alertRules.warnings", defaultValue: "Warnings")
    case alertSeverityCritical: String(localized: "alertRules.critical", defaultValue: "Critical")
    default: String(localized: "alertRules.all", defaultValue: "All")
    }
}

/// "Off" for 0, else "15 min": a repeat choice.
func alertRepeatLabel(_ minutes: Int) -> String {
    minutes <= 0 ? String(localized: "Off") : String(localized: "\(minutes) min")
}

/// "Phone's (Europe/Paris)" for "", else the zone itself.
func alertZoneLabel(_ zone: String) -> String {
    let phone = TimeZone.current.identifier
    return zone.isEmpty ? String(localized: "Phone's (\(phone))") : zone
}

/// A segmented choice among `labels` (already localized), `selected` its index, under a
/// `title` that also labels it for VoiceOver.
struct RuleSegments: View {
    let title: String
    let labels: [String]
    let selected: Int
    let onSelect: (Int) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: title)
            Picker(selection: Binding(get: { selected }, set: { onSelect($0) })) {
                ForEach(labels.indices, id: \.self) { index in
                    Text(verbatim: labels[index]).tag(index)
                }
            } label: {
                Text(verbatim: title)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .accessibilityLabel(Text(verbatim: title))
        }
    }
}

/// A title, its hint and a switch on one row.
struct RuleToggle: View {
    let title: String
    let hint: String
    let isOn: Bool
    let onChange: (Bool) -> Void

    var body: some View {
        Toggle(isOn: Binding(get: { isOn }, set: { onChange($0) })) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title)
                Text(verbatim: hint).font(.caption).foregroundStyle(.secondary)
            }
        }
    }
}

/// A quiet-hours window: its start and end (a time picker each), its zone ("" the phone's, a
/// list to pick another from) and "except critical".
struct QuietHoursRows: View {
    let quiet: QuietHours
    let onChange: (QuietHours) -> Void
    @State private var pickingZone = false

    var body: some View {
        DatePicker(
            String(localized: "Quiet hours start"), selection: clock(\.start), displayedComponents: .hourAndMinute)
        DatePicker(String(localized: "Quiet hours end"), selection: clock(\.end), displayedComponents: .hourAndMinute)
        Button {
            pickingZone = true
        } label: {
            LabeledContent {
                Text(verbatim: alertZoneLabel(quiet.tz)).lineLimit(1)
            } label: {
                Text("Time zone")
            }
        }
        .foregroundStyle(.primary)
        .sheet(isPresented: $pickingZone) {
            TimeZonePickerSheet(current: quiet.tz) { zone in
                var next = quiet
                next.tz = zone
                onChange(next)
            }
        }
        RuleToggle(
            title: String(localized: "Except critical"),
            hint: String(
                localized: "A node down, an etcd alarm, an unreachable cluster and other critical alerts still notify"
            ),
            isOn: quiet.exceptCritical
        ) { on in
            var next = quiet
            next.exceptCritical = on
            onChange(next)
        }
    }

    /// The "HH:MM" at `field` as a date today (the picker shows its hour and minute only).
    private func clock(_ field: WritableKeyPath<QuietHours, String>) -> Binding<Date> {
        Binding(
            get: {
                let (hour, minute) = quietClock(quiet[keyPath: field])
                return Calendar.current.date(bySettingHour: hour, minute: minute, second: 0, of: Date()) ?? Date()
            },
            set: { date in
                let parts = Calendar.current.dateComponents([.hour, .minute], from: date)
                var next = quiet
                next[keyPath: field] = quietClockText(hour: parts.hour ?? 0, minute: parts.minute ?? 0)
                onChange(next)
            })
    }
}

/// The phone's zone first ("", followed when the phone moves), then every region zone,
/// filtered by what is typed.
struct TimeZonePickerSheet: View {
    let current: String
    let onPick: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    private static let zones = TimeZone.knownTimeZoneIdentifiers
        .filter { $0.contains("/") && !$0.hasPrefix("Etc/") }
        .sorted()

    private var shown: [String] {
        let text = query.trimmingCharacters(in: .whitespaces)
        let matching = text.isEmpty ? Self.zones : Self.zones.filter { $0.localizedCaseInsensitiveContains(text) }
        return [""] + matching
    }

    var body: some View {
        NavigationStack {
            List(shown, id: \.self) { zone in
                Button {
                    onPick(zone)
                    dismiss()
                } label: {
                    HStack {
                        Text(verbatim: alertZoneLabel(zone)).foregroundStyle(.primary)
                        Spacer()
                        if zone == current { Image(systemName: "checkmark").foregroundStyle(.tint) }
                    }
                }
                .accessibilityAddTraits(zone == current ? .isSelected : [])
            }
            .searchable(text: $query)
            .navigationTitle(Text("Time zone"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }
}
