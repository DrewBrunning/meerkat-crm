package com.mycorrhizal.crm.feature.contacts

import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.InsertDriveFile
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mycorrhizal.crm.model.network.ContactAttachment
import com.mycorrhizal.crm.ui.R
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * N7 contact attachments (web-parity for the contact-detail Attachments
 * section): list the contact's files, upload a new one from the system
 * document picker, download/open an existing one, and delete (confirmed).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AttachmentsScreen(
    contactId: Int,
    onBack: () -> Unit,
    viewModel: AttachmentsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    LaunchedEffect(contactId) {
        viewModel.setContact(contactId)
    }

    val pickLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument(),
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            val resolver = context.contentResolver
            // Probe the declared size BEFORE reading anything — a picker can
            // offer a multi-GB file that would OOM the app before a post-read
            // size check ever ran (mirrors ContactDetailScreen's photo picker
            // and VcfImportScreen).
            val meta = withContext(Dispatchers.IO) { queryAttachmentMeta(resolver, uri) }
            if (meta != null && meta.size != null && meta.size > AttachmentsViewModel.MAX_ATTACHMENT_SIZE_BYTES) {
                viewModel.rejectUpload()
                return@launch
            }
            val bytes = withContext(Dispatchers.IO) { readAttachmentBytes(resolver, uri) }
            // Backstop for providers that report no size.
            if (bytes.size > AttachmentsViewModel.MAX_ATTACHMENT_SIZE_BYTES) {
                viewModel.rejectUpload()
                return@launch
            }
            if (bytes.isEmpty()) return@launch
            viewModel.upload(
                fileName = meta?.name?.takeIf { it.isNotBlank() } ?: "attachment",
                mimeType = meta?.mimeType ?: "application/octet-stream",
                bytes = bytes,
            )
        }
    }

    AttachmentsScreenContent(
        uiState = state,
        onBack = onBack,
        onAddClick = { pickLauncher.launch(arrayOf("*/*")) },
        onDownload = viewModel::download,
        onDelete = viewModel::delete,
        onErrorShown = viewModel::onErrorShown,
        onDownloadHandled = viewModel::onDownloadHandled,
    )
}

