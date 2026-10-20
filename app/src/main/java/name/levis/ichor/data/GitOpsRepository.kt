package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ClusterRepo
import name.levis.ichor.model.DEMO_CLUSTER_REPO
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.FluxStatus
import name.levis.ichor.model.GitHost
import name.levis.ichor.model.GitHostCheck
import name.levis.ichor.model.GitFile
import name.levis.ichor.model.GitOpsSource
import name.levis.ichor.model.GitOpsSourceFind
import name.levis.ichor.model.GitProposal
import name.levis.ichor.model.GitProposalDefaults
import name.levis.ichor.model.GitProposalResult
import name.levis.ichor.model.GitRepoInfo
import name.levis.ichor.model.GitRepoSearch
import name.levis.ichor.model.TalosConfigPatch
import name.levis.ichor.model.TalosRepoCompare
import name.levis.ichor.model.TalosWriteBackPlan
import name.levis.ichor.model.TalosRepoFiles
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.GitTree
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.model.ProposeBlock
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.QuickEditResult
import name.levis.ichor.model.QuickEditTargets
import name.levis.ichor.model.decodeQuickEditResult
import name.levis.ichor.model.decodeQuickEditTargets
import name.levis.ichor.model.quickEditJson
import name.levis.ichor.model.decodeGitRepoInfo
import name.levis.ichor.model.repoSource
import name.levis.ichor.model.SourceReadPath
import name.levis.ichor.model.gitHostJsonLacksToken
import name.levis.ichor.model.gitProposalJson
import name.levis.ichor.model.proposeBlock
import name.levis.ichor.R
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichorgo.ArgoSyncListener
import name.levis.ichorgo.FluxReconcileListener
import name.levis.ichorgo.Ichorgo

