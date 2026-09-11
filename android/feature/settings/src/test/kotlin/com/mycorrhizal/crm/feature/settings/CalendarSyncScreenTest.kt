package com.mycorrhizal.crm.feature.settings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.ContactSubscription
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertEquals
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
class CalendarSyncScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    @Test
    fun `shows a calendar row with sync edit and delete actions`() {
        val calendar = CalendarSubscription(id = 1, name = "Personal", url = "https://example.com/a.ics")
        var synced = false
        var edited = false
        var deleted = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSubscriptionRow(
                    calendar = calendar,
                    syncing = false,
                    onSync = { synced = true },
                    onEdit = { edited = true },
                    onDelete = { deleted = true },
                )
            }
        }

        composeTestRule.onNodeWithText("Personal").assertIsDisplayed()
        composeTestRule.onNodeWithContentDescription("Sync Personal now").performClick()
        composeTestRule.onNodeWithContentDescription("Edit Personal").performClick()
        composeTestRule.onNodeWithContentDescription("Delete Personal").performClick()
        assertTrue(synced && edited && deleted)
    }

    @Test
    fun `a disabled calendar shows the disabled label`() {
        val calendar = CalendarSubscription(id = 1, name = "Personal", url = "https://example.com/a.ics", syncEnabled = false)
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSubscriptionRow(calendar = calendar, syncing = false, onSync = {}, onEdit = {}, onDelete = {})
            }
        }

        composeTestRule.onNodeWithText("Disabled").assertIsDisplayed()
    }

    @Test
    fun `a standing failure shows the failing-since line with the consecutive count`() {
        val calendar = CalendarSubscription(
            id = 1,
            name = "Personal",
            url = "https://example.com/a.ics",
            lastSyncStatus = "error",
            consecutiveFailures = 3,
            incidentFirstFailureAt = "2026-01-01T00:00:00Z",
        )
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSubscriptionRow(calendar = calendar, syncing = false, onSync = {}, onEdit = {}, onDelete = {})
            }
        }

        composeTestRule.onNodeWithText("Sync failed (3 times)").assertIsDisplayed()
        composeTestRule.onNodeWithText("consecutive failures", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("last success: never", substring = true).assertIsDisplayed()
    }

    @Test
    fun `a healthy sync shows the last run tallies`() {
        val calendar = CalendarSubscription(
            id = 1,
            name = "Personal",
            url = "https://example.com/a.ics",
            lastSyncStatus = "success",
            lastRunStats = mapOf("created" to 2, "updated" to 1, "skipped" to 0),
        )
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSubscriptionRow(calendar = calendar, syncing = false, onSync = {}, onEdit = {}, onDelete = {})
            }
        }

        composeTestRule.onNodeWithText("Last sync: 2 created, 1 updated, 0 skipped").assertIsDisplayed()
    }

    @Test
    fun `a terminal failure shows the dedicated notice instead of the failing-since line`() {
        val calendar = CalendarSubscription(
            id = 1,
            name = "Personal",
            url = "https://example.com/a.ics",
            consecutiveFailures = 5,
            terminalFailureAt = "2026-01-01T00:00:00Z",
            terminalReason = "auth-expiry",
        )
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSubscriptionRow(calendar = calendar, syncing = false, onSync = {}, onEdit = {}, onDelete = {})
            }
        }

        composeTestRule.onNodeWithText("Sync stopped").assertIsDisplayed()
        composeTestRule.onNodeWithText("credentials have expired", substring = true).assertIsDisplayed()
        composeTestRule.onNodeWithText("consecutive failures", substring = true).assertDoesNotExist()
    }

    @Test
    fun `a contact subscription row is read-only and shows pending conflicts`() {
        val subscription = ContactSubscription(
            id = 1,
            name = "Address Book",
            url = "https://example.com/carddav/",
            lastSyncStatus = "success",
            lastRunStats = mapOf("created" to 1, "updated" to 0, "archived" to 0, "skipped" to 0),
            pendingConflicts = 2,
        )
        composeTestRule.setContent {
            MycorrhizalTheme {
                ContactSubscriptionRow(subscription = subscription)
            }
        }

        composeTestRule.onNodeWithText("Address Book").assertIsDisplayed()
        composeTestRule.onNodeWithText("Last sync: 1 created, 0 updated, 0 archived, 0 skipped").assertIsDisplayed()
        composeTestRule.onNodeWithText("2 unreviewed conflicts").assertIsDisplayed()
    }

    @Test
    fun `the editor dialog confirms with trimmed input`() {
        var confirmedName = ""
        var confirmedUrl = ""
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarEditorDialog(
                    initial = null,
                    isSaving = false,
                    onConfirm = { input ->
                        confirmedName = input.name
                        confirmedUrl = input.url
                    },
                    onDismiss = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Name").performTextInput("Personal")
        composeTestRule.onNodeWithText("Calendar URL").performTextInput(" https://example.com/a.ics ")
        composeTestRule.onNodeWithText("Save").performClick()

        assertEquals("Personal", confirmedName)
        assertEquals("https://example.com/a.ics", confirmedUrl)
    }

    @Test
    fun `an insecure url with credentials shows a warning`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarEditorDialog(initial = null, isSaving = false, onConfirm = {}, onDismiss = {})
            }
        }

        composeTestRule.onNodeWithText("Calendar URL").performTextInput("http://example.com/a.ics")
        composeTestRule.onNodeWithText("Username").performTextInput("alice")

        composeTestRule.onNodeWithText("not encrypted", substring = true).assertIsDisplayed()
    }
}
