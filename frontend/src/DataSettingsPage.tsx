import DownloadIcon from '@mui/icons-material/Download';
import InsightsIcon from '@mui/icons-material/Insights';
import TuneIcon from '@mui/icons-material/Tune';
import UploadIcon from '@mui/icons-material/Upload';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Divider,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { suggestContactAddresses } from './api/dataSuggestions';
import {
  type ExportFormat,
  type ExportSelection,
  exportContacts,
  exportContactsAsVcf,
  exportDataAsCsv,
} from './api/export';
import { getImportHistory, type ImportRun } from './api/import';
import { suggestRelationshipEdges } from './api/relationshipEdges';
import CalendarSyncSettings from './components/CalendarSyncSettings';
import ContactAddressSuggestions from './components/ContactAddressSuggestions';
import ContactFieldSettings from './components/ContactFieldSettings';
import ContactSyncSettings from './components/ContactSyncSettings';
import CustomFieldsSettings from './components/CustomFieldsSettings';
import ExportFieldPickerDialog from './components/ExportFieldPickerDialog';
import ImportContactsDialog from './components/ImportContactsDialog';
import MeerkatImportDialog from './components/MeerkatImportDialog';
import MonicaImportDialog from './components/MonicaImportDialog';
import RelationshipSuggestionsInbox from './components/RelationshipSuggestionsInbox';
import { useDocumentTitle } from './hooks/useDocumentTitle';
import { handleFetchError } from './utils/errorHandler';

