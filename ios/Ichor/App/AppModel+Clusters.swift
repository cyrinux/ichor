import Foundation
import Observation
import IchorCore

// Loading, saving and editing the stored clusters, and showing a parsed config.
extension AppModel {
    /// How clusters are called on screen (given names, unless the screenshot mode is on).
    var labels: ClusterLabels { ClusterLabels(names: clusterNames, masked: settings.privacyMask) }

    /// The name of the cluster on screen.
    var activeLabel: String { activeSummary.map(labels.of) ?? activeContext }

    /// Loads the stored configs (Keychain); nothing is read before the first unlock.
    func load() async {
        StartupTrace.markOnce("config load start")
        defer {
            loaded = true
            StartupTrace.markOnce("config emitted")
        }
        WakeOnLanStore.shared.loadIfNeeded()
        let stored = StoredConfigs.load()
        StartupTrace.markOnce("config decrypted")
        let parsed = await stored.parsed()
        // Fail closed: one store unreadable shows neither (the other alone could overwrite it).
        unreadable = parsed.unreadable
        guard unreadable.isEmpty, let all = ConfigSummary.combined(talos: parsed.talos, kube: parsed.kube) else {
            return
        }
        apply(
            talos: parsed.talos == nil ? nil : stored.talos, talosSummary: parsed.talos,
            kube: parsed.kube == nil ? nil : stored.kube, kubeSummary: parsed.kube,
            preferred: all.selectedContext(
                index: Self.savedContextIndex, name: UserDefaults.standard.string(forKey: Keys.context)),
            generation: parsed.generation)
    }

    /// Stores `newYAML`. With a config already stored its contexts are added to it (one
    /// stored talosconfig, a context per cluster). A stored context is never overwritten: one
    /// of the same name (Talos or kubeconfig cluster) is added as name-1, name-2…, unless
    /// `replacingSameCluster` (the demo added again) and it is the same cluster (same CA). The
    /// imported config's current context becomes the one shown.
    func save(yaml newYAML: String, replacingSameCluster: Bool = false) async throws {
        try checkWritable()
        let merged: String
        if yaml != nil || kubeYAML != nil {
            let stored = yaml ?? ""
            let kube = kubeYAML ?? ""
            var choices = ""
            if replacingSameCluster {
                let conflicts = try await TalosClient.talosImportConflicts(stored: stored, kube: kube, added: newYAML)
                choices =
                    "["
                    + conflicts.filter { $0.sameAs != nil }
                    .map { "{\"index\":\($0.index),\"replace\":true}" }
                    .joined(separator: ",") + "]"
            }
            merged = try await TalosClient.mergeTalosconfig(
                stored: stored, kube: kube, added: newYAML, choices: choices)
        } else {
            merged = newYAML
        }
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        let parsed = try await TalosClient.parse(merged)
        try SecureConfigStore.save(Data(merged.utf8))
        apply(
            talos: merged, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: parsed.current,
            generation: generation)
    }

    /// Adds the contexts of the kubeconfig `added` to the stored kubeconfig, as `choices` say
    /// (contexts left out, same cluster replaced; see KubeImportConflicts for the names). The
    /// imported kubeconfig's current context becomes the one shown.
    func saveKube(added: String, choices: [KubeImportChoice]) async throws {
        try checkWritable()
        let merged = try await TalosClient.mergeKubeconfig(
            stored: kubeYAML ?? "", talos: yaml ?? "", added: added, choices: choices)
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        let parsed = try await TalosClient.parseKubeconfig(merged)
        try SecureConfigStore.save(Data(merged.utf8), item: .kubeconfig)
        apply(
            talos: yaml, talosSummary: talosSummary, kube: merged, kubeSummary: parsed, preferred: parsed.current,
            generation: generation)
    }

