package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import name.levis.ichor.model.GitHost
import name.levis.ichor.model.GitHostCheck
import name.levis.ichor.model.GitHostKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GitHostStoreTest {

    private val prefs = MemoryPrefs()
    private val files = HashMap<String, MemoryValue>()
    private var demo = false

    private fun store() = GitHostStore(
        prefs,
        { host -> files.getOrPut(host) { MemoryValue() } },
        CoroutineScope(Dispatchers.Unconfined),
        demo = { demo },
        tokenWrites = Dispatchers.Unconfined,
    )

    @Test
    fun aHostIsListedWithoutItsTokenAndSurvivesARestart() = runBlocking {
        store().set("https://github.com/", GitHostKind.GITHUB, " ghp-test ")

        assertEquals("ghp-test", files.getValue("github.com").value)
        assertFalse(prefs.all.values.any { it.toString().contains("ghp-test") })
        val again = store()
        assertEquals(listOf(GitHost("github.com", GitHostKind.GITHUB)), again.hosts.value)
        assertEquals("ghp-test", again.token("github.com"))
    }

    @Test
    fun aBlankTokenKeepsTheStoredOne() = runBlocking {
        val store = store()
        store.set("gitlab.example.org", GitHostKind.AUTO, "glpat-test")
        store.set("gitlab.example.org", GitHostKind.GITLAB, "  ")

        assertEquals(listOf(GitHost("gitlab.example.org", GitHostKind.GITLAB)), store.hosts.value)
        assertEquals("glpat-test", store.token("gitlab.example.org"))
    }

    @Test
    fun removingDeletesTheTokenAndTheLogin() = runBlocking {
        val store = store()
        store.set("git.example.org", GitHostKind.GITEA, "gitea-test")
        store.rememberLogin("git.example.org", "octo")
        store.remove("https://git.example.org/")

        assertTrue(store.hosts.value.isEmpty())
        assertNull(files.getValue("git.example.org").value)
        assertEquals("", store.token("git.example.org"))
        assertTrue(store.logins.value.isEmpty())
    }

    @Test
    fun theLastLoginIsKeptUntilTheTokenChanges() {
        val store = store()
        store.set("github.com", GitHostKind.AUTO, "one")
        store.rememberLogin("github.com", "octo")
        assertEquals(mapOf("github.com" to "octo"), store().logins.value)

        store.set("github.com", GitHostKind.AUTO, "two")
        assertTrue(store.logins.value.isEmpty())
    }

    @Test
    fun noLoginForAnUnlistedHostOrTheDemo() {
        val store = store()
        store.rememberLogin("github.com", "octo")
        store.set("github.com", GitHostKind.AUTO, "one")
        demo = true
        store.rememberLogin("github.com", "demo")

        assertTrue(store.logins.value.isEmpty())
    }

    @Test
    fun hostJsonUsesTheTypedTokenFirstAndTheDemoNone() = runBlocking {
        val store = store()
        store.set("github.com", GitHostKind.GITHUB, "stored")
        val host = GitHost("github.com", GitHostKind.GITHUB)

        assertTrue(store.hostJson(host).contains("\"token\":\"stored\""))
        assertTrue(store.hostJson(host, "typed").contains("\"token\":\"typed\""))
        demo = true
        assertEquals("""{"kind":"demo"}""", store.hostJson(host))
    }

    @Test
    fun tokenFilesAreNamedFromAHash() {
        val name = GitHostStore.tokenName("https://github.com/")
        assertEquals(name, GitHostStore.tokenName("github.com"))
        assertNotEquals(name, GitHostStore.tokenName("gitlab.example.org"))
        assertFalse(name.contains("github"))
        assertTrue(name.matches(Regex("git-host-[0-9a-f]{24}")))
    }

    @Test
    fun theCheckDecodesFromTheGoCore() {
        val check = decodeGitHostCheck("""{"login":"octo","host":"github.com","scopesOK":false,"missing":["pull_requests:write"],"future":1}""")
        assertEquals(GitHostCheck("octo", "github.com", scopesOK = false, missing = listOf("pull_requests:write")), check)
        assertEquals(emptyList<String>(), decodeGitHostCheck("""{"login":"octo","scopesOK":true,"missing":null}""").missing)
    }
}
