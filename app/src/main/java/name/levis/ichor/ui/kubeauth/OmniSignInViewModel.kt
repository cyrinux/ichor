package name.levis.ichor.ui.kubeauth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.data.OmniAuthRepository
import name.levis.ichor.data.SignInEvent
import name.levis.ichor.model.OmniSignInInfo
import name.levis.ichor.model.SignInPrompt
import name.levis.ichor.ui.userMessage

/** Where signing an Omni cluster in stands. */
sealed interface OmniSignInUi {
    data object Loading : OmniSignInUi

    /** How it is signed in now; [error] says why the last attempt failed, [checking] while a key is checked. */
    data class Ready(val info: OmniSignInInfo, val error: String? = null, val checking: Boolean = false) : OmniSignInUi

    /** A browser sign-in runs; [prompt] is null until the core gave the page to open. */
    data class Waiting(val info: OmniSignInInfo, val prompt: SignInPrompt? = null) : OmniSignInUi

    data object Done : OmniSignInUi

    /** The cluster's sign-in could not be read. */
    data class Failed(val message: String) : OmniSignInUi
}

/**
 * Signs the Omni cluster [context] in: with a service account key the user pastes, or in the
 * browser. Signing out also ends here ([Done]), as the cluster screens must load again.
 */
class OmniSignInViewModel(
    private val repo: OmniAuthRepository,
    private val context: String,
    /** A browser sign-in worked: the user is still in the browser, bring the app back. */
    private val onBrowserDone: () -> Unit = {},
) : ViewModel() {
    private val _state = MutableStateFlow<OmniSignInUi>(OmniSignInUi.Loading)
    val state: StateFlow<OmniSignInUi> = _state.asStateFlow()
    private var run: Job? = null

    fun load() {
        cancel()
        _state.value = OmniSignInUi.Loading
        viewModelScope.launch {
            _state.value = try {
                OmniSignInUi.Ready(repo.info(context))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                OmniSignInUi.Failed(e.userMessage())
            }
        }
    }

    /** Stores the service account [key]; the core checks it is one, and not expired. */
    fun submitKey(key: String) = whileChecking { repo.setServiceAccount(context, key) }

    fun signOut() = whileChecking { repo.signOut(context) }

    private fun whileChecking(action: suspend () -> Unit) {
        val ready = _state.value as? OmniSignInUi.Ready ?: return
        if (ready.checking) return
        _state.value = ready.copy(checking = true, error = null)
        viewModelScope.launch {
            _state.value = try {
                action()
                OmniSignInUi.Done
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                ready.copy(checking = false, error = e.userMessage())
            }
        }
    }

    /** Starts the browser sign-in: a new key Omni registers once the user approves it. */
    fun startBrowser() {
        val info = (_state.value as? OmniSignInUi.Ready)?.info ?: return
        run?.cancel()
        _state.value = OmniSignInUi.Waiting(info)
        run = viewModelScope.launch {
            try {
                repo.signIn(context).collect { event ->
                    _state.value = when (event) {
                        is SignInEvent.Prompt -> OmniSignInUi.Waiting(info, event.prompt)
                        is SignInEvent.Done -> event.error?.let { OmniSignInUi.Ready(info, error = it) } ?: OmniSignInUi.Done.also { onBrowserDone() }
                    }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.value = OmniSignInUi.Ready(info, error = e.userMessage())
            }
        }
    }

    /** The sheet closed: stops a sign-in in progress. */
    fun close() {
        run?.cancel()
        run = null
        _state.value = OmniSignInUi.Loading
    }

    /** Stops waiting for the browser. */
    fun cancel() {
        run?.cancel()
        run = null
        (_state.value as? OmniSignInUi.Waiting)?.let { _state.value = OmniSignInUi.Ready(it.info) }
    }
}
