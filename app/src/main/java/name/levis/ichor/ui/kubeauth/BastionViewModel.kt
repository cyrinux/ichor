package name.levis.ichor.ui.kubeauth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.KubeBastionRepository
import name.levis.ichor.model.BastionFields
import name.levis.ichor.model.BastionProblem
import name.levis.ichor.model.KubeBastionHostKey
import name.levis.ichor.model.KubeBastionInfo
import name.levis.ichor.model.bastionProblem
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** What the bastion sheet shows. Secrets live here only while the sheet is open (see [BastionViewModel.clear]). */
data class BastionUiState(
    val loading: Boolean = true,
    val info: KubeBastionInfo? = null,
    val host: String = "",
    val user: String = "",
    val useKey: Boolean = true,
    val privateKey: String = "",
    val passphrase: String = "",
    val password: String = "",
    /** The key the bastion presented, once checked: shown for the user to compare, then pinned. */
    val checked: KubeBastionHostKey? = null,
    /** The pinned key no longer matches: the sheet offers to replace it. */
    val pinMismatch: Boolean = false,
    val busy: Boolean = false,
    val error: UiText? = null,
    /** Saved or removed: the sheet closes and the cluster reloads. */
    val done: Boolean = false,
) {
    val configured: Boolean get() = info?.configured == true

    /** A host other than the configured one is a new bastion: its key is checked and trusted first. */
    val needsCheck: Boolean get() = !configured || hostChanged

    private val hostChanged: Boolean
        get() = info?.host.orEmpty().let { stored -> host.trim().isNotEmpty() && host.trim() != stored && "${host.trim()}:22" != stored }

    val canCheck: Boolean get() = host.isNotBlank() && !busy

    /** Host and user given, a credential typed (or kept), the key checked when it must be. */
    val canSave: Boolean
        get() {
            val credential = if (useKey) privateKey.isNotBlank() else password.isNotEmpty()
            return !busy && host.isNotBlank() && user.isNotBlank() &&
                (credential || configured) && (!needsCheck || checked != null)
        }
}

class BastionViewModel(
    private val bastions: KubeBastionRepository,
    private val context: String,
    /** The problem the screen that opened the sheet hit, if any: HOST_KEY opens on the pin replacement. */
    problem: BastionProblem?,
) : ViewModel() {
    private val _state = MutableStateFlow(BastionUiState(pinMismatch = problem == BastionProblem.HOST_KEY))
    val state: StateFlow<BastionUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            val info = runCatching { bastions.info(context) }
            _state.update { s ->
                info.fold(
                    onSuccess = { s.copy(loading = false, info = it, host = it.host, user = it.user, useKey = it.auth != KubeBastionInfo.AUTH_PASSWORD) },
                    onFailure = { s.copy(loading = false, error = it.uiText()) },
                )
            }
        }
    }

    fun host(value: String) = _state.update { it.copy(host = value, checked = null, error = null) }
    fun user(value: String) = _state.update { it.copy(user = value, error = null) }
    fun useKey(value: Boolean) = _state.update { it.copy(useKey = value, error = null) }
    fun privateKey(value: String) = _state.update { it.copy(privateKey = value, error = null) }
    fun passphrase(value: String) = _state.update { it.copy(passphrase = value) }
    fun password(value: String) = _state.update { it.copy(password = value, error = null) }

    /** Reads the host key the bastion presents (the configured host when replacing the pin). */
    fun checkHostKey() = run { s ->
        val key = bastions.hostKey(s.host.ifBlank { s.info?.host.orEmpty() })
        _state.update { it.copy(checked = key) }
    }

    /** Sets the bastion: the checked key pinned when there is one, empty secrets kept. */
    fun save() = run { s ->
        val fields = BastionFields(
            host = s.host,
            user = s.user,
            privateKey = if (s.useKey) s.privateKey.trim() else "",
            passphrase = if (s.useKey) s.passphrase else "",
            password = if (s.useKey) "" else s.password,
            hostKey = s.checked?.hostKey.orEmpty(),
        )
        bastions.set(context, fields)
        _state.update { it.copy(done = true) }
    }

    /** Pins the key just checked in place of the old one, nothing else changed. */
    fun replacePin() = run { s ->
        val key = s.checked ?: return@run
        bastions.set(context, BastionFields(hostKey = key.hostKey))
        _state.update { it.copy(done = true) }
    }

    fun remove() = run {
        bastions.remove(context)
        _state.update { it.copy(done = true) }
    }

    /** Forgets every secret typed, when the sheet closes. */
    fun clear() = _state.update { it.copy(privateKey = "", passphrase = "", password = "") }

    override fun onCleared() = clear()

    private fun run(block: suspend (BastionUiState) -> Unit) {
        val s = _state.value
        if (s.busy) return
        _state.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            try {
                block(s)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(error = e.uiText(), pinMismatch = it.pinMismatch || bastionProblem(e.message) == BastionProblem.HOST_KEY) }
            } finally {
                _state.update { it.copy(busy = false) }
            }
        }
    }
}
