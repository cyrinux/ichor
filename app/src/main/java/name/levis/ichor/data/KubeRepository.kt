package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.KubeChange
import name.levis.ichor.model.ApiHealthReport
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.KubeTopNodes
import name.levis.ichor.model.KubeTopPods
import name.levis.ichor.model.IntegrationReport
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.KUBE_PAGE_SIZE
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.KubeCronJobList
import name.levis.ichor.model.KubeCronJobPage
import name.levis.ichor.model.KubeEvent
import name.levis.ichor.model.KubeEventList
import name.levis.ichor.model.KubeNamespaces
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubePodPage
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeRevisionList
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.KubeRouteList
import name.levis.ichor.model.KubeWatchEvent
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.KubeWorkloadList
import name.levis.ichor.model.KubeWorkloadPage
import name.levis.ichor.model.PodPhaseFilter
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.PromDiscovery
import name.levis.ichor.model.PromOperatorStatus
import name.levis.ichor.model.PromRules
import name.levis.ichor.model.PromTargets
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromResult
import name.levis.ichor.model.PromSource
import name.levis.ichor.model.RoutePod
import name.levis.ichor.model.SELECTED_PODS_PAGE
import name.levis.ichor.model.SupportedIntegrations
import name.levis.ichor.model.WorkloadRef
import name.levis.ichor.model.toGoJson
import name.levis.ichorgo.Ichorgo

/**
 * The Kubernetes side of the active cluster, Talos or kubeconfig alike: workloads, pods,
 * CronJobs, routes, events, the API server's health, the checkup and Prometheus. Results
 * worth showing again at once go to the cache shared with [TalosRepository].
 */
