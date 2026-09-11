import BadgeIcon from '@mui/icons-material/Badge';
import BusinessIcon from '@mui/icons-material/Business';
import CloseIcon from '@mui/icons-material/Close';
import HomeIcon from '@mui/icons-material/Home';
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
import type { ElementType } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';
import type { ReachOutSuggestion, ReachOutSuggestionKind } from '../api/reachOutSuggestions';

interface ReachOutSuggestionsListProps {
  suggestions: ReachOutSuggestion[];
  loading: boolean;
  error: string | null;
  onDismiss: (id: string) => void;
}

const kindIcon: Record<ReachOutSuggestionKind, ElementType> = {
  organization: BusinessIcon,
  title: BadgeIcon,
  address: HomeIcon,
};

// The event-driven counterpart to OverdueCadenceList (issue #177): "here's a
// concrete reason to reach out" (a job/title/address change), rather than a
// generic "it's been a while." Same dashboard-section shape as
// OverdueCadenceList -- each row links to the contact -- plus a dismiss
// action, since these are propose-then-approve suggestions, not facts.
export default function ReachOutSuggestionsList({
  suggestions,
  loading,
  error,
  onDismiss,
}: ReachOutSuggestionsListProps) {
  const { t } = useTranslation();

  if (error) {
    return <Alert severity="error">{error}</Alert>;
  }

  return (
    <Box>
      <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
        <BusinessIcon color="primary" fontSize="small" />
        <Typography
          variant="subtitle1"
          component="h2"
          sx={{
            fontWeight: 500,
          }}
        >
          {t('reachOut.title')}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
          <CircularProgress size={24} />
        </Box>
      ) : suggestions.length === 0 ? (
        <Card>
          <CardContent sx={{ py: 2 }}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('reachOut.empty')}
            </Typography>
          </CardContent>
        </Card>
      ) : (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          {suggestions.map((s) => {
            const KindIcon = kindIcon[s.kind];
            return (
              <Card
                key={s.id}
                component={Link}
                to={`/contacts/${s.contact_id}`}
                sx={{
                  textDecoration: 'none',
                  border: '1px solid',
                  borderColor: 'primary.main',
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
                      src={s.photo_thumbnail || undefined}
                      sx={{ bgcolor: 'primary.main', width: 40, height: 40 }}
                    >
                      {(s.contact_name || '?').charAt(0).toUpperCase()}
                    </Avatar>
                    <Box sx={{ flexGrow: 1 }}>
                      <Typography
                        variant="body2"
                        sx={{
                          fontWeight: 500,
                        }}
                      >
                        {s.contact_name || t('cadence.unknownContact')}
                      </Typography>
                      <Typography
                        variant="caption"
                        sx={{
                          color: 'text.secondary',
                        }}
                      >
                        {s.old_value
                          ? t('reachOut.changedFromTo', { old: s.old_value, new: s.new_value })
                          : t('reachOut.changedTo', { new: s.new_value })}
                      </Typography>
                    </Box>
                    <Chip
                      icon={<KindIcon fontSize="small" />}
                      label={t(`reachOut.kind.${s.kind}`)}
                      size="small"
                      color="primary"
                      variant="outlined"
                      sx={{ height: 20, fontSize: '0.7rem' }}
                    />
                    <Tooltip title={t('reachOut.dismiss')}>
                      <IconButton
                        size="small"
                        aria-label={t('reachOut.dismiss')}
                        onClick={(e) => {
                          e.preventDefault();
                          e.stopPropagation();
                          onDismiss(s.id);
                        }}
                      >
                        <CloseIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Box>
                </CardContent>
              </Card>
            );
          })}
        </Box>
      )}
    </Box>
  );
}
