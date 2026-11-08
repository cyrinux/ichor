package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.model.ARGO_SYNC_ERROR
import name.levis.ichor.model.ARGO_SYNC_FAILED
import name.levis.ichor.model.ArgoSyncProgress
import name.levis.ichor.model.FLUX_FOLLOW_FAILED
import name.levis.ichor.model.FluxReconcileProgress
import name.levis.ichor.ui.FollowedRun
import name.levis.ichor.ui.goErrorText

/** The Argo CD sync the app follows after starting it: at most one at a time. */
data class ArgoSyncRunState(
    val namespace: String,
    val name: String,
    val progress: ArgoSyncProgress? = null,
    /** The operation's final phase (Succeeded, Terminated...); "" while it runs or once no longer followed. */
    val result: String = "",
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Terminate was asked: Argo CD stops applying resources. */
    val terminating: Boolean = false,
    /** Why the last Terminate was refused, while the sync goes on. */
    val terminateError: String? = null,
) : FollowedRun {
    override val hostname: String get() = name
}

/** The Flux reconcile the app follows after requesting it: at most one at a time. */
data class FluxReconcileRunState(
    val kind: String,
    val namespace: String,
    val name: String,
    val progress: FluxReconcileProgress? = null,
    /** ready, failed or "still reconciling"; "" while followed or once no longer followed. */
    val result: String = "",
    override val finished: Boolean = false,
    override val error: String? = null,
    /** The user stopped following it: the reconcile goes on in the cluster. */
    val stopped: Boolean = false,
) : FollowedRun {
    override val hostname: String get() = name
}

/** A followed sync: [cancel] stops following, [terminate] stops the sync (throws when refused). */
class ArgoSyncHandle(val cancel: () -> Unit, val terminate: () -> Unit)

/** A followed reconcile, which can stop being followed. */
fun interface FluxReconcileHandle {
    fun cancel()
}

/** Starts following in the Go core; the callbacks get its JSON progress and its result. A fake in the tests. */
typealias ArgoSyncStarter = (namespace: String, name: String, onProgress: (String) -> Unit, onDone: (String, String) -> Unit) -> ArgoSyncHandle

typealias FluxReconcileStarter =
    (kind: String, namespace: String, name: String, onProgress: (String) -> Unit, onDone: (String, String) -> Unit) -> FluxReconcileHandle

/**
 * The Argo CD syncs and Flux reconciles started from the phone, followed app-wide like
 * [K8sUpgradeManager] until they end: leaving the screen keeps them followed, and a
 * foreground service keeps the app alive meanwhile. One of each kind at a time.
 * [onArgoStarted] / [onFluxStarted] run once one is followed. [io] runs Terminate.
 */