class KubeRepository(go: GoCall) : GoRepository(go) {
    /** The cluster's nodes as Kubernetes lists them: the home of a cluster added from a kubeconfig. */
    suspend fun kubeNodes(): KubeNodesOverview = go.remember(KUBE_NODES) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(KubeNodesOverview.serializer(), Ichorgo.kubeNodes(cfg, ctx, server)) }
    }

    /** CPU and memory each node uses (metrics-server); not cached: usage is live. */
    suspend fun topNodes(): KubeTopNodes = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeTopNodes.serializer(), Ichorgo.kubeTopNodes(cfg, ctx, server))
    }

    /** CPU and memory one pod uses, with its requests and limits: two small reads. */
    suspend fun topPod(namespace: String, name: String): KubeTopPods = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeTopPods.serializer(), Ichorgo.kubeTopPod(cfg, ctx, server, namespace, name))
    }

    /**
     * CPU and memory the pods of [namespace] (null: all) use; with their requests and limits for
     * one namespace only (see [KubeTopPods.boundsRead]).
     */
    suspend fun topPods(namespace: String?, selector: String = ""): KubeTopPods = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeTopPods.serializer(), Ichorgo.kubeTopPods(cfg, ctx, server, namespace.orEmpty(), selector))
    }

    /**
     * The Deployments, StatefulSets and DaemonSets running [pods] (an app's), through their
     * owners (os:admin): only those pods and owners are read, never a cluster-wide list.
     */
    suspend fun appWorkloads(pods: List<RoutePod>): List<KubeWorkload> = go.kube { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)
        TalosJson.decodeFromString(KubeWorkloadList.serializer(), Ichorgo.kubeAppWorkloads(cfg, ctx, server, json)).workloads
    }

    /** [workloads] as they are now (os:admin); one deleted since is left out. */
    suspend fun workloadsNamed(workloads: List<WorkloadRef>): List<KubeWorkload> = go.kube { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(WorkloadRef.serializer()), workloads)
        TalosJson.decodeFromString(KubeWorkloadList.serializer(), Ichorgo.kubeWorkloadsNamed(cfg, ctx, server, json)).workloads
    }

    /** `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin). */
    suspend fun rolloutRestart(workload: KubeWorkload) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeRolloutRestart(cfg, ctx, server, workload.kind, workload.namespace, workload.name)
    }

    /** `kubectl rollout status KIND/NAME -n NAMESPACE` with the pods (os:admin). Never cached: polled. */
    suspend fun rolloutStatus(workload: KubeWorkload): KubeRolloutStatus = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeRolloutStatus.serializer(), Ichorgo.kubeRolloutStatus(cfg, ctx, server, workload.kind, workload.namespace, workload.name))
    }

    /**
     * [rolloutStatus] kept live (os:admin): the status at the start, then again each time the
     * workload or one of its pods changes, until the collector cancels or the watch ends.
     */
    fun rolloutWatch(workload: KubeWorkload): Flow<StreamItem<KubeRolloutStatus>> =
        kubeLiveFlow(go::kubeTarget, KubeRolloutStatus.serializer()) { cfg, ctx, server, listener ->
            Ichorgo.startKubeRolloutWatch(cfg, ctx, server, workload.kind, workload.namespace, workload.name, listener)
        }

    /**
     * The pods of [selection] narrowed to [phase] kept live (os:admin): the whole list first,
     * then each pod added, changed or gone as the API server reports it, until the collector
     * cancels or the watch ends. Full objects, like the first page of [workloadPodsPage].
     */
    fun workloadPodsWatch(selection: PodSelection.OfWorkload, phase: PodPhaseFilter): Flow<StreamItem<KubeWatchEvent<KubePod>>> =
        kubeWatchFlow(go::kubeTarget, KubePod.serializer(), { TalosJson.decodeFromString(KubePodPage.serializer(), it).pods }) { cfg, ctx, server, listener ->
            Ichorgo.startKubeWorkloadPodsWatch(cfg, ctx, server, selection.kind, selection.namespace, selection.name, phase.query, listener)
        }

    /**
     * The pods of the Kubernetes node [kubeNode] (every namespace) narrowed to [phase] kept live
     * (os:admin), like [workloadPodsWatch]: the whole list first, then each change.
     */
    fun nodePodsWatch(kubeNode: String, phase: PodPhaseFilter): Flow<StreamItem<KubeWatchEvent<KubePod>>> =
        kubeWatchFlow(go::kubeTarget, KubePod.serializer(), { TalosJson.decodeFromString(KubePodPage.serializer(), it).pods }) { cfg, ctx, server, listener ->
            Ichorgo.startKubeNodePodsWatch(cfg, ctx, server, kubeNode, phase.query, listener)
        }

    /**
     * A signal each time one of [kinds] changes in [namespace] (null: every namespace), at most
     * every 2 s and never for the lists read at the start, until the collector cancels or the
     * watch ends: for the lists that cannot be merged row by row, read again on each signal.
     */
    fun changeWatch(namespace: String?, kinds: List<String>): Flow<StreamItem<KubeChange>> =
        kubeLiveFlow(go::kubeTarget, KubeChange.serializer()) { cfg, ctx, server, listener ->
            Ichorgo.startKubeChangeWatch(cfg, ctx, server, namespace.orEmpty(), kinds.joinToString(","), listener)
        }

    /**
     * The events of [namespace] (null: every namespace) kept live, coalesced by object, reason
     * and type (os:admin): the newest rows first, then each change, until the collector
     * cancels or the stream ends. [warningsOnly]: Warning events only.
     */
    fun eventsStream(namespace: String?, warningsOnly: Boolean): Flow<KubeEventsItem> =
        kubeEventsFlow(go::kubeTarget) { cfg, ctx, server, listener ->
            Ichorgo.startKubeEvents(cfg, ctx, server, namespace.orEmpty(), warningsOnly, listener)
        }

    /**
     * `kubectl scale KIND/NAME --replicas=N -n NAMESPACE` (os:admin): a warning ("" when none)
     * when a HorizontalPodAutoscaler manages the replicas and will change them again.
     */
    suspend fun scale(workload: KubeWorkload, replicas: Int): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeScale(cfg, ctx, server, workload.kind, workload.namespace, workload.name, replicas.toLong())
    }

    /** `kubectl rollout history deployment/NAME -n NAMESPACE`, newest first (os:admin). Never cached. */
    suspend fun deploymentRevisions(workload: KubeWorkload): List<KubeRevision> = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeRevisionList.serializer(), Ichorgo.kubeDeploymentRevisions(cfg, ctx, server, workload.namespace, workload.name)).revisions
    }

    /** `kubectl rollout undo deployment/NAME --to-revision=N -n NAMESPACE` (os:admin). */
    suspend fun rollbackDeployment(workload: KubeWorkload, revision: Int) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeRollbackDeployment(cfg, ctx, server, workload.namespace, workload.name, revision.toLong())
    }

    /** CronJobs with their recent runs through the Kubernetes API (os:admin). */
    suspend fun cronJobs(): List<KubeCronJob> = go.remember(CRON_JOBS) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(KubeCronJobList.serializer(), Ichorgo.kubeCronJobs(cfg, ctx, server)).cronJobs }
    }

    /** `kubectl create job --from=cronjob/NAME -n NAMESPACE` (os:admin): the new Job's name. */
    suspend fun triggerCronJob(cronJob: KubeCronJob): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubeTriggerCronJob(cfg, ctx, server, cronJob.namespace, cronJob.name)
    }

    /** Suspends (no new runs) or resumes [cronJob] (os:admin). */
    suspend fun suspendCronJob(cronJob: KubeCronJob, suspend: Boolean) = go.kube { cfg, ctx, server ->
        Ichorgo.kubeSuspendCronJob(cfg, ctx, server, cronJob.namespace, cronJob.name, suspend)
    }

    /** The Ingress and HTTPRoute URLs serving [pods] (os:admin). */
    suspend fun appRoutes(pods: List<RoutePod>): List<KubeRoute> = go.kube { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)
        TalosJson.decodeFromString(KubeRouteList.serializer(), Ichorgo.kubeAppRoutes(cfg, ctx, server, json)).routes
    }

    /**
     * `kubectl logs POD [-c CONTAINER] [--previous] --tail=N` through the Kubernetes API
     * (os:admin): [previous] reads the container's last terminated run. [container] "" for a
     * pod with one container.
     */
    suspend fun podLogs(pod: KubePod, container: String, previous: Boolean, tailLines: Int): String = go.kube { cfg, ctx, server ->
        Ichorgo.kubePodLogs(cfg, ctx, server, pod.namespace, pod.name, container, previous, tailLines.toLong())
    }

    /** The cluster's namespaces, to pick the scope of the Kubernetes lists (os:admin). */
    suspend fun namespaces(): KubeNamespaces = go.remember(NAMESPACES) {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(KubeNamespaces.serializer(), Ichorgo.kubeNamespaces(cfg, ctx, server)) }
    }

    /**
     * One page of the pods of [namespace] (null for every one), in the API server's order
     * (os:admin). [table]: the server's Table rows, without images nor containers ([pod]
     * reads those). [token]: the previous page's, "" for the first.
     */
    suspend fun podsPage(namespace: String?, token: String, table: Boolean, limit: Int = KUBE_PAGE_SIZE): KubePage<KubePod> = go.kube { cfg, ctx, server ->
        val json = Ichorgo.kubePodsPage(cfg, ctx, server, namespace.orEmpty(), token, limit.toLong(), table)
        TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
    }

    /**
     * One page of the pods scheduled on the Kubernetes node [kubeNode] ([kubeNodeName]), in
     * every namespace, narrowed to [phase]; as [podsPage] otherwise (os:admin).
     */
    suspend fun nodePodsPage(kubeNode: String, phase: PodPhaseFilter, token: String, table: Boolean, limit: Int = SELECTED_PODS_PAGE): KubePage<KubePod> =
        go.kube { cfg, ctx, server ->
            val json = Ichorgo.kubeNodePodsPage(cfg, ctx, server, kubeNode, phase.query, token, limit.toLong(), table)
            TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
        }

    /** One page of the pods [workload]'s selector matches, narrowed to [phase]; as [podsPage] otherwise (os:admin). */
    suspend fun workloadPodsPage(workload: PodSelection.OfWorkload, phase: PodPhaseFilter, token: String, table: Boolean, limit: Int = SELECTED_PODS_PAGE): KubePage<KubePod> =
        go.kube { cfg, ctx, server ->
            val json = Ichorgo.kubeWorkloadPodsPage(cfg, ctx, server, workload.kind, workload.namespace, workload.name, phase.query, token, limit.toLong(), table)
            TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
        }

    /** One pod in full (images, containers, last termination), for a row read from a Table (os:admin). */
    suspend fun pod(namespace: String, name: String): KubePod = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubePod.serializer(), Ichorgo.kubePod(cfg, ctx, server, namespace, name))
    }

    /** One page of the workloads of [kind] in [namespace] (null for every one), as [podsPage] (os:admin). */
    suspend fun workloadsPage(kind: String, namespace: String?, token: String, limit: Int = KUBE_PAGE_SIZE): KubePage<KubeWorkload> = go.kube { cfg, ctx, server ->
        val json = Ichorgo.kubeWorkloadsPage(cfg, ctx, server, kind, namespace.orEmpty(), token, limit.toLong())
        TalosJson.decodeFromString(KubeWorkloadPage.serializer(), json).toPage()
    }

    /** One workload as it is now (os:admin): its replicas, for a restart asked outside the Workloads list. */
    suspend fun workload(kind: String, namespace: String, name: String): KubeWorkload = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeWorkload.serializer(), Ichorgo.kubeWorkload(cfg, ctx, server, kind, namespace, name))
    }

    /**
     * The workload from a list already loaded: of its namespace or of every namespace; null
     * when none holds it.
     */
    fun cachedWorkload(kind: String, namespace: String, name: String): KubeWorkload? =
        listOf(workloadsKey(namespace), workloadsKey(null)).firstNotNullOfOrNull { key ->
            cached<List<KubeWorkload>>(key)?.value?.firstOrNull { it.kind == kind && it.namespace == namespace && it.name == name }
        }

    /** One page of the CronJobs of [namespace] (null for every one) with their runs, as [podsPage] (os:admin). */
    suspend fun cronJobsPage(namespace: String?, token: String, limit: Int = KUBE_PAGE_SIZE): KubePage<KubeCronJob> = go.kube { cfg, ctx, server ->
        val json = Ichorgo.kubeCronJobsPage(cfg, ctx, server, namespace.orEmpty(), token, limit.toLong())
        TalosJson.decodeFromString(KubeCronJobPage.serializer(), json).toPage()
    }

    /**
     * The projects the app integrates with and whether the cluster runs each one, from one API
     * discovery (os:admin). [hints]: see [Inventory.supportedIntegrationHints]. Never cached.
     */
    suspend fun supportedIntegrations(hints: String): SupportedIntegrations = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(SupportedIntegrations.serializer(), Ichorgo.kubeSupportedIntegrations(cfg, ctx, server, hints))
    }

    /**
     * The Kubernetes API server's health and what loads it (os:admin): readyz and livez, then
     * two /metrics scrapes a few seconds apart for the live rates. Live figures: never cached.
     */
    suspend fun apiHealth(): ApiHealthReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(ApiHealthReport.serializer(), Ichorgo.kubeAPIHealth(cfg, ctx, server))
    }

    /**
     * The cluster checkup (os:admin): what no other screen shows, section by section. It lists the
     * cluster's pods and asks every kubelet for its volumes: on demand, never cached.
     */
    suspend fun checkup(): CheckupReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(CheckupReport.serializer(), Ichorgo.kubeCheckup(cfg, ctx, server))
    }

    /**
     * The Kubernetes events of the [kind] named [name] in [namespace], newest first (os:admin); with
     * an empty [kind], those of [name] and of what it owns by name (ReplicaSets, pods).
     */
    suspend fun kubeEvents(namespace: String, kind: String, name: String): List<KubeEvent> = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeEventList.serializer(), Ichorgo.kubeEvents(cfg, ctx, server, namespace, kind, name)).events
    }

    /**
     * The API groups the cluster serves that Ichor does not read yet, by operator, with their
     * kinds (os:admin): what an integration request can name. Never cached.
     */
    suspend fun integrations(): IntegrationReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(IntegrationReport.serializer(), Ichorgo.kubeIntegrations(cfg, ctx, server))
    }

    /** `kubectl delete pod NAME -n NAMESPACE` (os:admin): its controller starts a new one. */
    suspend fun deletePod(pod: KubePod) = go.kube { cfg, ctx, server -> Ichorgo.kubeDeletePod(cfg, ctx, server, pod.namespace, pod.name) }

    /** Deletes an `ichor-netperf-*` namespace a network test left behind (os:admin); the core refuses any other. */
    suspend fun deleteNetPerfNamespace(name: String) = go.kube { cfg, ctx, server -> Ichorgo.netPerfDeleteNamespace(cfg, ctx, server, name) }

    /** Prometheus-compatible query APIs among the cluster's Services, the likeliest first. */
    suspend fun promDiscover(): List<PromSource> = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PromDiscovery.serializer(), Ichorgo.promDiscover(cfg, ctx, server)).sources
    }

    /** [query] from [start] to [end] (unix seconds) against [source], about 250 points. */
    suspend fun promRange(source: PromSource, query: String, start: Long, end: Long): PromResult = go.kube { cfg, ctx, server ->
        val json = Ichorgo.promQueryRange(cfg, ctx, server, source.toGoJson(), query, start, end, 0)
        TalosJson.decodeFromString(PromResult.serializer(), json)
    }

    /** The rule groups [source] evaluates, those in trouble first, each naming its PrometheusRule. */
    suspend fun promRules(source: PromSource): PromRules = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PromRules.serializer(), Ichorgo.promRules(cfg, ctx, server, source.toGoJson()))
    }

    /** The scrape pools of [source] with their down targets. */
    suspend fun promTargets(source: PromSource): PromTargets = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PromTargets.serializer(), Ichorgo.promTargets(cfg, ctx, server, source.toGoJson()))
    }

    /** The Prometheus Operator's Prometheus and Alertmanager objects and its monitor counts. */
    suspend fun promOperatorStatus(): PromOperatorStatus = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PromOperatorStatus.serializer(), Ichorgo.promOperatorStatus(cfg, ctx, server))
    }

    /** The built-in panels. */
    suspend fun promPresets(): List<PromPanel> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(PromPanel.serializer()), Ichorgo.promPresets())
    }

    /** [source] checked and cleaned up by Go, its secret kept. */
    suspend fun normalizePromSource(source: PromSource): PromSource = withContext(Dispatchers.IO) {
        val json = Ichorgo.normalizePromSource(source.toGoJson())
        val checked = TalosJson.decodeFromString(PromSource.serializer(), json)
        // Go never returns the secret; without authentication there is none to keep.
        checked.copy(secret = if (checked.auth == PromSource.AUTH_NONE) "" else source.secret.trim())
    }
}
