import SwiftUI
import IchorCore

/// `talosctl pcap` on one node (os:operator or os:admin): options, then the live packet list,
/// then the saved file to share, save or open.
struct CaptureView: View {
    let node: String
    let hostname: String

    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var session = CaptureSession()
    @State private var options = CaptureOptions()
    @State private var links: LoadState<[NetLink]> = .loading
    @State private var showVirtual = false
    /// The filter ValidateCaptureFilter last answered for, and its answer.
    @State private var validatedFilter: String?
    @State private var filterError = ""
    @State private var confirmLeave = false

    var body: some View {
        Group {
            switch session.state {
            case .setup:
                setupForm
            case .running, .finished:
                CaptureLiveView(session: session, newCapture: { session.reset() })
            }
        }
        .navigationTitle(String(localized: "Capture · \(hostname)"))
        .navigationBarTitleDisplayMode(.inline)
        // While capturing, leaving asks first (the back swipe is disabled with the button).
        .navigationBarBackButtonHidden(session.isRunning)
        .toolbar {
            if session.isRunning {
                ToolbarItem(placement: .topBarLeading) {
                    Button { confirmLeave = true } label: {
                        Label("Back", systemImage: "chevron.backward")
                    }
                }
            } else {
                // Not while capturing: leaving this screen stops the capture.
                ToolbarItem(placement: .primaryAction) {
                    NavigationLink {
                        CapturesListView()
                    } label: {
                        Label("Saved captures", systemImage: "folder")
                    }
                }
            }
        }
        .confirmationDialog(Text("Stop the capture?"), isPresented: $confirmLeave, titleVisibility: .visible) {
            Button("Stop and leave", role: .destructive) {
                session.stop()
                dismiss()
            }
            Button("Keep capturing", role: .cancel) {}
        } message: {
            Text("The packets captured so far stay in the saved captures.")
        }
        .task { await loadLinks() }
        .task(id: options.trimmedFilter) { await validateFilter() }
        .onDisappear { if session.isRunning { session.leave() } }
    }

    private var problem: CaptureOptionsProblem? {
        captureOptionsProblem(options, validatedFilter: validatedFilter, filterError: filterError)
    }

