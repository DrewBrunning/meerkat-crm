import CloseIcon from '@mui/icons-material/Close';
import RestoreIcon from '@mui/icons-material/Restore';
import SyncProblemIcon from '@mui/icons-material/SyncProblem';
import {
  Alert,
  Avatar,
  Box,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  IconButton,
  Tooltip,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';
import type { ContactSyncConflict } from '../api/contactSyncConflicts';

interface SyncConflictListProps {
  conflicts: ContactSyncConflict[];
  loading: boolean;
  error: string | null;
  onRestore: (conflict: ContactSyncConflict) => void;
  onDismiss: (id: string) => void;
}

// valueToLabel turns a stored conflict value into something human-readable.
// Array fields (email/phone/address/url/impp/circles) are stored as their
// JSON array; scalars are plain strings. A blank value renders as an em dash.
function valueToLabel(value: string): string {
  if (!value) return '—';
  try {
    const parsed = JSON.parse(value);
    if (Array.isArray(parsed)) {
      if (parsed.length === 0) return '—';
      return parsed
        .map((entry) => {
          if (typeof entry === 'string') return entry;
          return entry.value ?? entry.full ?? entry.street ?? entry.name ?? '';
        })
        .filter(Boolean)
        .join(', ');
    }
  } catch {
    // not JSON — a plain scalar string
  }
  return value;
}

// The usability half of issue #395: a CardDAV sync overwrote a local edit
// (full-replace by design), and this is the only record of what was lost.
// Each row names the field and offers the local value back via restore, or
// lets the user accept the remote value via dismiss. Same dashboard-section
// shape as ReachOutSuggestionsList; rows link to the contact.
export default function SyncConflictList({
  conflicts,
  loading,
  error,
  onRestore,
  onDismiss,
}: SyncConflictListProps) {
  const { t } = useTranslation();

  if (error) {
    return <Alert severity="error">{error}</Alert>;
  }

  return (
    <Box>
      <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
        <SyncProblemIcon color="warning" fontSize="small" />
        <Typography
          variant="subtitle1"
          component="h2"
          sx={{
            fontWeight: 500,
          }}
        >
          {t('syncConflicts.title')}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
          <CircularProgress size={24} />
        </Box>
      ) : conflicts.length === 0 ? (
        <Card>
          <CardContent sx={{ py: 2 }}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('syncConflicts.empty')}
            </Typography>
          </CardContent>
        </Card>
      ) : (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          {conflicts.map((conflict) => (
            <Card
              key={conflict.id}
              component={Link}
              to={`/contacts/${conflict.contact_id}`}
              sx={{
                textDecoration: 'none',
                border: '1px solid',
                borderColor: 'warning.main',
                '&:hover': {
                  boxShadow: 2,
                  transform: 'translateY(-1px)',
                  transition: 'all 0.2s',
                },
              }}
            >
              <CardContent sx={{ py: 1.5 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                  {/* #196: decorative -- aria-hidden so the initial doesn't
                      double into the row link's accessible name. */}
                  <Avatar
                    aria-hidden
                    src={conflict.photo_thumbnail || undefined}
                    sx={{ bgcolor: 'warning.main', width: 40, height: 40 }}
                  >
                    {(conflict.contact_name || '?').charAt(0).toUpperCase()}
                  </Avatar>
                  <Box sx={{ flexGrow: 1 }}>
                    <Typography
                      variant="body2"
                      sx={{
                        fontWeight: 500,
                      }}
                    >
                      {conflict.contact_name || t('cadence.unknownContact')}
                    </Typography>
                    <Typography
                      variant="caption"
                      sx={{
                        color: 'text.secondary',
                      }}
                    >
                      {t('syncConflicts.overwritten', {
                        field: t(`syncConflicts.field.${conflict.field}`),
                        local: valueToLabel(conflict.local_value),
                        remote: valueToLabel(conflict.remote_value),
                      })}
                    </Typography>
                  </Box>
                  <Chip
                    icon={<SyncProblemIcon fontSize="small" />}
                    label={t(`syncConflicts.field.${conflict.field}`)}
                    size="small"
                    color="warning"
                    variant="outlined"
                    sx={{ height: 20, fontSize: '0.7rem' }}
                  />
                  <Tooltip title={t('syncConflicts.restore')}>
                    <IconButton
                      size="small"
                      color="primary"
                      aria-label={t('syncConflicts.restore')}
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        onRestore(conflict);
                      }}
                    >
                      <RestoreIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title={t('syncConflicts.dismiss')}>
                    <IconButton
                      size="small"
                      aria-label={t('syncConflicts.dismiss')}
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        onDismiss(conflict.id);
                      }}
                    >
                      <CloseIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </Box>
              </CardContent>
            </Card>
          ))}
        </Box>
      )}
    </Box>
  );
}
