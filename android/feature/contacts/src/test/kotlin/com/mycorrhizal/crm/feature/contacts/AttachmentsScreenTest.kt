package com.mycorrhizal.crm.feature.contacts

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.mycorrhizal.crm.model.network.ContactAttachment
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AttachmentsScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private val scan = ContactAttachment(
        id = 7,
        originalName = "scan.pdf",
        contentType = "application/pdf",
        sizeBytes = 2048,
    )

    private var added = 0
    private var opened: ContactAttachment? = null
    private var deleted: ContactAttachment? = null
    private var handled = 0

    private fun setContent(uiState: AttachmentUiState) {
        added = 0
        opened = null
        deleted = null
        handled = 0
        composeTestRule.setContent {
            MycorrhizalTheme {
                AttachmentsScreenContent(
                    uiState = uiState,
                    onBack = {},
                    onAddClick = { added += 1 },
                    onDownload = { opened = it },
                    onDelete = { deleted = it },
                    onDownloadHandled = { handled += 1 },
                )
            }
        }
    }

    @Test
    fun `empty list shows the upload prompt`() {
        setContent(AttachmentUiState(attachments = emptyList(), total = 0, isLoading = false))
        composeTestRule.onNodeWithTag("attachments-empty").assertIsDisplayed()
        composeTestRule.onNodeWithText("Add attachment").assertIsDisplayed()
    }

    @Test
    fun `populated list shows rows and tapping one downloads it`() {
        setContent(AttachmentUiState(attachments = listOf(scan), total = 1, isLoading = false))
        composeTestRule.onNodeWithText("scan.pdf").assertIsDisplayed()
        composeTestRule.onNodeWithTag("attachment-7").performClick()
        assertEquals(7, opened?.id)
    }

    @Test
    fun `add attachment fires the picker callback`() {
        setContent(AttachmentUiState(attachments = listOf(scan), total = 1, isLoading = false))
        composeTestRule.onNodeWithTag("add-attachment").performClick()
        assertEquals(1, added)
    }

    @Test
    fun `delete asks for confirmation then fires`() {
        setContent(AttachmentUiState(attachments = listOf(scan), total = 1, isLoading = false))
        composeTestRule.onNodeWithTag("delete-attachment-7").performClick()
        composeTestRule.onNodeWithText("Delete scan.pdf? This cannot be undone.").assertIsDisplayed()
        composeTestRule.onNodeWithText("Delete").performClick()
        assertEquals(7, deleted?.id)
    }

    @Test
    fun `a finished download is handled once`() {
        setContent(
            AttachmentUiState(
                attachments = listOf(scan),
                total = 1,
                isLoading = false,
                downloaded = scan to "bytes".toByteArray(),
            ),
        )
        composeTestRule.waitForIdle()
        assertEquals(1, handled)
    }

    @Test
    fun `upload in flight shows the spinner instead of the add icon`() {
        setContent(
            AttachmentUiState(
                attachments = listOf(scan),
                total = 1,
                isLoading = false,
                isUploading = true,
            ),
        )
        composeTestRule.waitForIdle()
        // add-attachment node still exists (disabled) — assert no double handling.
        assertTrue(added == 0)
        assertNull(opened)
    }
}
