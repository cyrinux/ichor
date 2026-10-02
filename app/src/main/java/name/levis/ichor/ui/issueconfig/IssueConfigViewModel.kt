package name.levis.ichor.ui.issueconfig

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.CertValidity
import name.levis.ichor.model.DEFAULT_SHARED_ROLES
import name.levis.ichor.model.IssueMode
import name.levis.ichor.model.fitsInQr
import name.levis.ichor.model.rolesArgument
import name.levis.ichor.model.toggled
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText

/** What the user chose; [renewRoles] is only used when the current roles cannot be reused. */
data class IssueForm(
    val mode: IssueMode = IssueMode.RENEW,
    val renewRoles: List<String> = emptyList(),
    val sharedRoles: List<String> = DEFAULT_SHARED_ROLES,
    val validity: CertValidity = CertValidity.DEFAULT,
)

/** One issuance, fixed when the user confirmed it. */
data class IssueRequest(val mode: IssueMode, val context: String, val roles: List<String>, val validity: CertValidity)

sealed interface IssueState {
    data object Editing : IssueState
    data class Issuing(val request: IssueRequest) : IssueState
    data class Failed(val request: IssueRequest, val message: UiText) : IssueState

    /** [certNotAfter]: the stored certificate's new expiry (epoch seconds). */
    data class Renewed(val request: IssueRequest, val certNotAfter: Long) : IssueState

    /**
     * A config for another device. [yaml] holds a private key: it only lives here, in memory,
     * until the user leaves the screen (never saved state, cache, disk or logs).
     */
    class Issued(val request: IssueRequest, val yaml: String, val expiresAt: Long) : IssueState {
        val fitsInQr: Boolean get() = fitsInQr(yaml)
        val size: Int get() = yaml.encodeToByteArray().size
        override fun toString() = "Issued(${request.context}, ${request.roles})" // never the YAML
    }
}

class IssueConfigViewModel(private val talos: TalosRepository, private val configs: ConfigRepository) : ViewModel() {
    private val _form = MutableStateFlow(IssueForm())
    val form: StateFlow<IssueForm> = _form.asStateFlow()

    private val _state = MutableStateFlow<IssueState>(IssueState.Editing)
    val state: StateFlow<IssueState> = _state.asStateFlow()

    fun setMode(mode: IssueMode) = _form.update { it.copy(mode = mode) }
    fun setValidity(validity: CertValidity) = _form.update { it.copy(validity = validity) }
    fun toggleSharedRole(role: String) = _form.update { it.copy(sharedRoles = it.sharedRoles.toggled(role)) }
    fun toggleRenewRole(role: String) = _form.update { it.copy(renewRoles = it.renewRoles.toggled(role)) }

    fun issue(request: IssueRequest) {
        if (_state.value is IssueState.Issuing || request.roles.isEmpty()) return
        _state.value = IssueState.Issuing(request)
        viewModelScope.launch {
            _state.value = try {
                val yaml = talos.generateTalosconfig(rolesArgument(request.roles), request.validity.hours)
                when (request.mode) {
                    IssueMode.RENEW -> {
                        configs.replaceCredentials(request.context, yaml)
                        IssueState.Renewed(request, configs.config.value?.activeSummary?.certNotAfter ?: 0)
                    }
                    IssueMode.OTHER_DEVICE -> IssueState.Issued(request, yaml, request.validity.expiresAt(System.currentTimeMillis()))
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                IssueState.Failed(request, e.uiText())
            }
        }
    }

    /** Drops any issued config (and its key) and goes back to the form. */
    fun clear() {
        _state.value = IssueState.Editing
    }

    override fun onCleared() {
        clear()
    }
}