    /// Adds the built-in Kubernetes demo next to the stored clusters, replacing one added before.
    func saveKubeDemo() async throws {
        let added = try await TalosClient.demoKubeconfig()
        let conflicts = try await TalosClient.kubeImportConflicts(
            stored: kubeYAML ?? "", talos: yaml ?? "", added: added)
        let choices = conflicts.filter { $0.sameAs != nil }.map { KubeImportChoice(index: $0.index, replace: true) }
        try await saveKube(added: added, choices: choices)
    }

    /// Replaces both stored configs with those of a restored backup (nil: none of that kind)
    /// and shows the context at `activeIndex`. Both are checked before anything is written, and
    /// a failed write puts the previous configs back, so a failure leaves everything as it was.
    func replace(talos newTalos: String?, kube newKube: String?, activeIndex: Int?) async throws {
        try checkWritable()
        var parsedTalos: ConfigSummary?
        var parsedKube: ConfigSummary?
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        if let newTalos { parsedTalos = try await TalosClient.parse(newTalos) }
        if let newKube { parsedKube = try await TalosClient.parseKubeconfig(newKube) }
        guard let all = ConfigSummary.combined(talos: parsedTalos, kube: parsedKube) else {
            throw TalosError(message: String(localized: "The backup holds no cluster."))
        }
        let previous = StoredConfigs.load()
        do {
            try write(newTalos, item: .talosconfig)
            try write(newKube, item: .kubeconfig)
        } catch {
            try? write(previous.talos, item: .talosconfig)
            try? write(previous.kube, item: .kubeconfig)
            throw error
        }
        SharedStore.clear()  // the widget stops showing the previous config's clusters
        apply(
            talos: newTalos, talosSummary: parsedTalos, kube: newKube, kubeSummary: parsedKube,
            preferred: all.selectedContext(index: activeIndex, name: nil), generation: generation)
        dataGeneration += 1
    }

    /// Writes are refused while a stored config could not be read (see unreadableConfigs).
    private func checkWritable() throws {
        guard unreadable.isEmpty else {
            throw TalosError(
                message: String(localized: "A stored config could not be read. Nothing is changed until it can be."))
        }
    }

    /// Stores `text` as `item`, or deletes it when nil.
    private func write(_ text: String?, item: SecureConfigStore.Item) throws {
        if let text {
            try SecureConfigStore.save(Data(text.utf8), item: item)
        } else {
            SecureConfigStore.delete(item)
        }
    }

    /// Restored names (all of them) and colors (the others keep the one just assigned) of the stored clusters.
    func restoreClusterSettings(
        names: [String: String], colors: [String: Int], kubeServers servers: [String: String],
        vpnOnly restoredVpnOnly: Set<String> = [], kubeAccess links: [String: String] = [:],
        repos: [String: ClusterRepo] = [:], auditEvents restoredAuditEvents: Set<String> = []
    ) {
        storeColors(clusterColors.merging(colors) { _, new in new })
        storeNames(names)
        storeKubeServers(servers)
        settings.storeVpnOnly(restoredVpnOnly)
        settings.storeAuditEventClusters(restoredAuditEvents)
        storeKubeAccess(links)
        storeClusterRepos(repos)
    }

    /// Removes the cluster `name` (a context and its credentials) from the store it is in,
    /// showing its neighbour if it was the active one. Removing a store's last cluster deletes
    /// that store; removing the very last one deletes everything (clear).
    func removeCluster(_ name: String) async throws {
        try checkWritable()
        guard let before = summary, let removed = before.contexts.firstIndex(where: { $0.name == name }) else { return }
        guard before.contexts.count > 1 else {
            clear()
            return
        }
        let active = before.contexts.firstIndex { $0.name == activeContext } ?? removed
        // By position: the masked names of the screenshot mode may change with the set of contexts.
        let index = ConfigSummary.activeIndexAfterRemoval(
            active: active, removed: removed, remaining: before.contexts.count - 1)
        if before.contexts[removed].isKube {
            guard let current = kubeYAML else { return }
            let left = try await TalosClient.removeKubeContext(stored: current, context: name)
            // "" once no kubeconfig cluster is left: the store goes.
            let remaining = left.isEmpty ? nil : left
            let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
            var parsed: ConfigSummary?
            if let remaining { parsed = try await TalosClient.parseKubeconfig(remaining) }
            try write(remaining, item: .kubeconfig)
            applyIndex(
                talos: yaml, talosSummary: talosSummary, kube: remaining, kubeSummary: parsed, index: index,
                generation: generation)
        } else {
            guard let current = yaml else { return }
            // The talosconfig's last cluster: the store goes (kubeconfig clusters are left).
            let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
            var remaining: String?
            var parsed: ConfigSummary?
            if (talosSummary?.contexts.count ?? 0) > 1 {
                let left = try await TalosClient.removeContext(stored: current, context: name)
                remaining = left
                parsed = try await TalosClient.parse(left)
            }
            try write(remaining, item: .talosconfig)
            applyIndex(
                talos: remaining, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, index: index,
                generation: generation)
        }
    }