    private var setupForm: some View {
        Form {
            Section {
                interfacePicker
                Toggle("Show virtual interfaces", isOn: $showVirtual)
            } header: {
                Text("Interface")
            }
            filterSection
            Section("Limits") {
                Picker("Duration", selection: $options.duration) {
                    ForEach(CaptureDuration.allCases) { Text($0.localizedLabel).tag($0) }
                }
                Picker("Maximum size", selection: $options.sizeLimit) {
                    ForEach(CaptureSizeLimit.allCases) { Text(verbatim: "\($0.rawValue) MiB").tag($0) }
                }
                Toggle(isOn: $options.promiscuous) {
                    VStack(alignment: .leading) {
                        Text("Promiscuous mode")
                        Text("Also capture traffic not addressed to this node.").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                Button("Start capture") { Task { await start() } }
                    .disabled(problem != nil)
            } footer: {
                Text("The capture is saved on this phone. It can contain sensitive traffic (tokens, internal names): share it with care.")
            }
        }
        .themedBackground()
    }

    @ViewBuilder private var interfacePicker: some View {
        switch links {
        case .loading:
            ProgressView()
        case .failed(let message):
            Text(message).font(.footnote).foregroundStyle(.statusBad)
            Button("Retry") { Task { await loadLinks() } }
        case .loaded(let all, _, _):
            let shown = captureInterfaces(all, includeVirtual: showVirtual || all.first { $0.name == options.interface }?.virtual == true)
            Picker("Interface", selection: $options.interface) {
                ForEach(shown) { link in
                    Text(verbatim: link.isUp ? link.name : "\(link.name) (\(String(localized: "down")))").tag(link.name)
                }
            }
        }
    }

    private var filterSection: some View {
        Section {
            TextField("Filter (BPF), empty = everything", text: $options.filter, axis: .vertical)
                .font(.body.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack {
                    ForEach(CapturePreset.all) { preset in
                        Button(preset.localizedName) { options.filter = preset.filter }
                            .buttonStyle(.bordered)
                            .tint(options.trimmedFilter == preset.filter ? Color.accentColor : Color.secondary)
                            .controlSize(.small)
                    }
                }
            }
            switch problem {
            case .invalidFilter(let message):
                Text(message).font(.footnote).foregroundStyle(.statusBad)
            case .checkingFilter:
                Text("Checking the filter…").font(.footnote).foregroundStyle(.secondary)
            default:
                EmptyView()
            }
        } header: {
            Text("Filter")
        } footer: {
            Text("tcpdump syntax, e.g. host 192.0.2.1 and tcp port 443. The Talos API port (50000) is never captured: it carries the capture itself.")
        }
    }

    private func loadLinks() async {
        guard let client = model.client else { return }
        links = await .from { try await client.network(node: node).links }
        if case .loaded(let all, _, _) = links, options.interface.isEmpty {
            options.interface = captureInterfaces(all, includeVirtual: false).first?.name ?? ""
        }
    }

    /// Debounced: Go compiles the expression after typing pauses.
    private func validateFilter() async {
        let filter = options.trimmedFilter
        guard !filter.isEmpty else {
            validatedFilter = filter
            filterError = ""
            return
        }
        try? await Task.sleep(nanoseconds: 400_000_000)
        guard !Task.isCancelled else { return }
        let error = await Task.detached { TalosClient.validateCaptureFilter(filter) }.value
        guard !Task.isCancelled else { return }
        validatedFilter = filter
        filterError = error
    }

    private func start() async {
        guard let client = model.client, problem == nil else { return }
        session.start(client: client, node: node, hostname: hostname, options: options)
    }
}

extension CaptureDuration {
    var localizedLabel: String {
        switch self {
        case .s10: String(localized: "10 s")
        case .s30: String(localized: "30 s")
        case .s60: String(localized: "60 s")
        case .m5: String(localized: "5 min")
        }
    }
}

extension CapturePreset {
    var localizedName: String {
        switch name {
        case "Kubernetes API": String(localized: "Kubernetes API")
        default: name
        }
    }
}

extension ProtoKind {
    var color: Color {
        switch self {
        case .tcp: .blue
        case .udp: .purple
        case .icmp: .orange
        case .dns: .teal
        case .tls: .green
        case .arp: .gray
        case .other: .secondary
        }
    }
}

/// Stats, live list (newest at the bottom, follows unless scrolled up), then the file actions.
private struct CaptureLiveView: View {
    let session: CaptureSession
    let newCapture: () -> Void

    @State private var follow = true
    @State private var selected: PacketSummary?
    @State private var deleted = false

    var body: some View {
        VStack(spacing: 0) {
            statsBar
            ScrollViewReader { proxy in
                List(session.packets) { packet in
                    Button { if !session.isRunning { selected = packet } } label: { PacketRow(packet: packet) }
                        .buttonStyle(.plain)
                        .id(packet.n)
                        .onAppear { if packet.n == session.packets.last?.n { follow = true } }
                }
                .listStyle(.plain)
                .simultaneousGesture(DragGesture().onChanged { _ in follow = false })
                .overlay {
                    if session.packets.isEmpty && session.isRunning {
                        ContentUnavailableView("Waiting for packets…", systemImage: "antenna.radiowaves.left.and.right")
                    }
                }
                .overlay(alignment: .bottomTrailing) {
                    if !follow, let last = session.packets.last {
                        Button {
                            follow = true
                            proxy.scrollTo(last.n, anchor: .bottom)
                        } label: {
                            Label("Jump to latest", systemImage: "arrow.down.to.line")
                        }
                        .buttonStyle(.borderedProminent)
                        .padding()
                    }
                }
                .onChange(of: session.packets.last?.n) { _, last in
                    if follow, let last { proxy.scrollTo(last, anchor: .bottom) }
                }
            }
            if case .finished(let file, let error) = session.state {
                finishedBar(file: deleted ? nil : file, error: error)
            }
        }
        .sheet(item: $selected) { packet in
            if case .finished(let file?, _) = session.state {
                NavigationStack { PacketDetailView(path: file.path, packet: packet) }
            }
        }
    }

    private var statsBar: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading) {
                Text("\(session.packetCount) packets").font(.subheadline.monospacedDigit())
                Text(verbatim: formatBytes(session.bytes)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            Spacer()
            TimelineView(.periodic(from: .now, by: 1)) { context in
                let end = session.stoppedAt ?? context.date
                Text(verbatim: formatElapsed(Int(end.timeIntervalSince(session.startedAt ?? end))))
                    .font(.subheadline.monospacedDigit())
            }
            if session.isRunning {
                Button("Stop", role: .destructive) { session.stop() }
                    .buttonStyle(.borderedProminent)
                    .tint(.red)
            }
        }
        .padding(.horizontal).padding(.vertical, 8)
        .background(.bar)
    }

    private func finishedBar(file: URL?, error: String?) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
            if let file {
                Text(verbatim: file.lastPathComponent).font(.caption.monospaced()).foregroundStyle(.secondary)
                CaptureFileActions(url: file, onDelete: { deleted = true })
            } else if deleted {
                Text("Capture deleted.").font(.footnote).foregroundStyle(.secondary)
            }
            Button("New capture", action: newCapture)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.bar)
    }
}

/// "12  10:42:01.123  TCP  192.0.2.1:443 → 192.0.2.9:51000  [ACK] …"
struct PacketRow: View {
    let packet: PacketSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(verbatim: "\(packet.number)").foregroundStyle(.secondary)
                Text(Date(epochMillis: packet.ts), format: .dateTime.hour().minute().second().secondFraction(.fractional(3)))
                    .foregroundStyle(.secondary)
                Text(verbatim: packet.proto.isEmpty ? "?" : packet.proto)
                    .fontWeight(.semibold)
                    .foregroundStyle(packet.protoKind.color)
                    .padding(.horizontal, 4)
                    .background(packet.protoKind.color.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))
                Spacer()
                Text(verbatim: "\(packet.len) B").foregroundStyle(.secondary)
            }
            Text(verbatim: "\(packet.src) → \(packet.dst)").lineLimit(1).truncationMode(.middle)
            if !packet.info.isEmpty {
                Text(verbatim: packet.info).foregroundStyle(.secondary).lineLimit(2)
            }
        }
        .font(.caption.monospaced())
    }
}

/// Share, Save to Files and Delete for a capture file.
struct CaptureFileActions: View {
    let url: URL
    let onDelete: () -> Void

    var body: some View {
        LocalFileActions(url: url, contentType: .pcap, wrapperOptions: .immediate,
                         deleteTitle: Text("Delete this capture?"), onDelete: onDelete)
    }
}
