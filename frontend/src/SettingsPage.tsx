import AddIcon from '@mui/icons-material/Add';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import BlockIcon from '@mui/icons-material/Block';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import KeyIcon from '@mui/icons-material/Key';
import LanguageIcon from '@mui/icons-material/Language';
import LockResetIcon from '@mui/icons-material/LockReset';
import PersonIcon from '@mui/icons-material/Person';
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  IconButton,
  InputLabel,
  Link,
  MenuItem,
  Paper,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import type { SelectChangeEvent } from '@mui/material/Select';
import { type FormEvent, useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router';
import { type ThemePreference, useThemePreference } from './AppThemeProvider';
import { getCurrentUser } from './api/admin';
import {
  API_TOKEN_EXPIRY_OPTIONS,
  type ApiToken,
  type ApiTokenCreateResponse,
  type ApiTokenScope,
  createApiToken,
  DEFAULT_API_TOKEN_EXPIRY_DAYS,
  DEFAULT_API_TOKEN_SCOPE,
  getApiTokens,
  revokeAllApiTokens,
  revokeApiToken,
  rotateApiToken,
} from './api/apiTokens';
import { changePassword } from './api/auth';
import { type Contact, getContacts, getContactsByUid } from './api/contacts';
import { updateDateFormat, updateLanguage, updateSelfContact } from './api/users';
import { fetchAndCacheUserInfo, isAdmin } from './auth';
import AppDialog from './components/AppDialog';
import BuildVersionCard from './components/BuildVersionCard';
import ImmichSettings from './components/ImmichSettings';
import LinkFieldTypesSettings from './components/LinkFieldTypesSettings';
import NextcloudSettings from './components/NextcloudSettings';
import NotificationSettings from './components/NotificationSettings';
import PaperlessSettings from './components/PaperlessSettings';
import SeafileSettings from './components/SeafileSettings';
import SessionsSettings from './components/SessionsSettings';
import TwoFactorSettings from './components/TwoFactorSettings';
import WebhooksSettings from './components/WebhooksSettings';
import { useSnackbar } from './context/SnackbarContext';
import { type DateFormat, useDateFormat } from './DateFormatProvider';
import { useDocumentTitle } from './hooks/useDocumentTitle';

export default function SettingsPage() {
  const { t, i18n } = useTranslation();
  useDocumentTitle(t('nav.profile'));
  const { preference: themePreference, setPreference: setThemePreference } = useThemePreference();
  const { dateFormat, setDateFormat } = useDateFormat();
  const { showSuccess, showError } = useSnackbar();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [passwordError, setPasswordError] = useState('');
  const [passwordSuccess, setPasswordSuccess] = useState('');
  const [changingPassword, setChangingPassword] = useState(false);

  // T90: "Your contact" picker — which of the caller's own contacts is them.
  const [selfContact, setSelfContact] = useState<Contact | null>(null);
  const [selfContactOptions, setSelfContactOptions] = useState<Contact[]>([]);
  const [selfContactSearch, setSelfContactSearch] = useState('');
  const [selfContactLoading, setSelfContactLoading] = useState(false);
  const [selfContactSaving, setSelfContactSaving] = useState(false);

  // API tokens
  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [tokensLoading, setTokensLoading] = useState(true);
  const [tokensError, setTokensError] = useState('');
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newTokenName, setNewTokenName] = useState('');
  const [newTokenExpiryDays, setNewTokenExpiryDays] = useState<number>(
    DEFAULT_API_TOKEN_EXPIRY_DAYS,
  );
  const [newTokenScope, setNewTokenScope] = useState<ApiTokenScope>(DEFAULT_API_TOKEN_SCOPE);
  const [createLoading, setCreateLoading] = useState(false);
  const [createdToken, setCreatedToken] = useState<ApiTokenCreateResponse | null>(null);
  const [createdTokenIsRotation, setCreatedTokenIsRotation] = useState(false);
  const [copied, setCopied] = useState(false);
  const [revokeDialogOpen, setRevokeDialogOpen] = useState(false);
  const [revokingToken, setRevokingToken] = useState<ApiToken | null>(null);
  const [revokeLoading, setRevokeLoading] = useState(false);
  const [revokeAllDialogOpen, setRevokeAllDialogOpen] = useState(false);
  const [revokeAllLoading, setRevokeAllLoading] = useState(false);
  const [rotateDialogOpen, setRotateDialogOpen] = useState(false);
  const [rotatingToken, setRotatingToken] = useState<ApiToken | null>(null);
  const [rotateLoading, setRotateLoading] = useState(false);

  const fetchTokens = useCallback(async () => {
    setTokensLoading(true);
    setTokensError('');
    try {
      const response = await getApiTokens();
      setTokens(response.tokens);
    } catch (err) {
      setTokensError(err instanceof Error ? err.message : t('apiTokens.loadError'));
    } finally {
      setTokensLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchTokens();
  }, [fetchTokens]);

  const handleCreateToken = async () => {
    if (!newTokenName.trim()) return;
    setCreateLoading(true);
    try {
      const result = await createApiToken(newTokenName.trim(), newTokenExpiryDays, newTokenScope);
      setCreateDialogOpen(false);
      setNewTokenName('');
      setNewTokenExpiryDays(DEFAULT_API_TOKEN_EXPIRY_DAYS);
      setNewTokenScope(DEFAULT_API_TOKEN_SCOPE);
      setCreatedToken(result);
      setCreatedTokenIsRotation(false);
      setCopied(false);
      await fetchTokens();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('apiTokens.createError'));
    } finally {
      setCreateLoading(false);
    }
  };

  const handleCopy = () => {
    if (createdToken) {
      navigator.clipboard.writeText(createdToken.token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleRevoke = async () => {
    if (!revokingToken) return;
    setRevokeLoading(true);
    try {
      await revokeApiToken(revokingToken.id);
      setRevokeDialogOpen(false);
      setRevokingToken(null);
      showSuccess(t('apiTokens.revokeSuccess'));
      await fetchTokens();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('apiTokens.revokeError'));
    } finally {
      setRevokeLoading(false);
    }
  };

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return t('apiTokens.neverUsed');
    return new Date(dateStr).toLocaleString();
  };

  const isExpired = (token: ApiToken) =>
    token.expires_at !== null && new Date(token.expires_at) <= new Date();

  const isActive = (token: ApiToken) => !token.revoked_at && !isExpired(token);
  const activeTokenCount = tokens.filter(isActive).length;

  const handleRevokeAll = async () => {
    setRevokeAllLoading(true);
    try {
      const result = await revokeAllApiTokens();
      setRevokeAllDialogOpen(false);
      showSuccess(t('apiTokens.revokeAllSuccess', { count: result.revoked }));
      await fetchTokens();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('apiTokens.revokeAllError'));
    } finally {
      setRevokeAllLoading(false);
    }
  };

  const handleRotate = async () => {
    if (!rotatingToken) return;
    setRotateLoading(true);
    try {
      const result = await rotateApiToken(rotatingToken.id);
      setRotateDialogOpen(false);
      setRotatingToken(null);
      setCreatedToken(result);
      setCreatedTokenIsRotation(true);
      setCopied(false);
      showSuccess(t('apiTokens.rotateSuccess'));
      await fetchTokens();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('apiTokens.rotateError'));
    } finally {
      setRotateLoading(false);
    }
  };

  const handleLanguageChange = async (event: SelectChangeEvent) => {
    const newLang = event.target.value;
    // Update frontend i18n immediately for responsive UI
    i18n.changeLanguage(newLang);

    // Sync to backend for email language preferences (fire and forget)
    try {
      await updateLanguage(newLang);
    } catch (error) {
      // Silently fail - the frontend language is still updated
      // Backend sync failure doesn't affect UI language
      console.error('Failed to sync language to backend:', error);
    }
  };

  const handleThemeChange = (event: SelectChangeEvent<ThemePreference>) => {
    setThemePreference(event.target.value as ThemePreference);
  };

  const handleDateFormatChange = async (event: SelectChangeEvent<DateFormat>) => {
    const newFormat = event.target.value as DateFormat;
    // Update frontend immediately for responsive UI
    setDateFormat(newFormat);

    // Sync to backend for email date format preferences (fire and forget)
    try {
      await updateDateFormat(newFormat);
    } catch (error) {
      // Silently fail - the frontend date format is still updated
      // Backend sync failure doesn't affect UI date format
      console.error('Failed to sync date format to backend:', error);
    }
  };

  const handlePasswordChange = async (event: FormEvent) => {
    event.preventDefault();
    setPasswordError('');
    setPasswordSuccess('');

    if (newPassword !== confirmPassword) {
      setPasswordError(t('settings.password.mismatch'));
      return;
    }

    setChangingPassword(true);

    try {
      const message = await changePassword(currentPassword, newPassword);
      setPasswordSuccess(message || t('settings.password.success'));
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : t('settings.password.error');
      setPasswordError(errorMessage);
    } finally {
      setChangingPassword(false);
    }
  };

  // T90: resolve the current self-contact (by uid, including archived — a user
  // may have archived the contact they marked as "Me") so the picker shows what
  // is currently selected.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const user = await getCurrentUser();
        const uid = user.self_contact_vcard_uid ?? null;
        if (!uid) {
          if (!cancelled) setSelfContact(null);
          return;
        }
        const byUid = await getContactsByUid([uid]);
        if (!cancelled) setSelfContact(byUid.get(uid) ?? null);
      } catch {
        // Non-critical on the settings page; the picker just stays empty.
        if (!cancelled) setSelfContact(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // T90: options for the picker autocomplete — the same getContacts({search})
  // shape MergeContactsDialog uses, debounced like it is there.
  const loadSelfContactOptions = useCallback(async (search: string = '') => {
    setSelfContactLoading(true);
    try {
      const response = await getContacts({ limit: 100, search });
      setSelfContactOptions(response.contacts);
    } catch {
      setSelfContactOptions([]);
    } finally {
      setSelfContactLoading(false);
    }
  }, []);

  useEffect(() => {
    const timeoutId = setTimeout(
      () => loadSelfContactOptions(selfContactSearch),
      selfContactSearch ? 300 : 0,
    );
    return () => clearTimeout(timeoutId);
  }, [selfContactSearch, loadSelfContactOptions]);

  const selfContactName = (c: Contact | null): string => {
    if (!c) return '';
    return [c.firstname, c.lastname].filter(Boolean).join(' ') || c.nickname || '';
  };

  const handleSelfContactSelect = async (contact: Contact | null) => {
    if (selfContactSaving) return;
    setSelfContactSaving(true);
    try {
      await updateSelfContact(contact?.uid ?? null);
      setSelfContact(contact);
      // Refresh the localStorage cache so the contact-detail badge reads the
      // change without a reload (auth.ts getCachedSelfContactVCardUID).
      await fetchAndCacheUserInfo();
      showSuccess(
        contact ? t('settings.selfContact.saveSuccess') : t('settings.selfContact.clearSuccess'),
      );
    } catch (error) {
      showError(error instanceof Error ? error.message : t('settings.selfContact.saveError'));
    } finally {
      setSelfContactSaving(false);
    }
  };

  return (
    <Box sx={{ maxWidth: 1200, mx: 'auto', mt: 2, p: 2 }}>
      <Typography variant="h5" component="h1" gutterBottom sx={{ mb: 1.5 }}>
        {t('settings.title')}
      </Typography>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <LanguageIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.language.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <FormControl fullWidth size="small">
            <InputLabel id="language-select-label">{t('settings.language.label')}</InputLabel>
            <Select
              labelId="language-select-label"
              value={i18n.language}
              label={t('settings.language.label')}
              onChange={handleLanguageChange}
            >
              <MenuItem value="en">English</MenuItem>
              <MenuItem value="de">Deutsch</MenuItem>
              <MenuItem value="it">Italiano</MenuItem>
              <MenuItem value="es">Español</MenuItem>
              <MenuItem value="fr">Français</MenuItem>
            </Select>
          </FormControl>

          <Typography
            variant="caption"
            sx={{
              color: 'text.secondary',
              mt: 1,
              display: 'block',
            }}
          >
            {t('settings.language.description')}
          </Typography>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <CalendarMonthIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.dateFormat.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <FormControl fullWidth size="small">
            <InputLabel id="date-format-select-label">{t('settings.dateFormat.label')}</InputLabel>
            <Select
              labelId="date-format-select-label"
              value={dateFormat}
              label={t('settings.dateFormat.label')}
              onChange={handleDateFormatChange}
            >
              <MenuItem value="eu">{t('settings.dateFormat.options.eu')}</MenuItem>
              <MenuItem value="us">{t('settings.dateFormat.options.us')}</MenuItem>
              <MenuItem value="iso">{t('settings.dateFormat.options.iso')}</MenuItem>
              <MenuItem value="ca">{t('settings.dateFormat.options.ca')}</MenuItem>
              <MenuItem value="eu-hyphen">{t('settings.dateFormat.options.eu-hyphen')}</MenuItem>
              <MenuItem value="us-mmm">{t('settings.dateFormat.options.us-mmm')}</MenuItem>
              <MenuItem value="us-mmmm">{t('settings.dateFormat.options.us-mmmm')}</MenuItem>
              <MenuItem value="eu-mmm">{t('settings.dateFormat.options.eu-mmm')}</MenuItem>
              <MenuItem value="eu-mmmm">{t('settings.dateFormat.options.eu-mmmm')}</MenuItem>
            </Select>
          </FormControl>

          <Typography
            variant="caption"
            sx={{
              color: 'text.secondary',
              mt: 1,
              display: 'block',
            }}
          >
            {t('settings.dateFormat.description')}
          </Typography>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <DarkModeIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.theme.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <FormControl fullWidth size="small">
            <InputLabel id="theme-select-label">{t('settings.theme.label')}</InputLabel>
            <Select
              labelId="theme-select-label"
              value={themePreference}
              label={t('settings.theme.label')}
              onChange={handleThemeChange}
            >
              <MenuItem value="system">{t('settings.theme.options.system')}</MenuItem>
              <MenuItem value="light">{t('settings.theme.options.light')}</MenuItem>
              <MenuItem value="dark">{t('settings.theme.options.dark')}</MenuItem>
            </Select>
          </FormControl>

          <Typography
            variant="caption"
            sx={{
              color: 'text.secondary',
              mt: 1,
              display: 'block',
            }}
          >
            {t('settings.theme.description')}
          </Typography>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <PersonIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.selfContact.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <Autocomplete
            size="small"
            options={selfContactOptions}
            getOptionLabel={selfContactName}
            value={selfContact}
            onChange={(_, value) => handleSelfContactSelect(value)}
            onInputChange={(_, value) => setSelfContactSearch(value)}
            filterOptions={(x) => x}
            loading={selfContactLoading}
            isOptionEqualToValue={(a, b) => a.uid === b.uid}
            noOptionsText={t('settings.selfContact.noMatches')}
            renderInput={(params) => (
              <TextField
                {...params}
                label={t('settings.selfContact.label')}
                placeholder={t('settings.selfContact.placeholder')}
              />
            )}
          />
          {selfContact && (
            <Button
              size="small"
              variant="outlined"
              sx={{ mt: 1 }}
              onClick={() => handleSelfContactSelect(null)}
              disabled={selfContactSaving}
            >
              {t('settings.selfContact.clear')}
            </Button>
          )}

          <Typography
            variant="caption"
            sx={{
              color: 'text.secondary',
              mt: 1,
              display: 'block',
            }}
          >
            {t('settings.selfContact.description')}
          </Typography>
        </CardContent>
      </Card>

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <LockResetIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
            <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
              {t('settings.password.title')}
            </Typography>
          </Box>
          <Divider sx={{ mb: 1.5 }} />

          <form onSubmit={handlePasswordChange}>
            <Stack spacing={1.5}>
              <Typography
                variant="body2"
                sx={{
                  color: 'text.secondary',
                }}
              >
                {t('settings.password.description')}
              </Typography>
              {passwordError && (
                <Alert severity="error" sx={{ py: 0 }}>
                  {passwordError}
                </Alert>
              )}
              {passwordSuccess && (
                <Alert severity="success" sx={{ py: 0 }}>
                  {passwordSuccess}
                </Alert>
              )}
              <TextField
                label={t('settings.password.current')}
                type="password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
                fullWidth
                required
                size="small"
              />
              <TextField
                label={t('settings.password.new')}
                type="password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                fullWidth
                required
                size="small"
              />
              <TextField
                label={t('settings.password.confirm')}
                type="password"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
                fullWidth
                required
                size="small"
              />
              <Button type="submit" variant="contained" size="small" disabled={changingPassword}>
                {changingPassword
                  ? t('settings.password.changing')
                  : t('settings.password.changeButton')}
              </Button>
            </Stack>
          </form>
        </CardContent>
      </Card>

      {/* N8: TOTP two-factor auth */}
      <TwoFactorSettings />

      {/* Issue #866: active-session inventory */}
      <SessionsSettings />

      <WebhooksSettings />

      <ImmichSettings />

      <PaperlessSettings />

      <SeafileSettings />

      <NextcloudSettings />

      <NotificationSettings />

      <LinkFieldTypesSettings />

      <Card sx={{ mb: 2 }}>
        <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
          <Box
            sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1 }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center' }}>
              <KeyIcon sx={{ mr: 1, color: 'text.secondary', fontSize: 20 }} />
              <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 500 }}>
                {t('apiTokens.title')}
              </Typography>
            </Box>
            <Stack direction="row" spacing={1}>
              <Button
                variant="outlined"
                size="small"
                color="error"
                startIcon={<BlockIcon />}
                onClick={() => setRevokeAllDialogOpen(true)}
                disabled={activeTokenCount === 0}
              >
                {t('apiTokens.revokeAllButton')}
              </Button>
              <Button
                variant="outlined"
                size="small"
                startIcon={<AddIcon />}
                onClick={() => setCreateDialogOpen(true)}
              >
                {t('apiTokens.createButton')}
              </Button>
            </Stack>
          </Box>
          <Divider sx={{ mb: 1.5 }} />
          {tokensLoading && <CircularProgress />}
          {tokensError && <Alert severity="error">{tokensError}</Alert>}
          {!tokensLoading && !tokensError && (
            /* At narrow (mobile) widths the table overflows and this
               container becomes horizontally scrollable via MUI's own
               overflow-x: auto -- tabIndex + role/aria-label make that
               scrollable region reachable and named for keyboard/screen
               reader users (axe's scrollable-region-focusable, WCAG
               2.1.1/2.1.3; caught by the mobileLayout.spec.ts automatic a11y
               scan, issue #259). */
            <TableContainer
              component={Paper}
              variant="outlined"
              tabIndex={0}
              role="region"
              aria-label={t('apiTokens.title')}
            >
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>{t('apiTokens.columns.name')}</TableCell>
                    <TableCell>{t('apiTokens.columns.scope')}</TableCell>
                    <TableCell>{t('apiTokens.columns.created')}</TableCell>
                    <TableCell>{t('apiTokens.columns.lastUsed')}</TableCell>
                    <TableCell>{t('apiTokens.columns.expires')}</TableCell>
                    <TableCell>{t('apiTokens.columns.status')}</TableCell>
                    <TableCell>{t('apiTokens.columns.actions')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {tokens.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={7} align="center">
                        <Typography
                          sx={{
                            color: 'text.secondary',
                          }}
                        >
                          {t('apiTokens.noTokens')}
                        </Typography>
                      </TableCell>
                    </TableRow>
                  ) : (
                    tokens.map((token) => (
                      <TableRow key={token.id}>
                        <TableCell sx={{ overflowWrap: 'anywhere' }}>{token.name}</TableCell>
                        <TableCell>
                          <Chip
                            label={
                              token.scope === 'carddav'
                                ? t('apiTokens.createDialog.scopeCardDAV')
                                : t('apiTokens.createDialog.scopeFull')
                            }
                            size="small"
                            variant="outlined"
                          />
                        </TableCell>
                        <TableCell>{new Date(token.created_at).toLocaleString()}</TableCell>
                        <TableCell>{formatDate(token.last_used_at)}</TableCell>
                        <TableCell>{formatDate(token.expires_at)}</TableCell>
                        <TableCell>
                          {token.revoked_at ? (
                            <Chip label={t('apiTokens.revoked')} color="error" size="small" />
                          ) : isExpired(token) ? (
                            <Chip label={t('apiTokens.expired')} color="warning" size="small" />
                          ) : (
                            <Chip label={t('apiTokens.active')} color="success" size="small" />
                          )}
                        </TableCell>
                        <TableCell>
                          {isActive(token) && (
                            <Stack direction="row" spacing={0.5}>
                              <Tooltip title={t('apiTokens.rotateDialog.title')}>
                                <IconButton
                                  size="small"
                                  onClick={() => {
                                    setRotatingToken(token);
                                    setRotateDialogOpen(true);
                                  }}
                                  aria-label={t('apiTokens.rotateDialog.title')}
                                >
                                  <AutorenewIcon fontSize="small" />
                                </IconButton>
                              </Tooltip>
                              <Tooltip title={t('apiTokens.revokeDialog.title')}>
                                <IconButton
                                  size="small"
                                  color="error"
                                  onClick={() => {
                                    setRevokingToken(token);
                                    setRevokeDialogOpen(true);
                                  }}
                                  aria-label={t('apiTokens.revokeDialog.title')}
                                >
                                  <BlockIcon fontSize="small" />
                                </IconButton>
                              </Tooltip>
                            </Stack>
                          )}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </CardContent>
      </Card>

      {/* Create token dialog */}
      <AppDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('apiTokens.createDialog.title')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            label={t('apiTokens.createDialog.nameLabel')}
            value={newTokenName}
            onChange={(e) => setNewTokenName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleCreateToken();
            }}
            fullWidth
            margin="normal"
            slotProps={{
              htmlInput: { maxLength: 100 },
            }}
          />
          <TextField
            select
            label={t('apiTokens.createDialog.expiryLabel')}
            value={newTokenExpiryDays}
            onChange={(e) => setNewTokenExpiryDays(Number(e.target.value))}
            fullWidth
            margin="normal"
          >
            {API_TOKEN_EXPIRY_OPTIONS.map((days) => (
              <MenuItem key={days} value={days}>
                {t('apiTokens.createDialog.expiryDays', { days })}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label={t('apiTokens.createDialog.scopeLabel')}
            value={newTokenScope}
            onChange={(e) => setNewTokenScope(e.target.value as ApiTokenScope)}
            helperText={
              newTokenScope === 'carddav'
                ? t('apiTokens.createDialog.scopeCardDAVHelp')
                : t('apiTokens.createDialog.scopeFullHelp')
            }
            fullWidth
            margin="normal"
          >
            <MenuItem value="full">{t('apiTokens.createDialog.scopeFull')}</MenuItem>
            <MenuItem value="carddav">{t('apiTokens.createDialog.scopeCardDAV')}</MenuItem>
          </TextField>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateDialogOpen(false)} disabled={createLoading}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="contained"
            onClick={handleCreateToken}
            disabled={createLoading || !newTokenName.trim()}
          >
            {t('apiTokens.createDialog.createButton')}
          </Button>
        </DialogActions>
      </AppDialog>

      {/* Token created dialog -- shared by create and rotate, since rotate
          also mints a fresh plaintext that's only shown this once. */}
      <Dialog
        open={!!createdToken}
        onClose={() => {
          setCreatedToken(null);
          setCreatedTokenIsRotation(false);
        }}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>
          {createdTokenIsRotation
            ? t('apiTokens.rotatedDialog.title')
            : t('apiTokens.createdDialog.title')}
        </DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            {createdTokenIsRotation
              ? t('apiTokens.rotatedDialog.warning')
              : t('apiTokens.createdDialog.warning')}
          </Alert>
          <Stack
            direction="row"
            spacing={1}
            sx={{
              alignItems: 'center',
            }}
          >
            <TextField
              value={createdToken?.token || ''}
              fullWidth
              size="small"
              slotProps={{
                input: { readOnly: true },
                htmlInput: { style: { fontFamily: 'monospace', fontSize: '0.85rem' } },
              }}
            />
            <Tooltip
              title={
                copied ? t('apiTokens.createdDialog.copied') : t('apiTokens.createdDialog.copy')
              }
            >
              <IconButton
                onClick={handleCopy}
                color={copied ? 'success' : 'default'}
                aria-label={
                  copied ? t('apiTokens.createdDialog.copied') : t('apiTokens.createdDialog.copy')
                }
              >
                <ContentCopyIcon />
              </IconButton>
            </Tooltip>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button
            variant="contained"
            onClick={() => {
              setCreatedToken(null);
              setCreatedTokenIsRotation(false);
            }}
          >
            {t('apiTokens.createdDialog.done')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Revoke confirmation dialog */}
      <Dialog open={revokeDialogOpen} onClose={() => setRevokeDialogOpen(false)}>
        <DialogTitle>{t('apiTokens.revokeDialog.title')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('apiTokens.revokeDialog.message', { name: revokingToken?.name || '' })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRevokeDialogOpen(false)} disabled={revokeLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="contained" color="error" onClick={handleRevoke} disabled={revokeLoading}>
            {t('apiTokens.revokeDialog.confirm')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Revoke-all confirmation dialog (issue #572) */}
      <Dialog open={revokeAllDialogOpen} onClose={() => setRevokeAllDialogOpen(false)}>
        <DialogTitle>{t('apiTokens.revokeAllDialog.title')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('apiTokens.revokeAllDialog.message', { count: activeTokenCount })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRevokeAllDialogOpen(false)} disabled={revokeAllLoading}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="contained"
            color="error"
            onClick={handleRevokeAll}
            disabled={revokeAllLoading || activeTokenCount === 0}
          >
            {t('apiTokens.revokeAllDialog.confirm')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Rotate confirmation dialog (issue #572) */}
      <Dialog open={rotateDialogOpen} onClose={() => setRotateDialogOpen(false)}>
        <DialogTitle>{t('apiTokens.rotateDialog.title')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('apiTokens.rotateDialog.message', { name: rotatingToken?.name || '' })}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRotateDialogOpen(false)} disabled={rotateLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="contained" onClick={handleRotate} disabled={rotateLoading}>
            {t('apiTokens.rotateDialog.confirm')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Which build is running -- what a user quotes in a bug report. */}
      <BuildVersionCard />

      {/* The full operational snapshot lives on its own admin page (issue
          #649); admins get a cross-link from here, non-admins never see it. */}
      {isAdmin() && (
        <Box sx={{ mb: 2 }}>
          <Link component={RouterLink} to="/system-status">
            {t('settings.about.viewSystemStatus')}
          </Link>
        </Box>
      )}
    </Box>
  );
}