export default function DataSettingsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('nav.data'));
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const [exportSuccess, setExportSuccess] = useState('');
  const [exportingVcf, setExportingVcf] = useState(false);
  const [exportVcfError, setExportVcfError] = useState('');
  const [exportVcfSuccess, setExportVcfSuccess] = useState('');
  const [pickerOpen, setPickerOpen] = useState(false);
  const [customExporting, setCustomExporting] = useState(false);
  const [customExportError, setCustomExportError] = useState('');
  const [customExportSuccess, setCustomExportSuccess] = useState('');

  // T56: the
  // bulk import entry point, reusing the exact same wizard the Contacts page
  // uses — one import flow, two doors.
  const [importOpen, setImportOpen] = useState(false);
  // Monica import assistant (issue #549) — a separate door into the same
  // preview→confirm contract, for users migrating from a live Monica account.
  const [monicaImportOpen, setMonicaImportOpen] = useState(false);
  // Meerkat import assistant (issue #550) — direct-DB upload for migrating
  // from an upstream meerkat-crm deployment.
  const [meerkatImportOpen, setMeerkatImportOpen] = useState(false);

  // Issue #651: persisted import run history, refreshed on mount and whenever
  // an import completes.
  const [importHistory, setImportHistory] = useState<ImportRun[]>([]);
  const [importHistoryError, setImportHistoryError] = useState('');

  const loadImportHistory = useCallback(async () => {
    try {
      setImportHistory(await getImportHistory());
      setImportHistoryError('');
    } catch (error) {
      setImportHistoryError(
        error instanceof Error ? error.message : t('settings.data.import.history.loadError'),
      );
    }
  }, [t]);

  useEffect(() => {
    void loadImportHistory();
  }, [loadImportHistory]);

  // T104 + address suggestions: opt-in triggers for the inference engines,
  // plus the message state for the last run. Bumping the load keys reloads
  // the inboxes below.
  const [suggestingRelationships, setSuggestingRelationships] = useState(false);
  const [relationshipSuggestMessage, setRelationshipSuggestMessage] = useState<string | null>(null);
  const [relationshipSuggestError, setRelationshipSuggestError] = useState<string | null>(null);
  const [relationshipLoadKey, setRelationshipLoadKey] = useState(0);
  const [suggestingAddresses, setSuggestingAddresses] = useState(false);
  const [addressSuggestMessage, setAddressSuggestMessage] = useState<string | null>(null);
  const [addressSuggestError, setAddressSuggestError] = useState<string | null>(null);
  const [addressLoadKey, setAddressLoadKey] = useState(0);

  const handleSuggestRelationships = async () => {
    setRelationshipSuggestError('');
    setRelationshipSuggestMessage('');
    setSuggestingRelationships(true);
    try {
      const result = await suggestRelationshipEdges();
      setRelationshipLoadKey((n) => n + 1);
      setRelationshipSuggestMessage(
        result.total > 0
          ? t('settings.data.propose.relationshipsGenerated', { count: result.total })
          : t('settings.data.propose.noRelationshipSuggestions'),
      );
    } catch (error) {
      handleFetchError(error, 'suggesting relationships');
      setRelationshipSuggestError(t('settings.data.propose.relationshipsError'));
    } finally {
      setSuggestingRelationships(false);
    }
  };

  const handleSuggestAddresses = async () => {
    setAddressSuggestError('');
    setAddressSuggestMessage('');
    setSuggestingAddresses(true);
    try {
      const result = await suggestContactAddresses();
      setAddressLoadKey((n) => n + 1);
      setAddressSuggestMessage(
        result.total > 0
          ? t('settings.data.propose.addressesGenerated', { count: result.total })
          : t('settings.data.propose.noAddressSuggestions'),
      );
    } catch (error) {
      handleFetchError(error, 'suggesting addresses');
      setAddressSuggestError(t('settings.data.propose.addressesError'));
    } finally {
      setSuggestingAddresses(false);
    }
  };

  const handleExportData = async () => {
    setExportError('');
    setExportSuccess('');
    setExporting(true);

    try {
      await exportDataAsCsv();
      setExportSuccess(t('settings.export.success'));
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : t('settings.export.error');
      setExportError(errorMessage);
    } finally {
      setExporting(false);
    }
  };

  const handleExportVcf = async () => {
    setExportVcfError('');
    setExportVcfSuccess('');
    setExportingVcf(true);

    try {
      await exportContactsAsVcf();
      setExportVcfSuccess(t('settings.exportVcf.success'));
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : t('settings.exportVcf.error');
      setExportVcfError(errorMessage);
    } finally {
      setExportingVcf(false);
    }
  };

  const handleCustomExport = async (format: ExportFormat, selection: ExportSelection) => {
    setCustomExportError('');
    setCustomExportSuccess('');
    setCustomExporting(true);
    try {
      await exportContacts(format, selection);
      setCustomExportSuccess(t('settings.exportFieldPicker.success'));
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : t('settings.exportFieldPicker.exportError');
      setCustomExportError(errorMessage);
    } finally {
      setCustomExporting(false);
    }
  };

  return (
    <Box sx={{ maxWidth: 1200, mx: 'auto', mt: 2, p: 2 }}>
      <Typography variant="h5" component="h1" gutterBottom sx={{ mb: 1.5 }}>
        {t('settings.data.title')}
      </Typography>

      <ContactFieldSettings />

      <CustomFieldsSettings />

      <CalendarSyncSettings />

      <ContactSyncSettings />

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <InsightsIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.data.propose.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Stack spacing={1.5}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('settings.data.propose.description')}
            </Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
              <Button
                variant="contained"
                size="small"
                startIcon={
                  suggestingRelationships ? (
                    <CircularProgress size={16} color="inherit" />
                  ) : undefined
                }
                onClick={handleSuggestRelationships}
                disabled={suggestingRelationships}
              >
                {suggestingRelationships
                  ? t('settings.data.propose.suggestingRelationships')
                  : t('settings.data.propose.suggestRelationships')}
              </Button>
              <Button
                variant="contained"
                size="small"
                startIcon={
                  suggestingAddresses ? <CircularProgress size={16} color="inherit" /> : undefined
                }
                onClick={handleSuggestAddresses}
                disabled={suggestingAddresses}
              >
                {suggestingAddresses
                  ? t('settings.data.propose.suggestingAddresses')
                  : t('settings.data.propose.suggestAddresses')}
              </Button>
            </Stack>
            {relationshipSuggestError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {relationshipSuggestError}
              </Alert>
            )}
            {relationshipSuggestMessage && (
              <Alert severity="info" sx={{ py: 0 }}>
                {relationshipSuggestMessage}
              </Alert>
            )}
            {addressSuggestError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {addressSuggestError}
              </Alert>
            )}
            {addressSuggestMessage && (
              <Alert severity="info" sx={{ py: 0 }}>
                {addressSuggestMessage}
              </Alert>
            )}
          </Stack>

          <RelationshipSuggestionsInbox loadKey={relationshipLoadKey} />
          <ContactAddressSuggestions loadKey={addressLoadKey} />
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <UploadIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.data.import.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Stack spacing={1.5}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('settings.data.import.description')}
            </Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
              <Button
                variant="contained"
                size="small"
                startIcon={<UploadIcon />}
                onClick={() => setImportOpen(true)}
              >
                {t('settings.data.import.importButton')}
              </Button>
              <Button variant="outlined" size="small" onClick={() => setMonicaImportOpen(true)}>
                {t('settings.data.import.monicaButton')}
              </Button>
              <Button variant="outlined" size="small" onClick={() => setMeerkatImportOpen(true)}>
                {t('settings.data.import.meerkatButton')}
              </Button>
            </Stack>

            <Divider />
            <Typography variant="subtitle2" component="h3">
              {t('settings.data.import.history.title')}
            </Typography>
            {importHistoryError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {importHistoryError}
              </Alert>
            )}
            {importHistory.length === 0 ? (
              <Typography
                variant="body2"
                sx={{
                  color: 'text.secondary',
                }}
              >
                {t('settings.data.import.history.empty')}
              </Typography>
            ) : (
              <Box sx={{ overflowX: 'auto' }}>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>{t('settings.data.import.history.colWhen')}</TableCell>
                      <TableCell>{t('settings.data.import.history.colFormat')}</TableCell>
                      <TableCell align="right">
                        {t('settings.data.import.history.colCreated')}
                      </TableCell>
                      <TableCell align="right">
                        {t('settings.data.import.history.colUpdated')}
                      </TableCell>
                      <TableCell align="right">
                        {t('settings.data.import.history.colSkipped')}
                      </TableCell>
                      <TableCell align="right">
                        {t('settings.data.import.history.colErrors')}
                      </TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {importHistory.map((run) => (
                      <TableRow key={run.id}>
                        <TableCell>{new Date(run.created_at).toLocaleString()}</TableCell>
                        <TableCell>
                          {t(`settings.data.import.history.format.${run.format}`)}
                        </TableCell>
                        <TableCell align="right">{run.created}</TableCell>
                        <TableCell align="right">{run.updated}</TableCell>
                        <TableCell align="right">{run.skipped}</TableCell>
                        <TableCell align="right">{run.error_count}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            )}
          </Stack>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <DownloadIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.export.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Stack spacing={1.5}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('settings.export.description')}
            </Typography>
            {/*
              Issue #861: the CSV is the user's own full backup, so it carries
              private/secret items and unconfirmed relationships that the
              vCard/JSContact exports hold back. Saying so before the download
              is the point — the sensitivity marker means "don't share this",
              and a user who assumes it also means "not in my backup file"
              would hand out more than they meant to.
            */}
            <Alert severity="info" sx={{ py: 0 }}>
              {t('settings.export.sensitivityNotice')}
            </Alert>
            {exportError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {exportError}
              </Alert>
            )}
            {exportSuccess && (
              <Alert severity="success" sx={{ py: 0 }}>
                {exportSuccess}
              </Alert>
            )}
            <Button
              variant="contained"
              size="small"
              startIcon={
                exporting ? <CircularProgress size={16} color="inherit" /> : <DownloadIcon />
              }
              onClick={handleExportData}
              disabled={exporting}
            >
              {exporting ? t('settings.export.exporting') : t('settings.export.downloadButton')}
            </Button>
          </Stack>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <DownloadIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.exportVcf.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Stack spacing={1.5}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('settings.exportVcf.description')}
            </Typography>
            {exportVcfError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {exportVcfError}
              </Alert>
            )}
            {exportVcfSuccess && (
              <Alert severity="success" sx={{ py: 0 }}>
                {exportVcfSuccess}
              </Alert>
            )}
            <Stack direction="row" spacing={1}>
              <Button
                variant="contained"
                size="small"
                startIcon={
                  exportingVcf ? <CircularProgress size={16} color="inherit" /> : <DownloadIcon />
                }
                onClick={handleExportVcf}
                disabled={exportingVcf}
              >
                {exportingVcf
                  ? t('settings.exportVcf.exporting')
                  : t('settings.exportVcf.downloadButton')}
              </Button>
              <Button
                variant="outlined"
                size="small"
                startIcon={<TuneIcon />}
                onClick={() => setPickerOpen(true)}
                disabled={customExporting}
              >
                {t('settings.exportFieldPicker.customExportButton')}
              </Button>
            </Stack>
            {customExportError && (
              <Alert severity="error" sx={{ py: 0 }}>
                {customExportError}
              </Alert>
            )}
            {customExportSuccess && (
              <Alert severity="success" sx={{ py: 0 }}>
                {customExportSuccess}
              </Alert>
            )}
          </Stack>
        </CardContent>
      </Card>

      <ExportFieldPickerDialog
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onExport={handleCustomExport}
      />

      <ImportContactsDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onImportComplete={() => {
          setImportOpen(false);
          void loadImportHistory();
        }}
      />

      <MonicaImportDialog
        open={monicaImportOpen}
        onClose={() => setMonicaImportOpen(false)}
        onImportComplete={() => {
          setMonicaImportOpen(false);
          void loadImportHistory();
        }}
      />

      <MeerkatImportDialog
        open={meerkatImportOpen}
        onClose={() => setMeerkatImportOpen(false)}
        onImportComplete={() => {
          setMeerkatImportOpen(false);
          void loadImportHistory();
        }}
      />
    </Box>
  );
}