    /// apply, showing the context at `index` of the combined list.
    private func applyIndex(
        talos: String?, talosSummary: ConfigSummary?, kube: String?, kubeSummary: ConfigSummary?,
        index: Int?, generation: Int
    ) {
        let all = ConfigSummary.combined(talos: talosSummary, kube: kubeSummary)
        apply(
            talos: talos, talosSummary: talosSummary, kube: kube, kubeSummary: kubeSummary,
            preferred: all?.selectedContext(index: index, name: nil), generation: generation)
    }

    /// Replaces the active context's ca/crt/key with those of `generated` (a renewed
    /// single-context talosconfig), keeping the other contexts and the active context. The
    /// result must parse, keep the same contexts and carry exactly the new certificate.
    func renewCredentials(with generated: String) async throws {
        try await replaceCredentials(of: activeContext, with: generated)
    }

    /// renewCredentials for the context `context` whichever is on screen (a CA rotation that
    /// went on while the user moved to another cluster); the context on screen stays.
    func replaceCredentials(of context: String, with generated: String) async throws {
        try checkWritable()
        guard let current = yaml, let before = talosSummary else {
            throw TalosError(message: String(localized: "No talosconfig is stored."))
        }
        let onScreen = activeContext
        let patched = try await TalosClient.replaceContextCredentials(
            stored: current, generated: generated, context: context)
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        let parsed = try await TalosClient.parse(patched)
        let issued = try await TalosClient.parse(generated)
        guard let renewed = parsed.context(named: context), let fresh = issued.context(named: context),
            renewed.certNotAfter == fresh.certNotAfter, Set(renewed.roles) == Set(fresh.roles),
            parsed.contexts.map(\.name) == before.contexts.map(\.name)
        else {
            throw TalosError(
                message: String(localized: "The renewed talosconfig did not validate; the stored one is unchanged."))
        }
        try SecureConfigStore.save(Data(patched.utf8))
        apply(
            talos: patched, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: onScreen,
            generation: generation)
    }

    /// Adds discovered `nodes` (addresses) to the nodes of `contextName` (the active context by
    /// default), keeping the other contexts; the overview reloads with them.
    func addNodes(_ nodes: [String], to contextName: String? = nil) async throws {
        try checkWritable()
        guard let current = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let context = activeContext
        let updated = try await TalosClient.addContextNodes(
            stored: current, context: contextName ?? context, nodes: nodes)
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        let parsed = try await TalosClient.parse(updated)
        try SecureConfigStore.save(Data(updated.utf8))
        apply(
            talos: updated, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary, preferred: context,
            generation: generation)
        dataGeneration += 1
    }

    /// Replaces the endpoints of the context `name` (the addresses the app connects through),
    /// keeping the other contexts; screens reload with them.
    func setEndpoints(_ endpoints: [String], of name: String) async throws {
        guard let current = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let updated = try await TalosClient.setContextEndpoints(stored: current, context: name, endpoints: endpoints)
        try await store(updated)
    }