/**
 * Stateless attachment content (house pattern): loading / empty / populated
 * states, the delete-confirm dialog, and the side effect that writes a
 * downloaded attachment out and opens it. The document-picker upload path
 * stays in [AttachmentsScreen] — this is the testable surface.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AttachmentsScreenContent(
    uiState: AttachmentUiState,
    onBack: () -> Unit = {},
    onAddClick: () -> Unit = {},
    onDownload: (ContactAttachment) -> Unit = {},
    onDelete: (ContactAttachment) -> Unit = {},
    onErrorShown: () -> Unit = {},
    onDownloadHandled: () -> Unit = {},
) {
    val snackbarHostState = remember { SnackbarHostState() }
    val context = LocalContext.current
    var pendingDelete by remember { mutableStateOf<ContactAttachment?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                    }
                },
                title = { Text(stringResource(R.string.contact_attachments), style = MaterialTheme.typography.titleLarge) },
                actions = {
                    IconButton(
                        onClick = onAddClick,
                        enabled = !uiState.isUploading,
                        modifier = Modifier.testTag("add-attachment"),
                    ) {
                        if (uiState.isUploading) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(20.dp),
                                strokeWidth = 2.dp,
                            )
                        } else {
                            Icon(
                                Icons.Outlined.AttachFile,
                                contentDescription = stringResource(R.string.attachments_upload),
                                tint = MaterialTheme.colorScheme.onPrimary,
                            )
                        }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = MaterialTheme.colorScheme.onPrimary,
                    navigationIconContentColor = MaterialTheme.colorScheme.onPrimary,
                    actionIconContentColor = MaterialTheme.colorScheme.onPrimary,
                ),
            )
        },
        snackbarHost = { SnackbarHost(snackbarHostState) },
    ) { padding ->
        Box(modifier = Modifier.fillMaxSize().padding(padding)) {
            when {
                uiState.isLoading && uiState.attachments.isEmpty() -> {
                    CircularProgressIndicator(
                        modifier = Modifier.align(Alignment.Center).testTag("attachments-loading"),
                    )
                }
                uiState.attachments.isEmpty() && uiState.error == null -> {
                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        modifier = Modifier.align(Alignment.Center).padding(24.dp),
                    ) {
                        Text(
                            text = stringResource(R.string.attachments_empty),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag("attachments-empty"),
                        )
                        TextButton(onClick = onAddClick) {
                            Text(stringResource(R.string.attachments_upload))
                        }
                    }
                }
                else -> {
                    LazyColumn(
                        modifier = Modifier.fillMaxSize().testTag("attachments-list"),
                        contentPadding = PaddingValues(vertical = 8.dp),
                    ) {
                        items(uiState.attachments, key = { it.id }) { attachment ->
                            AttachmentRow(
                                attachment = attachment,
                                downloading = uiState.downloadingId == attachment.id,
                                deleting = uiState.deletingId == attachment.id,
                                onOpen = { onDownload(attachment) },
                                onDelete = { pendingDelete = attachment },
                            )
                        }
                    }
                }
            }
        }
    }

    // Errors surface as a snackbar (the empty/error branch above only guards
    // a *first-load* failure; a delete/download/upload error must not blank
    // the already-loaded list).
    val errorMessage = uiState.error
        ?: uiState.uploadErrorRes?.let { stringResource(it) }
    LaunchedEffect(errorMessage) {
        if (errorMessage != null) {
            snackbarHostState.showSnackbar(errorMessage)
            onErrorShown()
        }
    }

    // A finished download is written to the cache and handed to a viewer via
    // FileProvider (the ContactDetail export pattern). Consumed exactly once.
    uiState.downloaded?.let { (attachment, bytes) ->
        val key = attachment.id
        LaunchedEffect(key) {
            openDownloadedAttachment(context, attachment, bytes)
            onDownloadHandled()
        }
    }

    pendingDelete?.let { attachment ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text(stringResource(R.string.attachments_delete_title)) },
            text = { Text(stringResource(R.string.attachments_delete_body, attachment.originalName)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        pendingDelete = null
                        onDelete(attachment)
                    },
                ) {
                    Text(stringResource(R.string.attachments_delete))
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
}

@Composable
private fun AttachmentRow(
    attachment: ContactAttachment,
    downloading: Boolean,
    deleting: Boolean,
    onOpen: () -> Unit,
    onDelete: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clickable(enabled = !downloading && !deleting, onClick = onOpen)
            .padding(horizontal = 16.dp, vertical = 12.dp)
            .testTag("attachment-${attachment.id}"),
    ) {
        Icon(
            if (downloading) {
                Icons.Outlined.Download
            } else {
                Icons.Outlined.InsertDriveFile
            },
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
        )
        Column(modifier = Modifier.weight(1f).padding(horizontal = 12.dp)) {
            Text(
                text = attachment.originalName,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = attachmentMetaLine(attachment.sizeBytes),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (deleting) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
        } else {
            IconButton(onClick = onDelete, modifier = Modifier.testTag("delete-attachment-${attachment.id}")) {
                Icon(
                    Icons.Outlined.Delete,
                    contentDescription = stringResource(R.string.attachments_delete),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun attachmentMetaLine(sizeBytes: Long): String {
    val size = if (sizeBytes > 0) {
        stringResource(R.string.attachments_size, formatFileSize(sizeBytes))
    } else {
        ""
    }
    return listOfNotNull(size.takeIf { it.isNotBlank() }).joinToString(" · ")
}

/**
 * Writes a downloaded attachment to the cache and hands it to a viewer via
 * FileProvider with a scoped read. The filename is sanitized before use — the
 * server's original_name is display-only and must never reach a filesystem
 * path untouched.
 */
private fun openDownloadedAttachment(context: Context, attachment: ContactAttachment, bytes: ByteArray) {
    val dir = File(context.cacheDir, "attachments").apply { mkdirs() }
    val safeName = attachment.originalName
        .replace(Regex("""[^A-Za-z0-9._-]"""), "_")
        .takeIf { it.isNotBlank() }
        ?: "attachment-${attachment.id}"
    val file = File(dir, safeName)
    try {
        file.writeBytes(bytes)
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, attachment.contentType.ifBlank { "application/octet-stream" })
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        context.startActivity(Intent.createChooser(intent, context.getString(R.string.attachments_open)))
    } catch (_: Exception) {
        // No activity can open this type — the download stays in the cache;
        // nothing actionable to do here.
    }
}

private data class AttachmentPick(
    val name: String?,
    val mimeType: String?,
    val size: Long?,
)

private fun queryAttachmentMeta(resolver: ContentResolver, uri: Uri): AttachmentPick {
    var name: String? = null
    var size: Long? = null
    resolver.query(uri, null, null, null, null)?.use { cursor ->
        if (cursor.moveToFirst()) {
            val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
            if (nameIndex >= 0 && !cursor.isNull(nameIndex)) name = cursor.getString(nameIndex)
            if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) size = cursor.getLong(sizeIndex)
        }
    }
    return AttachmentPick(name = name, mimeType = resolver.getType(uri), size = size)
}

/** Reads the picked file's bytes off the main thread (post-size-probe). */
private fun readAttachmentBytes(resolver: ContentResolver, uri: Uri): ByteArray =
    resolver.openInputStream(uri)?.use { it.readBytes() } ?: ByteArray(0)
