import SwiftUI
import TalosdevMobileCore
import UniformTypeIdentifiers

extension UTType {
    /// libpcap capture file (Wireshark, tcpdump).
    static var pcap: UTType { UTType(filenameExtension: "pcap", conformingTo: .data) ?? .data }
}

/// A capture file handed to fileExporter without loading it in memory.
struct PcapDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.pcap] }
    let url: URL

    init(url: URL) { self.url = url }

    init(configuration: ReadConfiguration) throws {
        throw CocoaError(.featureUnsupported) // export only
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        try FileWrapper(url: url, options: .immediate)
    }
}

/// Capture files kept on the phone: open, share, delete; total size.
struct CapturesListView: View {
    @State private var files: LoadState<[CaptureFile]> = .loading
    @State private var directory: URL?
    @State private var confirmDeleteAll = false

    var body: some View {
        LoadStateView(state: files, retry: load) { list in
            List {
                Section {
                    ForEach(list) { file in
                        if let url = directory?.appendingPathComponent(file.name) {
                            NavigationLink {
                                CaptureFileView(url: url)
                            } label: {
                                CaptureFileRow(file: file)
                            }
                            .swipeActions {
                                Button(role: .destructive) { delete(url) } label: { Label("Delete", systemImage: "trash") }
                                ShareLink(item: url) { Label("Share", systemImage: "square.and.arrow.up") }
                            }
                        }
                    }
                } header: {
                    if !list.isEmpty { Text("\(list.count) captures") + Text(verbatim: " · \(formatBytes(totalCaptureSize(list)))") }
                } footer: {
                    Text("Captures can contain sensitive traffic (tokens, internal names). They are kept on this phone only, out of backups: delete them when done.")
                }
            }
            .overlay {
                if list.isEmpty {
                    ContentUnavailableView("No captures", systemImage: "antenna.radiowaves.left.and.right",
                                           description: Text("Captures from a node's Capture packets menu appear here."))
                }
            }
            .toolbar {
                if !list.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Button("Delete all", role: .destructive) { confirmDeleteAll = true }
                    }
                }
            }
            .confirmationDialog(Text("Delete all captures?"), isPresented: $confirmDeleteAll, titleVisibility: .visible) {
                Button("Delete all", role: .destructive) {
                    for file in list { if let url = directory?.appendingPathComponent(file.name) { try? CaptureStore.delete(url) } }
                    Task { await load() }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle("Saved captures")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        files = await .from {
            directory = try CaptureStore.directory()
            return try CaptureStore.list()
        }
    }

    private func delete(_ url: URL) {
        try? CaptureStore.delete(url)
        Task { await load() }
    }
}

private struct CaptureFileRow: View {
    let file: CaptureFile

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: file.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
            Text(verbatim: "\(file.modified.formatted(date: .abbreviated, time: .shortened)) · \(formatBytes(file.size))")
                .font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// A saved capture, 500 packets per page.
struct CaptureFileView: View {
    let url: URL

    @Environment(\.dismiss) private var dismiss
    @State private var page: LoadState<PcapPage> = .loading
    @State private var offset = 0
    @State private var selected: PacketSummary?

    var body: some View {
        LoadStateView(state: page, retry: load) { page in
            List {
                Section {
                    CaptureFileActions(url: url, onDelete: { dismiss() })
                } footer: {
                    Text("Captures can contain sensitive traffic: share them with care.")
                }
                Section {
                    ForEach(page.packets) { packet in
                        Button { selected = packet } label: { PacketRow(packet: packet) }
                            .buttonStyle(.plain)
                    }
                } header: {
                    pager(total: page.total)
                }
            }
            .listStyle(.plain)
        }
        .navigationTitle(url.lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: offset) { await load() }
        .sheet(item: $selected) { packet in
            NavigationStack { PacketDetailView(path: url.path, packet: packet) }
        }
    }

    private func pager(total: Int) -> some View {
        HStack {
            Button { offset = max(offset - pcapPageSize, 0) } label: { Image(systemName: "chevron.left") }
                .disabled(offset == 0)
                .accessibilityLabel(Text("Previous page"))
            Spacer()
            Text("Packets \(total == 0 ? 0 : offset + 1)–\(min(offset + pcapPageSize, total)) of \(total)")
                .monospacedDigit()
            Spacer()
            Button { offset += pcapPageSize } label: { Image(systemName: "chevron.right") }
                .disabled(offset + pcapPageSize >= total)
                .accessibilityLabel(Text("Next page"))
        }
        .buttonStyle(.borderless)
    }

    private func load() async {
        page = await .from { try await TalosClient.readPcap(path: url.path, offset: offset) }
    }
}

/// One packet's decoded layers and hex dump.
struct PacketDetailView: View {
    let path: String
    let packet: PacketSummary

    @Environment(\.dismiss) private var dismiss
    @State private var detail: LoadState<PacketDetail> = .loading

    var body: some View {
        LoadStateView(state: detail, retry: load) { detail in
            List {
                ForEach(detail.layers.indices, id: \.self) { index in
                    let layer = detail.layers[index]
                    DisclosureGroup {
                        ForEach(layer.fields.indices, id: \.self) { i in
                            LabeledContent {
                                Text(verbatim: layer.fields[i].v).font(.caption.monospaced()).textSelection(.enabled)
                            } label: {
                                Text(verbatim: layer.fields[i].k).font(.caption)
                            }
                        }
                    } label: {
                        Text(verbatim: layer.name).font(.headline)
                    }
                }
                if !detail.hex.isEmpty {
                    Section("Bytes") {
                        ScrollView(.horizontal) {
                            Text(verbatim: detail.hex)
                                .font(.caption2.monospaced())
                                .textSelection(.enabled)
                                .fixedSize()
                                .padding(.vertical, 4)
                        }
                    }
                }
            }
        }
        .navigationTitle(String(localized: "Packet \(packet.number)"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
        }
        .task { await load() }
    }

    private func load() async {
        detail = await .from { try await TalosClient.packetDetail(path: path, index: packet.n) }
    }
}
