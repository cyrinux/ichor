package name.levis.ichor.data

import name.levis.ichor.model.VirtualMachines
import name.levis.ichor.model.VmAction
import name.levis.ichor.model.VmActionResult
import name.levis.ichorgo.Ichorgo

/** KubeVirt (and Harvester) virtual machines and the actions on them, through the Kubernetes API (os:admin). */
class VirtualMachineRepository(go: GoCall) : GoRepository(go) {
    /** The VMs of [namespace], every namespace when "". */
    suspend fun virtualMachines(namespace: String = ""): VirtualMachines = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VirtualMachines.serializer(), Ichorgo.kubeVirtualMachines(cfg, ctx, server, namespace))
    }

    /** Runs [action] on [namespace]/[name], like virtctl; the Go core refuses what the state forbids. */
    suspend fun vmAction(namespace: String, name: String, action: VmAction): VmActionResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VmActionResult.serializer(), Ichorgo.kubeVMAction(cfg, ctx, server, namespace, name, action.wire))
    }

    /** Stops [namespace]/[name] with [graceSeconds] (0: at once, no grace period). */
    suspend fun vmStopForce(namespace: String, name: String, graceSeconds: Int): VmActionResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(VmActionResult.serializer(), Ichorgo.kubeVMStop(cfg, ctx, server, namespace, name, graceSeconds.toLong()))
    }
}
