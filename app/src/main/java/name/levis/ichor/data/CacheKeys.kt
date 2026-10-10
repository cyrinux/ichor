package name.levis.ichor.data

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.KubeSpanOverview
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.NodeResources
import name.levis.ichor.model.ServiceInfo
import name.levis.ichor.model.ImageInfo
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeHardware
import name.levis.ichor.model.NodeNetwork
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer

// The keys TalosRepository caches results under, and which of them may be kept on disk.

const val OVERVIEW = "overview"
const val ETCD = "etcd"
const val KUBESPAN = "kubespan"
const val TOPOLOGY = "topology"
const val INVENTORY = "inventory"
const val WORKLOADS = "workloads"
const val PODS = "pods"
const val CRON_JOBS = "cronjobs"
const val NAMESPACES = "namespaces"

/** The home of a cluster added from a kubeconfig (its nodes, from the Kubernetes API). */
const val KUBE_NODES = "kubenodes"

/**
 * Keys of a Kubernetes list loaded page by page for [namespace] (null: every one), kept like
 * [PODS], [WORKLOADS] and [CRON_JOBS] (same prefix, same model).
 */
fun podsKey(namespace: String?) = "$PODS|${namespace ?: "*"}"
fun workloadsKey(namespace: String?) = "$WORKLOADS|${namespace ?: "*"}"
fun cronJobsKey(namespace: String?) = "$CRON_JOBS|${namespace ?: "*"}"
const val DATA_SERVICES = "dataservices"
const val ARGO_CD = "argocd"
const val FLUX = "flux"
fun servicesKey(node: String) = "services|$node"
fun resourcesKey(node: String) = "resources|$node"
const val CLUSTER_TIME = "clustertime"
fun networkKey(node: String) = "network|$node"
fun hardwareKey(node: String) = "hardware|$node"
fun sensorsKey(node: String) = "sensors|$node"
fun imagesKey(node: String) = "images|$node"

/**
 * The results [TalosRepository] may keep on disk, by key prefix, when "Keep last known state"
 * is on: what describes the cluster. Never live figures (stats, processes, connections, time,
 * logs) nor anything that may hold secrets (machine config, resources, kubeconfig).
 */
internal val PERSISTED: Map<String, KSerializer<*>> = mapOf(
    OVERVIEW to ClusterOverview.serializer(),
    KUBE_NODES to KubeNodesOverview.serializer(),
    ETCD to EtcdOverview.serializer(),
    KUBESPAN to KubeSpanOverview.serializer(),
    TOPOLOGY to ClusterTopology.serializer(),
    INVENTORY to Inventory.serializer(),
    WORKLOADS to ListSerializer(KubeWorkload.serializer()),
    PODS to ListSerializer(KubePod.serializer()),
    CRON_JOBS to ListSerializer(KubeCronJob.serializer()),
    "services" to ListSerializer(ServiceInfo.serializer()),
    "resources" to NodeResources.serializer(),
    "network" to NodeNetwork.serializer(),
    "hardware" to NodeHardware.serializer(),
    "images" to ListSerializer(ImageInfo.serializer()),
)

internal const val FEATURES_PREFIX = "|features|"
fun featuresKey(node: String) = "features|$node"
fun resourceTypesKey(node: String) = "resourcetypes|$node"
