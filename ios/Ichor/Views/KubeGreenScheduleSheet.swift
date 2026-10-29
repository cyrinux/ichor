import SwiftUI
import IchorCore

/// A SleepInfo's schedule, pre-filled: days (as text when they use cron steps), sleep and
/// wake-up times as "HH:mm" (cron syntax kept), an explicit "No wake-up", and the zone the times
/// are in, always written so a time is never saved without it. Save stays off until something
/// changed and every value is valid; the Argo CD self-heal warning shows when an app would
/// revert it.
struct KubeGreenScheduleSheet: View {
    let info: KubeGreenSleepInfo
    let onSaved: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var days: Set<Int>
    @State private var weekdaysText: String
    @State private var sleepAt: String
    @State private var noWakeUp: Bool
    @State private var wakeUpAt: String
    @State private var zone: String
    @State private var saving = false
    @State private var error: String?

    /// The days read as toggles; nil when they use cron steps and are edited as text.
    private let initialDays: Set<Int>?
    private let phoneZone = TimeZone.current.identifier

    // Explicit: the private @State makes the memberwise init private.
    init(info: KubeGreenSleepInfo, onSaved: @escaping () async -> Void) {
        self.info = info
        self.onSaved = onSaved
        let parsed = KubeGreenSchedule.days(info.weekdays)
        initialDays = parsed
        _days = State(initialValue: parsed ?? [])
        _weekdaysText = State(initialValue: info.weekdays)
        _sleepAt = State(initialValue: info.sleepAt)
        _noWakeUp = State(initialValue: info.wakeUpAt.isEmpty)
        _wakeUpAt = State(initialValue: info.wakeUpAt.isEmpty ? "08:00" : info.wakeUpAt)
        // An empty zone is UTC: say so rather than show the phone's zone over UTC times.
        _zone = State(initialValue: info.timeZone.isEmpty ? "UTC" : info.timeZone)
    }

    /// The days as written stay as written while the same days are picked ("0-6" is not
    /// rewritten "*"): nothing to save then, and no change Git would see as drift.
    private var weekdays: String {
        guard let initialDays else { return weekdaysText.trimmingCharacters(in: .whitespaces) }
        return days == initialDays ? info.weekdays : KubeGreenSchedule.weekdays(days)
    }
    private var trimmedSleep: String { sleepAt.trimmingCharacters(in: .whitespaces) }
    private var trimmedWake: String { noWakeUp ? "" : wakeUpAt.trimmingCharacters(in: .whitespaces) }
    private var trimmedZone: String { zone.trimmingCharacters(in: .whitespaces) }
    private var daysValid: Bool { initialDays != nil ? !days.isEmpty : KubeGreenSchedule.weekdaysValid(weekdays) }
    private var sleepValid: Bool { KubeGreenSchedule.timeValid(trimmedSleep) }
    private var wakeValid: Bool { noWakeUp || KubeGreenSchedule.timeValid(trimmedWake) }
    private var zoneValid: Bool { KubeGreenSchedule.zoneValid(trimmedZone) }
    private var changed: Bool {
        weekdays != info.weekdays || trimmedSleep != info.sleepAt || trimmedWake != info.wakeUpAt
            || trimmedZone != (info.timeZone.isEmpty ? "UTC" : info.timeZone)
    }
    private var canSave: Bool { daysValid && sleepValid && wakeValid && zoneValid && changed && !saving }

    private var healer: ArgoSelfHealer? {
        ArgoCDStore.shared.selfHealer(kind: "SleepInfo", namespace: info.namespace, name: info.name, key: model.argoKey)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Weekdays") {
                    if initialDays != nil {
                        // Monday first, as the week reads; cron numbers Sunday 0.
                        ForEach([1, 2, 3, 4, 5, 6, 0], id: \.self) { d in
                            Toggle(isOn: dayBinding(d)) { Text(verbatim: Calendar.current.weekdaySymbols[d]) }
                        }
                        if !daysValid { invalid(Text("Pick at least one day")) }
                    } else {
                        TextField(text: $weekdaysText, prompt: Text(verbatim: "1-5")) {
                            Text("Weekdays, as cron day-of-week (1-5, *, 0,6)")
                        }
                        .font(.body.monospaced())
                    }
                }
                Section {
                    timeField(Text("Sleeps at"), $sleepAt, valid: sleepValid)
                    Toggle("No wake-up: it sleeps until changed by hand", isOn: $noWakeUp)
                    if !noWakeUp { timeField(Text("Wakes up at"), $wakeUpAt, valid: wakeValid) }
                }
                Section("Time zone") {
                    TextField(text: $zone, prompt: Text(verbatim: "UTC")) { Text("Time zone") }
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if !zoneValid { invalid(Text("Unknown time zone")) }
                    if trimmedZone != phoneZone {
                        Button(String(localized: "Use this phone’s zone (\(phoneZone))")) { zone = phoneZone }
                    }
                }
                if let healer {
                    Section {
                        Text(
                            "\(healer.app.name) deploys this with self-heal on: within minutes it puts it back as Git has it. Freeze \(healer.app.name) first to keep your change for a while."
                        )
                        .foregroundStyle(Color.statusWarn)
                    }
                }
                if let error {
                    Section { ErrorOrNoticeText(message: error) }
                }
            }
            .themedBackground()
            .navigationTitle(String(localized: "Schedule of \(info.label)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }.disabled(!canSave)
                }
            }
        }
    }

    private func dayBinding(_ d: Int) -> Binding<Bool> {
        Binding(get: { days.contains(d) }, set: { on in if on { days.insert(d) } else { days.remove(d) } })
    }

    private func timeField(_ label: Text, _ value: Binding<String>, valid: Bool) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            LabeledContent {
                TextField(text: value, prompt: Text(verbatim: "20:00")) { label }
                    .multilineTextAlignment(.trailing)
                    .font(.body.monospaced())
                    .keyboardType(.numbersAndPunctuation)
            } label: {
                label
            }
            if !valid { invalid(Text("An hour and a minute, such as 20:00")) }
        }
    }

    private func invalid(_ text: Text) -> some View {
        text.font(.footnote).foregroundStyle(Color.statusBad)
    }

    private func save() async {
        guard let client = model.client else { return }
        saving = true
        error = nil
        defer { saving = false }
        do {
            try await client.setKubeGreenSchedule(
                namespace: info.namespace, name: info.name, weekdays: weekdays,
                sleepAt: trimmedSleep, wakeUpAt: trimmedWake, timeZone: trimmedZone)
            await onSaved()
            dismiss()
        } catch {
            self.error = KubeGreenText.saveFailed(info.label, error.localizedDescription)
        }
    }
}

/// The schedule editor's texts with arguments, passed as Strings so each key is the `%@` one.
enum KubeGreenText {
    static func saveFailed(_ a: String, _ b: String) -> String {
        String(localized: "Could not save the schedule of \(a): \(b)")
    }
}