    /// Puts each endpoint a network search found first among the endpoints of the contexts it
    /// answered for, all in one write: nothing is stored if one fails. Once a cluster answers
    /// again, node discovery offers the members it still misses.
    func addFoundEndpoints(_ matches: [EndpointMatch]) async throws {
        guard var updated = yaml else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        for match in matches {
            for context in match.contexts {
                updated = try await TalosClient.addContextEndpoint(
                    stored: updated, context: context, endpoint: match.endpoint)
            }
        }
        try await store(updated)
    }

    /// Stores an edited config, keeping the context on screen, and reloads the screens.
    private func store(_ updated: String) async throws {
        try checkWritable()
        let generation = TalosClient.privacyGeneration()  // before parsing: see ConfigSummaryCache.store
        let parsed = try await TalosClient.parse(updated)
        try SecureConfigStore.save(Data(updated.utf8))
        let index = summary?.contexts.firstIndex { $0.name == activeContext }
        let all = ConfigSummary.combined(talos: parsed, kube: kubeSummary)
        apply(
            talos: updated, talosSummary: parsed, kube: kubeYAML, kubeSummary: kubeSummary,
            preferred: all?.selectedContext(index: index, name: activeContext), generation: generation)
        dataGeneration += 1
    }

    /// Deletes every stored cluster: both configs, and what was kept about them.
    func clear() {
        SecureConfigStore.delete()
        LastKnownStore.wipe()
        SearchRecents.clear()
        RecentTargetStore.shared.clear()
        PinStore.shared.wipe()  // they name the clusters' nodes and objects
        PublicIPStore.wipe()
        MetricsStore.keep(fingerprints: [])  // their credentials go with the config
        AlertmanagerStore.keep(fingerprints: [])
        WakeOnLanStore.shared.wipe()
        KubeAuthStore.shared.wipe()  // its sealed item went with the others
        settings.storeVpnOnly([])
        settings.storeMonitorUnwatched([])
        AlertSnoozeStore.keep(clusters: [])
        AlertRulesStore.keep(fingerprints: [])
        AlertMuteStore.keep(clusters: [])
        storeKubeAccess([:])
        publicIPReports = [:]
        SharedStore.clear()  // the widget stops showing the old clusters
        HistoryStore.wipe()
        forgetFeatures()
        yaml = nil
        kubeYAML = nil
        talosSummary = nil
        kubeSummary = nil
        unreadable = []
        summary = nil
        QuickActions.update(summary: nil, labels: labels)
        publishWidgetClusters()
        SpotlightIndex.wipe()
    }

    /// The saved context position; nil for installs from before it was saved.
    nonisolated static var savedContextIndex: Int? {
        UserDefaults.standard.object(forKey: Keys.contextIndex) as? Int
    }

    /// Parses the stored config again, keeping the active context by position (its name
    /// changes when masking is toggled).
    // Internal, not private: AppModel+Settings.swift uses it.
    func reparse() async {
        guard let before = summary else { return }
        let stored = StoredConfigs(talos: yaml, kube: kubeYAML)
        let parsed = await stored.parsed()
        // Both stores parsed before: one that no longer does is kept as it was rather than dropped.
        guard parsed.unreadable.isEmpty else { return }
        let index = before.contexts.firstIndex { $0.name == activeContext }
        applyIndex(
            talos: yaml, talosSummary: parsed.talos, kube: kubeYAML, kubeSummary: parsed.kube, index: index,
            generation: parsed.generation)
    }