/** Argo CD and Flux through their custom resources, on the Kubernetes API of the active cluster. */
class GitOpsRepository(
    go: GoCall,
    private val gitHosts: GitHostStore? = null,
    private val clusterRepos: ClusterRepoStore? = null,
) : GoRepository(go) {
    /**
     * Who [host]'s token signs in as and the scopes it lacks, asked of the forge (the demo
     * context answers without network). [token] checks a typed one before it is saved, else the
     * stored one is used. A successful check is remembered for the Settings list. Throws when refused.
     */
    suspend fun gitHostCheck(host: GitHost, token: String? = null): GitHostCheck {
        val store = checkNotNull(gitHosts) { "No Git host store" }
        val json = store.hostJson(host, token)
        val check = withContext(Dispatchers.IO) { decodeGitHostCheck(Ichorgo.gitForgeCheck(json)) }
        // A typed token is not saved yet: its login is remembered on Save, not here.
        if (token.isNullOrBlank()) store.rememberLogin(host.host, check.login)
        return check
    }

    /**
     * Argo CD Applications, ApplicationSets and projects through their custom resources
     * (os:admin); `installed` is false without Argo CD.
     */
    suspend fun argoCD(): ArgoStatus = go.remember(ARGO_CD) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(ArgoStatus.serializer(), Ichorgo.kubeArgoCD(cfg, ctx, server)) }
    }

    /** Runs [action] on [app] (os:admin); [options] for a sync or a rollback. Throws when refused. */
    suspend fun argoAction(app: ArgoApp, action: ArgoAction, options: ArgoSyncOptions? = null) = go.kube { cfg, ctx, server ->
        val json = options?.let { TalosJson.encodeToString(ArgoSyncOptions.serializer(), it) }.orEmpty()
        Ichorgo.kubeArgoAction(cfg, ctx, server, app.namespace, app.name, action.wire, json)
    }

    /**
     * Changes the sync windows of the AppProject [namespace]/[project] (os:admin): freezes apps
     * for a while, extends or ends a freeze, removes a window, clears ended freezes. Throws when
     * refused.
     */
    suspend fun argoFreeze(namespace: String, project: String, action: ArgoFreezeAction, options: ArgoFreezeOptions = ArgoFreezeOptions()) =
        go.kube { cfg, ctx, server ->
            Ichorgo.kubeArgoFreeze(cfg, ctx, server, namespace, project, action.wire, TalosJson.encodeToString(ArgoFreezeOptions.serializer(), options))
        }

    /**
     * How traffic reaches [app] (os:admin): hosts, Gateways, routes, Services, pods and nodes.
     * Never cached: the app detail asks again whenever it reloads the app.
     */
    suspend fun argoNetwork(app: ArgoApp): ArgoNetwork = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(ArgoNetwork.serializer(), Ichorgo.kubeArgoNetwork(cfg, ctx, server, app.namespace, app.name))
    }

    /**
     * Flux Kustomizations, HelmReleases and sources through their custom resources (os:admin);
     * `installed` is false without Flux.
     */
    suspend fun flux(): FluxStatus = go.remember(FLUX) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(FluxStatus.serializer(), Ichorgo.kubeFlux(cfg, ctx, server)) }
    }

    /** Runs [action] on the Flux object [kind] [namespace]/[name] (os:admin). Throws when refused. */
    suspend fun fluxAction(kind: String, namespace: String, name: String, action: FluxAction) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeFluxAction(cfg, ctx, server, kind, namespace, name, action.wire)
    }

    /**
     * What reconciling the Flux object [kind] [namespace]/[name] now would change, object by
     * object (os:admin, read only: server-side apply dry runs). Not cached: always fresh.
     */
    suspend fun fluxDiff(kind: String, namespace: String, name: String): FluxDiff = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.kubeFluxDiff(cfg, ctx, server, kind, namespace, name))
    }

    /**
     * What syncing the Argo CD Application [namespace]/[name] now would change, object by
     * object (os:admin, read only): what the application controller compared last, read from
     * Argo CD's Redis through a port-forward. Not cached on the phone: always fresh.
     */
    suspend fun argoDiff(namespace: String, name: String): FluxDiff = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.kubeArgoDiff(cfg, ctx, server, namespace, name))
    }

    /**
     * Where the files of an app are (os:admin, read only): [tool] "argocd" (kind Application)
     * or "flux" (Kustomization, HelmRelease), one entry per source. Not cached.
     */
    suspend fun source(tool: String, kind: String, namespace: String, name: String): GitOpsSource = go.kube { cfg, ctx, server ->
        decodeGitOpsSource(Ichorgo.gitOpsSource(cfg, ctx, server, tool, kind, namespace, name))
    }

    /**
     * The directory [dir] ("" for the root) of [src] at the revision the cluster runs: from the
     * Flux artifact (no token) or through the Git host's API with its saved token. Throws
     * [GitHostMissing] when the host has no token, and when [src] cannot be read at all.
     */
    suspend fun tree(src: GitSource, dir: String): GitTree = when (src.readPath) {
        SourceReadPath.FLUX -> {
            val ref = checkNotNull(src.fluxSource)
            go.kube { cfg, ctx, server -> decodeGitTree(Ichorgo.fluxSourceTree(cfg, ctx, server, ref.kind, ref.namespace, ref.name, dir)) }
        }
        SourceReadPath.FORGE -> {
            val host = forgeHost(src)
            withContext(Dispatchers.IO) { decodeGitTree(Ichorgo.gitForgeTree(host, src.repoURL, src.ref, dir)) }
        }
        SourceReadPath.NONE -> throw LocalizedException(UiText.Res(R.string.gitops_source_not_browsable))
    }

    /** The file [path] of [src], read the way [tree] reads its directories. */
    suspend fun file(src: GitSource, path: String): GitFile = when (src.readPath) {
        SourceReadPath.FLUX -> {
            val ref = checkNotNull(src.fluxSource)
            go.kube { cfg, ctx, server -> decodeGitFile(Ichorgo.fluxSourceFile(cfg, ctx, server, ref.kind, ref.namespace, ref.name, path)) }
        }
        SourceReadPath.FORGE -> {
            val host = forgeHost(src)
            withContext(Dispatchers.IO) { decodeGitFile(Ichorgo.gitForgeFile(host, src.repoURL, src.ref, path)) }
        }
        SourceReadPath.NONE -> throw LocalizedException(UiText.Res(R.string.gitops_source_not_browsable))
    }

    /**
     * The file [path] of [src] at [ref], always through the Git host's API with its saved token,
     * even for a Flux source read from its artifact: a proposal needs the forge's sha of the
     * file. Throws [GitHostMissing] without a token.
     */
    suspend fun forgeFile(src: GitSource, ref: String, path: String): GitFile {
        val host = forgeHost(src)
        return withContext(Dispatchers.IO) { decodeGitFile(Ichorgo.gitForgeFile(host, src.repoURL, ref, path)) }
    }

    /**
     * The manifests of source [index] ([src]) of [tool]'s [kind] [namespace]/[name] that declare
     * [resource] (os:admin, read only), searched under the source's path: from the Flux artifact
     * (no token) or through the Git host's API. Throws [GitHostMissing] when the forge needs a
     * token and none is saved.
     */
    suspend fun findSource(
        tool: String,
        kind: String,
        namespace: String,
        name: String,
        index: Int,
        src: GitSource,
        resource: KubeDiffResource,
    ): GitOpsSourceFind {
        val host = when (src.readPath) {
            SourceReadPath.FLUX -> ""
            SourceReadPath.FORGE -> forgeHost(src)
            SourceReadPath.NONE -> throw LocalizedException(UiText.Res(R.string.gitops_source_not_browsable))
        }
        return go.kube { cfg, ctx, server ->
            decodeGitOpsSourceFind(
                Ichorgo.gitOpsSourceFind(
                    cfg, ctx, server, host, tool, kind, namespace, name, index.toString(),
                    resource.kind, resource.namespace, resource.name,
                ),
            )
        }
    }

    /**
     * The lines of [src]'s whole repository that hold [query] (case-insensitive): from the Flux
     * artifact (no token), else from a snapshot of the repository at [src]'s revision,
     * downloaded once per commit (up to 48 MiB) with the Git host's saved token. Throws
     * [GitHostMissing] without a token.
     */
    suspend fun repoSearch(src: GitSource, query: String): GitRepoSearch = when (src.readPath) {
        SourceReadPath.FLUX -> {
            val ref = checkNotNull(src.fluxSource)
            go.kube { cfg, ctx, server ->
                decodeGitRepoSearch(Ichorgo.fluxSourceSearch(cfg, ctx, server, ref.kind, ref.namespace, ref.name, query, 0L))
            }
        }
        SourceReadPath.FORGE -> {
            val host = forgeHost(src)
            withContext(Dispatchers.IO) { decodeGitRepoSearch(Ichorgo.gitRepoSearch(host, src.repoURL, src.ref, query, 0L)) }
        }
        SourceReadPath.NONE -> throw LocalizedException(UiText.Res(R.string.gitops_source_not_browsable))
    }

    /** What stops proposing a change to [src]'s files (no Git host, no token), null when nothing does. */
    suspend fun proposeBlockOf(src: GitSource): ProposeBlock? {
        val blocked = proposeBlock(src, lacksToken = false)
        if (blocked != null) return blocked
        val store = gitHosts ?: return ProposeBlock.NotEditable
        val entry = store.hostFor(src.host)
        return proposeBlock(src, lacksToken = gitHostJsonLacksToken(store.hostJson(entry)))
    }

    /** The review of an edit: [before] → [after] of the file [name], as a one-file [FluxDiff]. */
    suspend fun textDiff(before: String, after: String, name: String): FluxDiff = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.textDiff(before, after, name))
    }

    /** What [content] offers to quick edits: its workloads' containers and replicas, a kustomization's images. No network. */
    suspend fun quickEditTargets(content: String): QuickEditTargets = withContext(Dispatchers.Default) {
        decodeQuickEditTargets(Ichorgo.gitOpsQuickEditTargets(content))
    }

    /** [content] with [edit] applied, comments and formatting kept (pure Go, no network). Throws when the target is not in the file. */
    suspend fun quickEdit(content: String, edit: QuickEdit): QuickEditResult = withContext(Dispatchers.Default) {
        decodeQuickEditResult(Ichorgo.gitOpsQuickEdit(content, quickEditJson(edit)))
    }

    /**
     * The prefilled pull request for a change of [path] deployed by [tool]'s [kind]
     * [namespace]/[name] on the active cluster. No network.
     */
    suspend fun proposalDefaults(tool: String, kind: String, namespace: String, name: String, path: String): GitProposalDefaults =
        go.kube { _, ctx, _ -> decodeGitProposalDefaults(Ichorgo.gitOpsProposalDefaults(tool, kind, namespace, name, path, ctx)) }

    /**
     * Commits [proposal] on a new branch of [src]'s repository and opens a pull (merge) request,
     * with the host's saved token; recorded in the active cluster's audit log, refused in the
     * demo context and in screenshot mode. Throws [GitHostMissing] without a token.
     */
    suspend fun propose(src: GitSource, proposal: GitProposal): GitProposalResult {
        val host = forgeHost(src)
        val options = gitProposalJson(src.repoURL, proposal)
        return go.kube { cfg, ctx, _ -> decodeGitProposalResult(Ichorgo.gitForgePropose(cfg, ctx, host, options)) }
    }

    /** The repository saved for the active cluster; the demo cluster's built-in one; null without one. */
    fun activeClusterRepo(): ClusterRepo? {
        val active = go.configs.config.value?.activeSummary ?: return null
        if (active.isDemo) return DEMO_CLUSTER_REPO
        return clusterRepos?.repos?.value?.get(active.fingerprint)
    }

    /**
     * What the forge tells of [repo] with its host's saved token: the default branch, the head
     * of the branch read, whether the token can push. Throws [GitHostMissing] without a token.
     */
    suspend fun repoInfo(repo: ClusterRepo): GitRepoInfo {
        val host = forgeHost(repo.host)
        return withContext(Dispatchers.IO) { decodeGitRepoInfo(Ichorgo.gitForgeRepo(host, repo.repoURL, repo.branch)) }
    }

    /** The active cluster's repository as a source of the source browser, at its branch head. */
    suspend fun clusterRepoSource(): GitSource {
        val repo = activeClusterRepo() ?: throw LocalizedException(UiText.Res(R.string.repo_none))
        return repoSource(repo, repoInfo(repo))
    }

    /**
     * The files of the active cluster's repository (under its Talos directory) that configure
     * the node [hostname] of [role], at its branch head. Throws without a repository or token.
     */
    suspend fun talosRepoFiles(hostname: String, role: String): TalosRepoFiles {
        val repo = activeClusterRepo() ?: throw LocalizedException(UiText.Res(R.string.repo_none))
        val host = forgeHost(repo.host)
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(TalosRepoFiles.serializer(), Ichorgo.talosRepoFiles(host, repo.repoURL, repo.branch, repo.talosDir, hostname, role))
        }
    }

    /**
     * Applies the repository files [paths] (in that order) at commit [sha] onto [node]'s live
     * config and says what differs (os:admin; only a dry run reaches the node).
     */
    suspend fun talosRepoCompare(node: String, hostname: String, sha: String, paths: List<String>): TalosRepoCompare {
        val repo = activeClusterRepo() ?: throw LocalizedException(UiText.Res(R.string.repo_none))
        val host = forgeHost(repo.host)
        val pathsJson = TalosJson.encodeToString(ListSerializer(String.serializer()), paths)
        return go.talos { cfg, ctx ->
            TalosJson.decodeFromString(TalosRepoCompare.serializer(), Ichorgo.talosRepoCompare(cfg, ctx, host, repo.repoURL, sha, node, hostname, pathsJson))
        }
    }

    /** The patch that turns [base] into [draft] (both redacted configs), its values and whether one is a secret's. Local. */
    suspend fun talosConfigPatchFrom(base: String, draft: String): TalosConfigPatch = withContext(Dispatchers.Default) {
        TalosJson.decodeFromString(TalosConfigPatch.serializer(), Ichorgo.talosConfigPatchFrom(base, draft))
    }

    /**
     * Where [patch] goes in the active cluster's repository for the node [hostname] of [role]:
     * its file (or a new one), with the values a shared patch sets left out.
     */
    suspend fun talosWriteBackPlan(hostname: String, role: String, patch: String): TalosWriteBackPlan {
        val repo = activeClusterRepo() ?: throw LocalizedException(UiText.Res(R.string.repo_none))
        val host = forgeHost(repo.host)
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(
                TalosWriteBackPlan.serializer(),
                Ichorgo.talosRepoWriteBackPlan(host, repo.repoURL, repo.branch, repo.talosDir, hostname, role, patch),
            )
        }
    }

    /** The prefilled pull request for a change of [path] in the cluster repository [repoURL], naming the active cluster. No network. */
    suspend fun repoProposalDefaults(repoURL: String, path: String): GitProposalDefaults {
        val cluster = go.configs.config.value?.activeSummary?.name.orEmpty()
        return withContext(Dispatchers.Default) { decodeGitProposalDefaults(Ichorgo.gitForgeRepoProposalDefaults(repoURL, path, cluster)) }
    }

    /** The forge argument for [src]'s host, with its saved token; [GitHostMissing] without one. */
    private suspend fun forgeHost(src: GitSource): String = forgeHost(src.host)

    /** The forge argument for [host], with its saved token; [GitHostMissing] without one. */
    private suspend fun forgeHost(host: String): String {
        val store = checkNotNull(gitHosts) { "No Git host store" }
        val entry = store.hostFor(host)
        val json = store.hostJson(entry)
        if (gitHostJsonLacksToken(json)) throw GitHostMissing(host)
        return json
    }

    /**
     * StartArgoSyncFollow: follows the sync of the Application [namespace]/[name] the phone just
     * started (os:admin, reads only); returns at once, the follow reports to [onProgress] and
     * [onDone] (once). See [GitOpsOperationManager].
     */
    fun startArgoSyncFollow(namespace: String, name: String, onProgress: (String) -> Unit, onDone: (String, String) -> Unit): ArgoSyncHandle {
        val t = go.kubeTarget()
        val run = Ichorgo.startArgoSyncFollow(
            t.yaml,
            t.context,
            t.server,
            namespace,
            name,
            object : ArgoSyncListener {
                override fun onProgress(json: String) = onProgress(json)
                override fun onDone(result: String, errMessage: String) = onDone(result, errMessage)
            },
        )
        return ArgoSyncHandle(cancel = run::cancel, terminate = run::terminate)
    }

    /**
     * StartFluxReconcileFollow: follows the reconcile of the Flux object [kind] [namespace]/[name]
     * the phone just requested (os:admin, reads only), for 10 minutes at most; returns at once.
     */
    fun startFluxReconcileFollow(
        kind: String,
        namespace: String,
        name: String,
        onProgress: (String) -> Unit,
        onDone: (String, String) -> Unit,
    ): FluxReconcileHandle {
        val t = go.kubeTarget()
        val run = Ichorgo.startFluxReconcileFollow(
            t.yaml,
            t.context,
            t.server,
            kind,
            namespace,
            name,
            object : FluxReconcileListener {
                override fun onProgress(json: String) = onProgress(json)
                override fun onDone(result: String, errMessage: String) = onDone(result, errMessage)
            },
        )
        return FluxReconcileHandle { run.cancel() }
    }
}

