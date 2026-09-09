import DevicesIcon from '@mui/icons-material/Devices';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSessions, revokeOtherSessions, revokeSession, type Session } from '../api/sessions';
import { useSnackbar } from '../context/SnackbarContext';

// SessionsSettings is the settings-page card for issue #866 (ASVS 3.3.4): the
// list of this account's active server-side sessions, each revocable, plus a
// "log out everywhere else" action. Revoking the current session logs this
// device out on its next request.
export default function SessionsSettings() {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();

  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);
  const [revokingOthers, setRevokingOthers] = useState(false);

  const refresh = async () => {
    try {
      const response = await getSessions();
      setSessions(response.sessions);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : t('sessions.loadError'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRevoke = async (session: Session) => {
    setBusyId(session.id);
    try {
      await revokeSession(session.id);
      showSuccess(
        session.current ? t('sessions.revokeCurrentSuccess') : t('sessions.revokeSuccess'),
      );
      await refresh();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('sessions.revokeError'));
    } finally {
      setBusyId(null);
    }
  };

  const handleRevokeOthers = async () => {
    setRevokingOthers(true);
    try {
      const result = await revokeOtherSessions();
      showSuccess(t('sessions.revokeOthersSuccess', { count: result.revoked }));
      await refresh();
    } catch (err) {
      showError(err instanceof Error ? err.message : t('sessions.revokeOthersError'));
    } finally {
      setRevokingOthers(false);
    }
  };

  const formatWhen = (iso: string) => new Date(iso).toLocaleString();

  const otherCount = (sessions ?? []).filter((s) => !s.current).length;

  return (
    <Card sx={{ mt: 3 }}>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <DevicesIcon color="action" />
          <Typography variant="h6">{t('sessions.title')}</Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('sessions.description')}
        </Typography>

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
            <CircularProgress size={24} />
          </Box>
        ) : error ? (
          <Alert severity="error">{error}</Alert>
        ) : sessions && sessions.length > 0 ? (
          <>
            <Box sx={{ overflowX: 'auto' }}>
              <Table size="small" aria-label={t('sessions.title')}>
                <TableHead>
                  <TableRow>
                    <TableCell>{t('sessions.device')}</TableCell>
                    <TableCell>{t('sessions.lastSeen')}</TableCell>
                    <TableCell>{t('sessions.signedIn')}</TableCell>
                    <TableCell align="right">{t('sessions.actions')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {sessions.map((session) => (
                    <TableRow key={session.id}>
                      <TableCell>
                        <Box
                          sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}
                        >
                          <span>{session.user_agent || t('sessions.unknownDevice')}</span>
                          {session.current && (
                            <Chip size="small" color="primary" label={t('sessions.thisDevice')} />
                          )}
                        </Box>
                        {session.ip && (
                          <Typography variant="caption" color="text.secondary">
                            {session.ip}
                          </Typography>
                        )}
                      </TableCell>
                      <TableCell>{formatWhen(session.last_seen_at)}</TableCell>
                      <TableCell>{formatWhen(session.created_at)}</TableCell>
                      <TableCell align="right">
                        <Button
                          size="small"
                          color="error"
                          disabled={busyId === session.id}
                          onClick={() => handleRevoke(session)}
                        >
                          {session.current ? t('sessions.revokeCurrent') : t('sessions.revoke')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>
            {otherCount > 0 && (
              <Box sx={{ mt: 2 }}>
                <Button
                  variant="outlined"
                  color="error"
                  disabled={revokingOthers}
                  onClick={handleRevokeOthers}
                >
                  {t('sessions.revokeOthersButton')}
                </Button>
              </Box>
            )}
          </>
        ) : (
          <Typography variant="body2" color="text.secondary">
            {t('sessions.empty')}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}
