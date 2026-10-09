import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// The last lines of a pod's log through the Kubernetes API, like `kubectl logs --tail`
/// (os:admin), with the previous run's (`--previous`) when its containers restarted: Talos
/// only keeps the running container's. A pod with several containers asks which one. A row
/// read from a Table (a large cluster's) has no containers nor last termination: the pod is
/// read in full first. Follow streams new lines (`kubectl logs -f`) while the sheet is open;
/// the toolbar also opens the pod's events, a port-forward and a shell in the container.
struct PodLogsSheet: View {
    let pod: KubePod

    init(pod: KubePod) {
        self.pod = pod
        _containers = State(initialValue: pod.containerNames)
        _container = State(initialValue: pod.containerNames.count > 1 ? pod.containerNames[0] : "")
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<String> = .loading
    @State private var previous = false
    /// "" lets Kubernetes pick the only container.
    @State private var container: String
    /// The pod's containers (from the API's error with an older core).
    @State private var containers: [String]
    @State private var message: String?
    /// Bumped by Refresh: part of the load task's id, so a refresh replaces the running load.
    @State private var refreshes = 0
    /// The latest load: an older one (the Go call ignores cancellation) never lands after it.
    @State private var generation = 0
    /// The pod read in full when the row lacked its containers; nil until then, or if that failed.
    @State private var detail: KubePod?
    /// Whether the full pod was asked for already.
    @State private var detailAsked = false
    /// Streams new lines instead of showing the last ones.
    @State private var following = false
    @State private var follower = LogFollower()
    @State private var display = LogDisplay()
    @State private var forwarding = false
    @State private var shelling = false
    /// CPU and memory in use (metrics-server); nil without it.
    @State private var usage: KubeTopPod?

    /// The pod with its containers and last termination when known.
    private var shown: KubePod { detail ?? pod }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let usage { PodUsageView(top: usage).padding(.horizontal).padding(.top, 8) }
                if containers.count > 1 || shown.restarts > 0 {
                    options.padding(.horizontal).padding(.vertical, 8)
                }
                if following {
                    FollowList(follower: follower, display: $display)
                } else {
                    LoadStateView(state: state, retry: { refreshes += 1 }) { text in LogText(lines: podLogLines(text), previous: previous) }
                }
            }
            .themedBackground()
            .task(id: pod.id) {
                let top = try? await model.client?.topPods(namespace: pod.namespace)
                usage = top?.available == true ? top?.byKey[pod.id] : nil
            }
            .navigationTitle(Text(verbatim: pod.name))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
                ToolbarItemGroup(placement: .primaryAction) {
                    Toggle(isOn: $following) {
                        Label("Follow", systemImage: "dot.radiowaves.left.and.right")
                    }
                    .toggleStyle(.button)
                    // Why it does not start, when there is no log yet.
                    NavigationLink {
                        KubeEventsPage(namespace: pod.namespace, kind: "Pod", name: pod.name)
                    } label: {
                        Image(systemName: "bell.badge").accessibilityLabel(Text(verbatim: CheckupText.kubeEventsTitle))
                    }
                    Menu {
                        if !following, case .loaded(let text, _, _) = state, !text.isEmpty {
                            Button { copy(text) } label: { Label("Copy", systemImage: "doc.on.doc") }
                            ShareLink(item: text) { Label("Share", systemImage: "square.and.arrow.up") }
                        }
                        Button { forwarding = true } label: {
                            Label("Port forward", systemImage: "arrow.left.arrow.right.circle")
                        }
                        KubeGatedMenuButton(String(localized: "Shell"), systemImage: "terminal", action: .execPod, in: pod.namespace) {
                            shelling = true
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                    }
                    if !following {
                        Button { refreshes += 1 } label: { Image(systemName: "arrow.clockwise") }
                            .accessibilityLabel(Text("Refresh"))
                    }
                }
            }
            .task(id: "\(container)/\(previous)/\(refreshes)/\(following)") {
                if following { await follow() } else { await load() }
            }
            // A previous run's log has no new lines.
            .onChange(of: following) { _, on in if on { previous = false } }
            .navigationDestination(isPresented: $forwarding) {
                PortForwardView(namespace: pod.namespace, pod: pod.name)
            }
            // In the container picked for the log; "" lets Kubernetes pick the only one.
            .navigationDestination(isPresented: $shelling) {
                PodShellView(namespace: pod.namespace, pod: pod.name, container: container)
            }
            .messageAlert($message)
            // Outermost: the toolbar's menu reads it too.
            .loadsKubeActionAccess(namespace: pod.namespace)
        }
    }

    private var options: some View {
        VStack(alignment: .leading, spacing: 8) {
            if containers.count > 1 {
                Picker("Container", selection: $container) {
                    ForEach(containers, id: \.self) { Text(verbatim: $0).tag($0) }
                }
            }
            if shown.restarts > 0 {
                Toggle(isOn: $previous) {
                    VStack(alignment: .leading) {
                        Text("Previous run")
                        Text("The log of the container's last run before it restarted.").font(.caption).foregroundStyle(.secondary)
                        if !shown.lastTermination.isEmpty {
                            Text("Last stop: \(shown.lastTermination)").font(.caption).foregroundStyle(.statusWarn)
                        }
                    }
                }
                .disabled(following)
            }
        }
    }

    /// Only through the task: a switch of container or run, or a refresh, cancels the load
    /// before, and its late answer is dropped.
    private func load() async {
        guard let client = model.client else { return }
        if await readDetail(client) { return }
        generation += 1
        let (asked, askedPrevious, mine) = (container, previous, generation)
        // Still the selection on screen, and no newer load started.
        let current = { !Task.isCancelled && mine == generation && asked == container && askedPrevious == previous }
        state = .loading
        do {
            let text = try await client.podLogs(pod, container: asked, previous: askedPrevious)
            guard current() else { return }
            state = .loaded(text, at: Date())
        } catch {
            guard current() else { return }
            let choices = podLogContainerChoices(fromError: error.localizedDescription)
            // Several containers: pick the first, the task loads again for it.
            if asked.isEmpty, let first = choices.first {
                containers = choices
                container = first
            } else {
                state = .failed(error.localizedDescription)
            }
        }
    }

    /// Streams the selected container's log while following; a pod with several containers
    /// gets its first one picked first (the task then follows that one).
    private func follow() async {
        guard let client = model.client else { return }
        if await readDetail(client) { return }
        if container.isEmpty, containers.count > 1 {
            container = containers[0]
            return
        }
        await follower.follow(client.followPodLogs(namespace: pod.namespace, pod: pod.name, container: container))
    }

    /// Reads the pod in full once when its row had no containers; true when that picked a
    /// container, whose own load the task then runs.
    private func readDetail(_ client: TalosClient) async -> Bool {
        guard !detailAsked, pod.containerNames.isEmpty else { return false }
        detailAsked = true
        state = .loading
        guard let full = try? await client.pod(namespace: pod.namespace, name: pod.name), !Task.isCancelled else { return false }
        detail = full
        guard full.containerNames.count > 1, container.isEmpty else { return false }
        containers = full.containerNames
        container = full.containerNames[0]
        return true
    }

    /// Device-only pasteboard: a log can hold secrets.
    private func copy(_ text: String) {
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: text]], options: [.localOnly: true])
        message = String(localized: "Copied.")
    }
}

/// The log lines, monospaced and selectable, scrolled to the newest.
private struct LogText: View {
    let lines: [String]
    let previous: Bool

    var body: some View {
        if lines.isEmpty && previous {
            ContentUnavailableView("No log from the previous run", systemImage: "doc.text")
        } else if lines.isEmpty {
            ContentUnavailableView("No log yet", systemImage: "doc.text")
        } else {
            // A List, not a LazyVStack under .defaultScrollAnchor(.bottom): the stack guesses the
            // height of the wrapped lines it has not drawn yet, and corrects the offset as they
            // appear, so scrolling back bounced up and down.
            ScrollViewReader { proxy in
                List(lines.indices, id: \.self) { index in
                    Text(verbatim: lines[index])
                        .monoFont()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                        .listRowInsets(EdgeInsets(top: 0.5, leading: 16, bottom: 0.5, trailing: 16))
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.clear)
                }
                .listStyle(.plain)
                .scalableMonoText(.logs)
                .scrollContentBackground(.hidden)
                .onAppear { proxy.scrollTo(lines.count - 1, anchor: .bottom) }
            }
        }
    }
}
