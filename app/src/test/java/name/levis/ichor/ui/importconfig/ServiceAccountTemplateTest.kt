package name.levis.ichor.ui.importconfig

import name.levis.ichor.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ServiceAccountTemplateTest {

    private val json = """
        [{"id":"read","yaml":"kind: ServiceAccount","apply":"kubectl apply -f ichor-phone.yaml","kubeconfig":"NS=ichor"},
         {"id":"future","yaml":"","apply":"","kubeconfig":"","extra":1}]
    """.trimIndent()

    @Test
    fun decodesTheCoreListIgnoringUnknownFields() {
        val templates = decodeServiceAccountTemplates(json)
        assertEquals(listOf("read", "future"), templates.map { it.id })
        assertEquals("kubectl apply -f ichor-phone.yaml", templates[0].apply)
    }

    @Test
    fun knownIdsHaveATitleUnknownOnesNone() {
        assertEquals(R.string.sa_help_template_read, ServiceAccountRole.of("read")?.label)
        assertEquals(R.string.sa_help_template_namespace, ServiceAccountRole.of("namespace")?.label)
        assertNull(ServiceAccountRole.of("future"))
    }
}
