package com.mycorrhizal.crm.feature.settings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import com.mycorrhizal.crm.model.network.CalendarSubscription
import com.mycorrhizal.crm.model.network.CalendarSyncResult
import com.mycorrhizal.crm.model.network.ContactSubscription
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

    // --- CalendarSyncContent: the full screen body, split from CalendarSyncScreen
    // (mirroring TwoFactorScreen/TwoFactorContent) so every loading/empty/error/
    // dialog branch is directly testable with a plain CalendarSyncUiState.

    @Test
    fun `loading with no data yet shows only a spinner`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(isLoading = true),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithText("No calendars added yet.").assertDoesNotExist()
    }

    @Test
    fun `no calendars and no contact subscriptions shows the empty state`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithText("No calendars added yet.").assertIsDisplayed()
    }

    @Test
    fun `an action error is shown as a banner`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(
                        calendars = listOf(CalendarSubscription(id = 1, name = "Personal")),
                        error = "Server error (500)",
                    ),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Server error (500)").assertIsDisplayed()
    }

    @Test
    fun `a sync result is shown as a success banner`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(
                        calendars = listOf(CalendarSubscription(id = 1, name = "Personal")),
                        lastSyncResult = CalendarSyncResult(created = 2, updated = 1, skipped = 0),
                    ),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithText("Synced: 2 created, 1 updated, 0 skipped").assertIsDisplayed()
    }

    @Test
    fun `contact subscriptions render below a divider even with no calendars`() {
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(
                        contactSubscriptions = listOf(ContactSubscription(id = 1, name = "Address Book")),
                    ),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithText("No calendars added yet.").assertIsDisplayed()
        composeTestRule.onNodeWithText("Contact Sync").assertIsDisplayed()
        composeTestRule.onNodeWithText("Address Book").assertIsDisplayed()
    }

    @Test
    fun `the back button invokes onBack`() {
        var backed = false
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(calendars = listOf(CalendarSubscription(id = 1, name = "Personal"))),
                    onBack = { backed = true },
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithContentDescription("Back").performClick()
        assertTrue(backed)
    }

    @Test
    fun `the sync-now button on a row invokes onSync with that calendar`() {
        var synced: CalendarSubscription? = null
        val calendar = CalendarSubscription(id = 1, name = "Personal")
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(calendars = listOf(calendar)),
                    onBack = {},
                    onSync = { synced = it },
                    onSave = { _, _ -> },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithContentDescription("Sync Personal now").performClick()
        assertEquals(calendar, synced)
    }

    @Test
    fun `the fab opens the add dialog and confirming calls onSave with no editing id`() {
        var savedName: String? = null
        var savedEditingId: Int? = -1
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(calendars = listOf(CalendarSubscription(id = 1, name = "Personal"))),
                    onBack = {},
                    onSync = {},
                    onSave = { input, editingId -> savedName = input.name; savedEditingId = editingId },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithContentDescription("Add Calendar").performClick()
        composeTestRule.onNodeWithText("Name").performTextInput("New Calendar")
        composeTestRule.onNodeWithText("Calendar URL").performTextInput("https://example.com/new.ics")
        composeTestRule.onNodeWithText("Save").performClick()

        assertEquals("New Calendar", savedName)
        assertNull(savedEditingId)
    }

    @Test
    fun `the edit button on a row opens a pre-filled dialog and confirming saves with that id`() {
        var savedEditingId: Int? = null
        val calendar = CalendarSubscription(id = 7, name = "Personal", url = "https://example.com/a.ics")
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(calendars = listOf(calendar)),
                    onBack = {},
                    onSync = {},
                    onSave = { _, editingId -> savedEditingId = editingId },
                    onDelete = {},
                )
            }
        }

        composeTestRule.onNodeWithContentDescription("Edit Personal").performClick()
        composeTestRule.onNodeWithText("Edit Calendar").assertIsDisplayed()
        composeTestRule.onNodeWithText("Save").performClick()

        assertEquals(7, savedEditingId)
    }

    @Test
    fun `the delete button on a row confirms before invoking onDelete`() {
        var deleted: CalendarSubscription? = null
        val calendar = CalendarSubscription(id = 1, name = "Personal")
        composeTestRule.setContent {
            MycorrhizalTheme {
                CalendarSyncContent(
                    state = CalendarSyncUiState(calendars = listOf(calendar)),
                    onBack = {},
                    onSync = {},
                    onSave = { _, _ -> },
                    onDelete = { deleted = it },
                )
            }
        }

        composeTestRule.onNodeWithContentDescription("Delete Personal").performClick()
        // Confirmation dialog: not deleted yet.
        assertEquals(null, deleted)
        composeTestRule.onNodeWithText("Delete Calendar").assertIsDisplayed()
        composeTestRule.onNodeWithText("Delete").performClick()

        assertEquals(calendar, deleted)
    }
}