    /// Shows the stored configs `talos` and `kube` (nil: none of that kind) and their parsed
    /// summaries, on the context `preferred` (else the current one). Callers keep at least one.
    private func apply(
        talos newTalos: String?, talosSummary newTalosSummary: ConfigSummary?,
        kube newKube: String?, kubeSummary newKubeSummary: ConfigSummary?, preferred: String?,
        generation: Int
    ) {
        guard let newSummary = ConfigSummary.combined(talos: newTalosSummary, kube: newKubeSummary) else { return }
        // Every change of the stored configs comes through here: the next launch reads these.
        ConfigSummaryCache.shared.store(
            talos: newTalos, talosSummary: newTalosSummary, kube: newKube, kubeSummary: newKubeSummary,
            generation: generation)
        forgetFeatures()
        yaml = newTalos
        kubeYAML = newKube
        talosSummary = newTalosSummary
        kubeSummary = newKubeSummary
        summary = newSummary
        activeContext = preferred.flatMap { newSummary.context(named: $0)?.name } ?? newSummary.current
        // Every cluster gets a color of its own; removed ones are forgotten.
        storeColors(assignClusterColors(saved: clusterColors, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeNames(keepClusterNames(saved: clusterNames, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeKubeServers(keepClusterNames(saved: kubeServers, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeSnapshotKeys(keepClusterNames(saved: snapshotKeys, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeSkippedTalosUpdates(
            keepClusterNames(saved: skippedTalosUpdates, fingerprints: newSummary.contexts.map(\.fingerprint)))
        settings.storeVpnOnly(
            keepVpnOnly(saved: settings.vpnOnly, fingerprints: newSummary.contexts.map(\.fingerprint)))
        // The app-wide "Record actions" switch of older versions, once: on, every cluster keeps it.
        let defaults = UserDefaults.standard
        if defaults.object(forKey: AuditEventKeys.enabled) != nil {
            let seeded = seededAuditEventClusters(
                legacyOn: defaults.bool(forKey: AuditEventKeys.enabled),
                contexts: newSummary.contexts)
            defaults.removeObject(forKey: AuditEventKeys.enabled)
            settings.storeAuditEventClusters(settings.auditEventClusters.union(seeded))
        }
        // Per cluster: a context added to a recording cluster records too; removed ones go.
        settings.storeAuditEventClusters(wholeClusters(settings.auditEventClusters, contexts: newSummary.contexts))
        storeClusterRepos(ClusterRepoStore.keep(clusterRepos, fingerprints: newSummary.contexts.map(\.fingerprint)))
        storeProposals(ProposalsStore.keep(storedProposals, fingerprints: newSummary.contexts.map(\.fingerprint)))
        // Sign-ins and Kubernetes access links go with their clusters.
        let kubeFingerprints = newKubeSummary?.contexts.map(\.fingerprint) ?? []
        storeKubeAccess(
            keepKubeAccess(
                kubeAccess, talos: newTalosSummary?.contexts.map(\.fingerprint) ?? [], kube: kubeFingerprints))
        // Omni clusters' sign-ins too: under their identity's auth key (and, from older cores,
        // their Talos context's fingerprint).
        KubeAuthStore.shared.keep(
            fingerprints: authStoreFingerprints(
                talos: newTalosSummary?.contexts ?? [],
                kube: newKubeSummary?.contexts ?? []))
        WakeOnLanStore.shared.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        LastKnownStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        MetricsStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        AlertmanagerStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        // A removed cluster's background state goes too: its snapshot, unreachable count and snoozes.
        settings.storeMonitorUnwatched(
            keepVpnOnly(saved: settings.monitorUnwatched, fingerprints: newSummary.contexts.map(\.fingerprint)))
        SharedStore.keep(clusters: newSummary.contexts.map(monitorClusterKey))
        HistoryStore.keep(clusters: newSummary.contexts.map(monitorClusterKey))
        AlertSnoozeStore.keep(clusters: newSummary.contexts.map(\.fingerprint))
        AlertRulesStore.keep(fingerprints: newSummary.contexts.map(\.fingerprint))
        AlertMuteStore.keep(clusters: newSummary.contexts.map(monitorClusterKey))
        let kept = publicIPReports.filter { report in newSummary.contexts.contains { $0.fingerprint == report.key } }
        if kept.count != publicIPReports.count {
            publicIPReports = kept
            try? PublicIPStore.save(kept)
        }
        QuickActions.update(summary: newSummary, labels: labels)
        publishWidgetClusters()
        updateSpotlight()
    }
}