/** The answer of `GitForgeCheck`: login, host, scopesOK, missing. */
internal fun decodeGitHostCheck(json: String): GitHostCheck = TalosJson.decodeFromString(GitHostCheck.serializer(), json)

/** The answer of `GitOpsSource`. */
internal fun decodeGitOpsSource(json: String): GitOpsSource = TalosJson.decodeFromString(GitOpsSource.serializer(), json)

/** The answer of `GitForgeTree` and `FluxSourceTree`. */
internal fun decodeGitTree(json: String): GitTree = TalosJson.decodeFromString(GitTree.serializer(), json)

/** The answer of `GitRepoSearch` and `FluxSourceSearch`. */
internal fun decodeGitRepoSearch(json: String): GitRepoSearch = TalosJson.decodeFromString(GitRepoSearch.serializer(), json)

/** The answer of `GitForgeFile` and `FluxSourceFile`. */
internal fun decodeGitFile(json: String): GitFile = TalosJson.decodeFromString(GitFile.serializer(), json)

/** The answer of `GitOpsSourceFind`. */
internal fun decodeGitOpsSourceFind(json: String): GitOpsSourceFind = TalosJson.decodeFromString(GitOpsSourceFind.serializer(), json)

/** The answer of `GitForgePropose`. */
internal fun decodeGitProposalResult(json: String): GitProposalResult = TalosJson.decodeFromString(GitProposalResult.serializer(), json)

/** The answer of `GitOpsProposalDefaults`. */
internal fun decodeGitProposalDefaults(json: String): GitProposalDefaults = TalosJson.decodeFromString(GitProposalDefaults.serializer(), json)

/** No token is saved for the Git host [host]:the source browser offers to add one in Settings. */
class GitHostMissing(val host: String) : LocalizedException(UiText.Res(R.string.gitops_source_no_token, host))
