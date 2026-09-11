import WarningIcon from '@mui/icons-material/Warning';
import {
  Alert,
  Avatar,
  Box,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';
import type { OverdueCadence } from '../api/cadencePolicies';
import { useDateFormat } from '../DateFormatProvider';

interface OverdueCadenceListProps {
  overdue: OverdueCadence[];
  loading: boolean;
  error: string | null;
}

// The overdue screen ("who have I not talked to in too long") -- the reason
// personal-relationship CRMs exist. Renders as a dashboard section; each row
// links to the contact. Contact-name fallback to the policy's entity uid
// keeps a row usable even if the contact was hard-purged out from under a
// stale policy.
export default function OverdueCadenceList({ overdue, loading, error }: OverdueCadenceListProps) {
  const { t } = useTranslation();
  const { formatDate } = useDateFormat();

  if (error) {
    return <Alert severity="error">{error}</Alert>;
  }

  return (
    <Box>
      <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
        <WarningIcon color="warning" fontSize="small" />
        <Typography
          variant="subtitle1"
          component="h2"
          sx={{
            fontWeight: 500,
          }}
        >
          {t('cadence.overdueTitle')}
        </Typography>
      </Box>

      {loading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
          <CircularProgress size={24} />
        </Box>
      ) : overdue.length === 0 ? (
        <Card>
          <CardContent sx={{ py: 2 }}>
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
              }}
            >
              {t('cadence.noOverdue')}
            </Typography>
          </CardContent>
        </Card>
      ) : (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          {overdue.map((item) => (
            <Card
              key={item.policy.id}
              component={Link}
              to={`/contacts/${item.contact_id}`}
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
                    src={item.photo_thumbnail || undefined}
                    sx={{ bgcolor: 'warning.main', width: 40, height: 40 }}
                  >
                    {(item.contact_name || item.policy.entity_id).charAt(0).toUpperCase()}
                  </Avatar>
                  <Box sx={{ flexGrow: 1 }}>
                    <Typography
                      variant="body2"
                      sx={{
                        fontWeight: 500,
                      }}
                    >
                      {item.contact_name || t('cadence.unknownContact')}
                    </Typography>
                    {item.health.next_due && (
                      <Typography
                        variant="caption"
                        sx={{
                          color: 'text.secondary',
                        }}
                      >
                        {t('cadence.dueOn', { date: formatDate(item.health.next_due) })}
                      </Typography>
                    )}
                  </Box>
                  <Chip
                    icon={<WarningIcon fontSize="small" />}
                    label={t('cadence.overdueBy', { days: item.health.overdue_by })}
                    size="small"
                    color="warning"
                    sx={{ height: 20, fontSize: '0.7rem' }}
                  />
                </Box>
              </CardContent>
            </Card>
          ))}
        </Box>
      )}
    </Box>
  );
}