class GitOpsOperationManager(
    private val startArgo: ArgoSyncStarter,
    private val startFlux: FluxReconcileStarter,
    private val onArgoStarted: () -> Unit = {},
    private val onFluxStarted: () -> Unit = {},
    private val io: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
) {
    private val _argoSync = MutableStateFlow<ArgoSyncRunState?>(null)
    val argoSync: StateFlow<ArgoSyncRunState?> = _argoSync.asStateFlow()
    private val _fluxReconcile = MutableStateFlow<FluxReconcileRunState?>(null)
    val fluxReconcile: StateFlow<FluxReconcileRunState?> = _fluxReconcile.asStateFlow()
    private var argoHandle: ArgoSyncHandle? = null
    private var fluxHandle: FluxReconcileHandle? = null

    // Each run's number: a late callback of an earlier run changes nothing.
    @Volatile private var argoRun = 0L
    @Volatile private var fluxRun = 0L

    /** Follows the sync of the Application [namespace]/[name] just started, unless one is followed; returns false then. */
    @Synchronized
    fun followArgoSync(namespace: String, name: String): Boolean {
        if (_argoSync.value?.running == true) return false
        val run = ++argoRun
        _argoSync.value = ArgoSyncRunState(namespace, name)
        argoHandle = try {
            startArgo(namespace, name, { onArgoProgress(run, it) }, { result, err -> onArgoDone(run, result, err) })
        } catch (e: Exception) {
            _argoSync.value = null
            throw e
        }
        onArgoStarted()
        return true
    }

    private fun onArgoProgress(run: Long, json: String) {
        val p = runCatching { TalosJson.decodeFromString(ArgoSyncProgress.serializer(), json) }.getOrNull() ?: return
        _argoSync.update { if (run == argoRun && it?.running == true) it.copy(progress = p) else it }
    }

    private fun onArgoDone(run: Long, result: String, errMessage: String) {
        _argoSync.update {
            if (run != argoRun || it == null || !it.running) return@update it
            // A failed sync is a failure of the run: Argo CD's message says why.
            val failed = result == ARGO_SYNC_FAILED || result == ARGO_SYNC_ERROR
            val error = errMessage.ifEmpty { null }?.let(::goErrorText)
                ?: it.progress?.message?.ifEmpty { null }?.takeIf { failed }
                ?: result.takeIf { failed }
            it.copy(result = result, finished = true, error = error, terminating = false)
        }
    }

    /** Stops the followed sync: Argo CD stops applying resources, the follow ends as Terminated. */
    @Synchronized
    fun terminate() {
        if (_argoSync.value?.let { it.running && !it.terminating } != true) return
        val run = argoRun
        val handle = argoHandle ?: return
        _argoSync.update { it?.copy(terminating = true, terminateError = null) }
        io.launch {
            val failure = runCatching { handle.terminate() }.exceptionOrNull() ?: return@launch
            val why = failure.message?.let(::goErrorText) ?: failure.toString()
            _argoSync.update { if (run == argoRun && it?.running == true) it.copy(terminating = false, terminateError = why) else it }
        }
    }

    /** Forgets a finished sync. */
    @Synchronized
    fun dismissArgo() {
        if (_argoSync.value?.finished == true) {
            argoHandle = null
            _argoSync.value = null
        }
    }

    /** Follows the reconcile of the Flux object [kind] [namespace]/[name] just requested, unless one is followed; returns false then. */
    @Synchronized
    fun followFluxReconcile(kind: String, namespace: String, name: String): Boolean {
        if (_fluxReconcile.value?.running == true) return false
        val run = ++fluxRun
        _fluxReconcile.value = FluxReconcileRunState(kind, namespace, name)
        fluxHandle = try {
            startFlux(kind, namespace, name, { onFluxProgress(run, it) }, { result, err -> onFluxDone(run, result, err) })
        } catch (e: Exception) {
            _fluxReconcile.value = null
            throw e
        }
        onFluxStarted()
        return true
    }

    private fun onFluxProgress(run: Long, json: String) {
        val p = runCatching { TalosJson.decodeFromString(FluxReconcileProgress.serializer(), json) }.getOrNull() ?: return
        _fluxReconcile.update { if (run == fluxRun && it?.running == true) it.copy(progress = p) else it }
    }

    private fun onFluxDone(run: Long, result: String, errMessage: String) {
        _fluxReconcile.update {
            if (run != fluxRun || it == null || !it.running) return@update it
            val failed = result == FLUX_FOLLOW_FAILED
            val error = errMessage.ifEmpty { null }?.let(::goErrorText)
                ?: it.progress?.message?.ifEmpty { null }?.takeIf { failed }
                ?: result.takeIf { failed }
            it.copy(result = result, finished = true, error = error)
        }
    }

    /** Stops following the reconcile: it goes on in the cluster. */
    @Synchronized
    fun stopFollowingFlux() {
        if (_fluxReconcile.value?.running != true) return
        fluxHandle?.cancel()
        _fluxReconcile.update { it?.copy(finished = true, stopped = true) }
    }

    /** Forgets a finished reconcile. */
    @Synchronized
    fun dismissFlux() {
        if (_fluxReconcile.value?.finished == true) {
            fluxHandle = null
            _fluxReconcile.value = null
        }
    }
}
