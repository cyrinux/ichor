package name.levis.ichor.security

import org.junit.Assert.assertEquals
import org.junit.Test

class PanicResponderTest {

    private val trusted = "org.example.panic"
    private val other = "org.example.other"

    private fun request(action: String?, caller: String?, trustedPackage: String? = trusted, configured: PanicResponse = PanicResponse.DELETE_CONFIG) =
        panicRequest(action, caller, trustedPackage, configured)

    @Test
    fun theConnectedTriggerGetsTheConfiguredResponse() {
        assertEquals(PanicRequest.Trigger(PanicResponse.DELETE_CONFIG), request(PanicActions.TRIGGER, trusted))
        assertEquals(PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, trusted, configured = PanicResponse.LOCK))
    }

    @Test
    fun anyOtherSenderCanOnlyLock() {
        assertEquals(PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, other))
        assertEquals("startActivity: no caller to verify", PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, null))
        assertEquals("nothing connected", PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, trusted, trustedPackage = null))
        assertEquals("no caller must not match no connection", PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, null, trustedPackage = null))
        assertEquals("blank is not a connection", PanicRequest.Trigger(PanicResponse.LOCK), request(PanicActions.TRIGGER, "", trustedPackage = ""))
    }

    @Test
    fun connectNeedsACaller() {
        assertEquals(PanicRequest.Connect(other), request(PanicActions.CONNECT, other))
        assertEquals(PanicRequest.Ignore, request(PanicActions.CONNECT, null))
        assertEquals(PanicRequest.Ignore, request(PanicActions.CONNECT, " "))
    }

    @Test
    fun onlyTheConnectedTriggerCanDisconnect() {
        assertEquals(PanicRequest.Disconnect, request(PanicActions.DISCONNECT, trusted))
        assertEquals(PanicRequest.Ignore, request(PanicActions.DISCONNECT, other))
        assertEquals(PanicRequest.Ignore, request(PanicActions.DISCONNECT, null))
        assertEquals(PanicRequest.Ignore, request(PanicActions.DISCONNECT, null, trustedPackage = null))
    }

    @Test
    fun otherActionsAreIgnored() {
        assertEquals(PanicRequest.Ignore, request("android.intent.action.VIEW", trusted))
        assertEquals(PanicRequest.Ignore, request(null, trusted))
    }

    @Test
    fun actionsAreThePanicKitOnes() {
        // The wire protocol: Ripple and every other trigger send exactly these.
        assertEquals("info.guardianproject.panic.action.TRIGGER", PanicActions.TRIGGER)
        assertEquals("info.guardianproject.panic.action.CONNECT", PanicActions.CONNECT)
        assertEquals("info.guardianproject.panic.action.DISCONNECT", PanicActions.DISCONNECT)
    }
}
