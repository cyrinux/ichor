import SwiftUI
import IchorCore

/// The Hardware screen's Sensors section: temperatures with a bar to their limit, fans,
/// per-core CPU frequency with the governor and the throttle counters; voltages and thermal
/// zones behind a disclosure. A "throttled" pill in the header when the node is held back.
/// Hidden by the caller on a VM (nothing to show, no error).
struct SensorsSection: View {
    let sensors: NodeSensors

    @State private var more = false

    var body: some View {
        Section {
            if sensors.throttled {
                Text("Cores run well below their top speed under the performance governor, or a sensor is above its max.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            SectionError(message: sensors.sensorsError)
            // By position: two chips of one driver (two NVMe drives) share chip and label.
            ForEach(Array(sensors.temperatures.enumerated()), id: \.offset) { TemperatureRow(sensor: $0.element) }
            ForEach(Array(sensors.fans.enumerated()), id: \.offset) { _, fan in
                LabeledContent {
                    Text("\(fan.rpm) RPM").monospacedDigit()
                } label: {
                    Text(verbatim: sensorName(fan.chip, fan.label))
                }
            }
            if !sensors.cpuFreq.isEmpty { frequencies }
            if let throttle = sensors.throttle {
                LabeledContent("Thermal throttle events") {
                    Text("\(throttle.coreEvents) core · \(throttle.packageEvents) package").monospacedDigit()
                }
            }
            if !sensors.voltages.isEmpty || !sensors.thermalZones.isEmpty {
                DisclosureGroup("Voltages and thermal zones", isExpanded: $more) {
                    ForEach(Array(sensors.voltages.enumerated()), id: \.offset) { _, v in
                        LabeledContent {
                            Text(verbatim: String(format: "%.3f V", v.volts)).monospacedDigit()
                        } label: {
                            Text(verbatim: sensorName(v.chip, v.label))
                        }
                    }
                    ForEach(Array(sensors.thermalZones.enumerated()), id: \.offset) { _, zone in
                        LabeledContent {
                            Text(verbatim: celsius(zone.celsius)).monospacedDigit()
                        } label: {
                            Text(verbatim: zone.type)
                        }
                    }
                }
            }
        } header: {
            HStack {
                Text("Sensors")
                Spacer()
                if sensors.throttled { StatusPill(label: String(localized: "Throttled"), color: .statusBad) }
            }
        }
    }

    private var frequencies: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("CPU frequency")
            let first = sensors.cpuFreq[0]
            let governors = Array(Set(sensors.cpuFreq.map(\.governor).filter { !$0.isEmpty })).sorted()
            Text(verbatim: (governors + ["\(first.minMhz)–\(first.maxMhz) MHz"]).joined(separator: "  ·  "))
                .font(.caption)
                .foregroundStyle(.secondary)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 110), alignment: .leading)], alignment: .leading, spacing: 2) {
                ForEach(sensors.cpuFreq) { f in
                    Text(verbatim: "cpu\(f.cpu) \(f.currentMhz) MHz").font(.caption.monospaced())
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// One temperature: chip and label, the value, a bar to its max (else crit) and the limits.
private struct TemperatureRow: View {
    let sensor: TemperatureSensor

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: sensorName(sensor.chip, sensor.label))
                Spacer()
                Text(verbatim: celsius(sensor.celsius))
                    .monospacedDigit()
                    .bold()
                    .foregroundStyle(sensor.hot ? Color.statusBad : Color.primary)
            }
            if let fraction = sensor.limitFraction {
                ProgressView(value: fraction).tint(color(fraction))
            }
            if !limits.isEmpty {
                Text(verbatim: limits.joined(separator: "  ·  ")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var limits: [String] {
        var out: [String] = []
        if sensor.maxCelsius > 0 { out.append(String(localized: "max \(oneDecimal(sensor.maxCelsius)) °C")) }
        if sensor.critCelsius > 0 { out.append(String(localized: "critical \(oneDecimal(sensor.critCelsius)) °C")) }
        return out
    }

    private func color(_ fraction: Double) -> Color {
        if sensor.hot { return .statusBad }
        return fraction >= 0.85 ? .statusWarn : .statusOK
    }
}

/// The node's PCI devices: product, vendor, class and the driver bound to it.
struct PCISection: View {
    let sensors: NodeSensors

    var body: some View {
        Section("PCI devices") {
            SectionError(message: sensors.errors["pci"])
            ForEach(sensors.pci) { device in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: device.product.isEmpty ? (device.subclass.isEmpty ? device.id : device.subclass) : device.product)
                    Text(verbatim: [device.vendor, device.subclass.isEmpty ? device.class : device.subclass]
                        .filter { !$0.isEmpty }.joined(separator: "  ·  "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(verbatim: [device.id, device.driver].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

private func sensorName(_ chip: String, _ label: String) -> String {
    chip.isEmpty ? label : "\(label) (\(chip))"
}

private func oneDecimal(_ value: Double) -> String {
    String(format: "%.1f", value)
}

private func celsius(_ value: Double) -> String {
    "\(oneDecimal(value)) °C"
}
