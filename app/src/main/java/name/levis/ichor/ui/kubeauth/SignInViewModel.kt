package name.levis.ichor.ui.kubeauth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.KubeAuthRepository
import name.levis.ichor.data.SignInEvent
import name.levis.ichor.model.KubeSignInInfo
import name.levis.ichor.model.SignInPrompt
import name.levis.ichor.model.credentialsFor
import name.levis.ichor.model.signInNeeded
import name.levis.ichor.ui.userMessage

/** Where signing a kubeconfig cluster in stands. */
sealed interface SignInUi {
    data object Loading : SignInUi

    /** The method, before (or after a failed) sign-in: [error] says why the last attempt failed. */
    data class Ready(val info: KubeSignInInfo, val error: String? = null, val checking: Boolean = false) : SignInUi

    /** A browser or device-code sign-in runs; [prompt] is null until the core asked for something. */
    data class Waiting(val info: KubeSignInInfo, val prompt: SignInPrompt? = null) : SignInUi

    data object Done : SignInUi

    /** The method could not be read (the cluster is gone, the core refused it). */
    data class Failed(val message: String) : SignInUi
}

/**
 * Signs the kubeconfig cluster [context] in: with credentials the user enters, or in the
 * browser / with a device code. Only from a screen the user opened, never in the background.
 */
class SignInViewModel(private val repo: KubeAuthRepository, private val context: String) : ViewModel() {
    private val _state = MutableStateFlow<SignInUi>(SignInUi.Loading)
    val state: StateFlow<SignInUi> = _state.asStateFlow()
    private var run: Job? = null

    /** Reads how the cluster signs in, dropping a previous attempt. */
    fun load() {
        cancel()
        _state.value = SignInUi.Loading
        viewModelScope.launch {
            _state.value = try {
                repo.info(context)?.let { SignInUi.Ready(it) } ?: SignInUi.Done
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                SignInUi.Failed(e.userMessage())
            }
        }
    }

    /**
     * Signs in with the [values] of [fields]. A method that goes on in the browser (EKS with
     * IAM Identity Center) answers "sign-in required" once they are stored: its sign-in starts.
     */
    fun submit(fields: List<String>, values: Map<String, String>) {
        val ready = _state.value as? SignInUi.Ready ?: return
        _state.value = ready.copy(checking = true, error = null)
        viewModelScope.launch {
            _state.value = try {
                repo.setCredentials(context, credentialsFor(fields, values))
                SignInUi.Done
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (signInNeeded(e.message) != null) {
                    startInteractive(ready.info)
                    return@launch
                }
                ready.copy(checking = false, error = e.userMessage())
            }
        }
    }

    /** Starts the browser or device-code sign-in. */
    fun startSignIn() {
        val info = (_state.value as? SignInUi.Ready)?.info ?: return
        startInteractive(info)
    }

    private fun startInteractive(info: KubeSignInInfo) {
        run?.cancel()
        _state.value = SignInUi.Waiting(info)
        run = viewModelScope.launch {
            try {
                repo.signIn(context).collect { event ->
                    _state.value = when (event) {
                        is SignInEvent.Prompt -> SignInUi.Waiting(info, event.prompt)
                        is SignInEvent.Done -> event.error?.let { SignInUi.Ready(info, error = it) } ?: SignInUi.Done
                    }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.value = SignInUi.Ready(info, error = e.userMessage())
            }
        }
    }

    /** The sheet closed: stops a sign-in in progress and forgets how the last one ended. */
    fun close() {
        run?.cancel()
        run = null
        _state.value = SignInUi.Loading
    }

    /** Stops waiting for the browser or the device code; the method shows again. */
    fun cancel() {
        run?.cancel()
        run = null
        (_state.value as? SignInUi.Waiting)?.let { _state.value = SignInUi.Ready(it.info) }
    }
}
