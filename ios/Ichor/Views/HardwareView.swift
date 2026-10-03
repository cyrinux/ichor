import SwiftUI
import IchorCore

/// "About this node": SMBIOS system, CPUs, memory, disks, system extensions and security
/// state (os:reader). A section that failed shows its error inline.
struct HardwareView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<NodeHardware> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { hw in
            List {
                systemSection(hw)
                processorsSection(hw)
                memorySection(hw)
                disksSection(hw)
                extensionsSection(hw)
                securitySection(hw)
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(String(localized: "About \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = model.seeded(state, from: .hardware(node: node))
        state = state.refreshed(with: await .from { try await model.fetch(.hardware(node: node), with: client) })
    }

    private func systemSection(_ hw: NodeHardware) -> some View {
        Section("System") {
            SectionError(message: hw.errors["system"])
            if let system = hw.system {
                InfoRow(label: String(localized: "Manufacturer"), value: system.manufacturer)
                InfoRow(label: String(localized: "Product"), value: system.product)
                InfoRow(label: String(localized: "Version"), value: system.version)
                InfoRow(label: String(localized: "Serial number"), value: system.serial, monospaced: true)
                InfoRow(label: String(localized: "SKU"), value: system.sku)
                InfoRow(label: String(localized: "UUID"), value: system.uuid, monospaced: true)
                InfoRow(label: String(localized: "BIOS version"), value: system.biosVersion)
            }
        }
    }

    private func processorsSection(_ hw: NodeHardware) -> some View {
        Section {
            SectionError(message: hw.errors["processors"])
            ForEach(hw.processors) { cpu in
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: cpu.model.isEmpty ? cpu.manufacturer : cpu.model)
                    Text(verbatim: cpuDetails(cpu)).font(.caption).foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Processors")
        } footer: {
            if hw.processors.count > 1 {
                Text("\(Int(hw.totalCores)) cores, \(Int(hw.totalThreads)) threads in total")
            }
        }
    }

    private func cpuDetails(_ cpu: ProcessorInfo) -> String {
        var parts = [cpu.socket, String(localized: "\(Int(cpu.cores)) cores, \(Int(cpu.threads)) threads")]
        if cpu.maxSpeedMhz > 0 { parts.append("\(cpu.maxSpeedMhz) MHz") }
        return parts.filter { !$0.isEmpty }.joined(separator: "  ·  ")
    }

    private func memorySection(_ hw: NodeHardware) -> some View {
        Section {
            SectionError(message: hw.errors["memory"])
            if !hw.memory.isEmpty {
                LabeledContent("Total", value: byteCount(Int64(clamping: hw.totalMemoryBytes), style: .memory))
            }
            ForEach(hw.memory) { module in
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        Text(verbatim: module.slot.isEmpty ? module.bank : module.slot)
                        Spacer()
                        Text(verbatim: byteCount(Int64(module.sizeMib) * 1_048_576, style: .memory)).monospacedDigit()
                    }
                    Text(verbatim: memoryDetails(module)).font(.caption).foregroundStyle(.secondary)
                }
            }
        } header: {
            Text("Memory")
        }
    }

    private func memoryDetails(_ module: MemoryModule) -> String {
        var parts = [module.manufacturer, module.type]
        if module.speed > 0 { parts.append("\(module.speed) MT/s") }
        return parts.filter { !$0.isEmpty }.joined(separator: "  ·  ")
    }

    private func disksSection(_ hw: NodeHardware) -> some View {
        Section("Disks") {
            SectionError(message: hw.errors["disks"])
            ForEach(sortedDisks(hw.disks)) { disk in
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        Text(verbatim: disk.devPath.isEmpty ? disk.name : disk.devPath).font(.body.monospaced())
                        if disk.systemDisk { StatusPill(label: String(localized: "System disk"), color: .blue) }
                        Spacer()
                        Text(verbatim: byteCount(Int64(clamping: disk.size), style: .file)).monospacedDigit()
                    }
                    Text(verbatim: diskDetails(disk)).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func diskDetails(_ disk: DiskInfo) -> String {
        var parts = [disk.type.uppercased(), disk.model, disk.serial]
        if disk.readonly { parts.append(String(localized: "read-only")) }
        return parts.filter { !$0.isEmpty }.joined(separator: "  ·  ")
    }

    private func extensionsSection(_ hw: NodeHardware) -> some View {
        Section("System extensions") {
            SectionError(message: hw.errors["extensions"])
            if hw.extensions.isEmpty && hw.errors["extensions"] == nil {
                Text("None").foregroundStyle(.secondary)
            }
            ForEach(hw.extensions) { ext in
                // Long versions (the schematic's 64-char hash) get their own line instead of squeezing the name.
                let inlineVersion = ext.version.count <= 16
                VStack(alignment: .leading, spacing: 3) {
                    HStack {
                        Text(verbatim: ext.name)
                        Spacer()
                        if inlineVersion {
                            Text(verbatim: ext.version).font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                    if !inlineVersion {
                        Text(verbatim: ext.version).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                    if !ext.description.isEmpty {
                        Text(verbatim: ext.description).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private func securitySection(_ hw: NodeHardware) -> some View {
        Section("Security") {
            SectionError(message: hw.errors["security"])
            if let security = hw.security {
                FlagRow(label: String(localized: "Secure Boot"), on: security.secureBoot)
                FlagRow(label: String(localized: "Booted with UKI"), on: security.bootedWithUki)
                FlagRow(label: String(localized: "Module signatures enforced"), on: security.moduleSignatureEnforced)
                InfoRow(label: String(localized: "SELinux"), value: security.selinuxState)
                InfoRow(label: String(localized: "FIPS"), value: security.fipsState)
                InfoRow(label: String(localized: "UKI signing key"), value: security.ukiSigningKeyFingerprint, monospaced: true)
                InfoRow(label: String(localized: "PCR signing key"), value: security.pcrSigningKeyFingerprint, monospaced: true)
            }
        }
    }

    private func byteCount(_ bytes: Int64, style: ByteCountFormatter.CountStyle) -> String {
        ByteCountFormatter.string(fromByteCount: bytes, countStyle: style)
    }
}

/// A label/value row, hidden when the value is empty; long values can be selected.
private struct InfoRow: View {
    let label: String
    let value: String
    var monospaced = false

    var body: some View {
        if !value.isEmpty {
            LabeledContent {
                Text(verbatim: value)
                    .font(monospaced ? .caption.monospaced() : .body)
                    .textSelection(.enabled)
                    .multilineTextAlignment(.trailing)
            } label: {
                Text(label)
            }
        }
    }
}

private struct FlagRow: View {
    let label: String
    let on: Bool

    var body: some View {
        LabeledContent(label) {
            Label(on ? String(localized: "Yes") : String(localized: "No"), systemImage: on ? "checkmark.shield.fill" : "xmark.shield")
                .foregroundStyle(on ? Color.green : Color.secondary)
        }
    }
}
