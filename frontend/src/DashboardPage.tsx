import CakeIcon from '@mui/icons-material/Cake';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import EmailIcon from '@mui/icons-material/Email';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import NotificationsIcon from '@mui/icons-material/Notifications';
import RepeatIcon from '@mui/icons-material/Repeat';
import ShuffleIcon from '@mui/icons-material/Shuffle';
import SkipNextIcon from '@mui/icons-material/SkipNext';
import StarIcon from '@mui/icons-material/Star';
import WarningIcon from '@mui/icons-material/Warning';
import {
  Alert,
  Avatar,
  Box,
  Card,
  CardContent,
  Chip,
  IconButton,
  Popover,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router';
import type { OverdueCadence } from './api/cadencePolicies';
import {
  type ContactSyncConflict,
  dismissContactSyncConflict,
  restoreContactSyncConflict,
} from './api/contactSyncConflicts';
import type { Birthday, Contact } from './api/contacts';
import { type DashboardReminder, getDashboard } from './api/dashboard';
import { dismissReachOutSuggestion, type ReachOutSuggestion } from './api/reachOutSuggestions';
import { completeReminder, getUpcomingReminders, skipReminder } from './api/reminders';
import { ContactListSkeleton } from './components/LoadingSkeletons';
import OverdueCadenceList from './components/OverdueCadenceList';
import ReachOutSuggestionsList from './components/ReachOutSuggestionsList';
import SyncConflictList from './components/SyncConflictList';
import { useDateFormat } from './DateFormatProvider';
import { useCircles } from './hooks/useCircles';
import { useDocumentTitle } from './hooks/useDocumentTitle';
import { handleError, handleFetchError } from './utils/errorHandler';

function DashboardPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('nav.dashboard'));
  const navigate = useNavigate();
  const { formatBirthday: formatBirthdayDate, formatDate } = useDateFormat();
  const [birthdays, setBirthdays] = useState<Birthday[]>([]);
  const [randomContacts, setRandomContacts] = useState<Contact[]>([]);
  // Issue #173: the favorites quick-access block.
  const [favoriteContacts, setFavoriteContacts] = useState<Contact[]>([]);
  const [upcomingReminders, setUpcomingReminders] = useState<DashboardReminder[]>([]);
  const [overdueCadences, setOverdueCadences] = useState<OverdueCadence[]>([]);
  const [reachOutSuggestions, setReachOutSuggestions] = useState<ReachOutSuggestion[]>([]);
  const [syncConflicts, setSyncConflicts] = useState<ContactSyncConflict[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [birthdaysInfoAnchor, setBirthdaysInfoAnchor] = useState<HTMLElement | null>(null);
  const [remindersInfoAnchor, setRemindersInfoAnchor] = useState<HTMLElement | null>(null);
  const [stayInTouchInfoAnchor, setStayInTouchInfoAnchor] = useState<HTMLElement | null>(null);
  const [favoritesInfoAnchor, setFavoritesInfoAnchor] = useState<HTMLElement | null>(null);

  const { circleNamesByUid } = useCircles();

  const loadDashboardData = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      // M3: one composite call replaces the birthdays/random-contacts/
      // upcoming-reminders/overdue-cadences fan-out, plus the per-reminder
      // contact lookup that used to follow it — the backend now embeds each
      // reminder's contact_name directly.
      const dashboard = await getDashboard();

      setBirthdays(dashboard.birthdays);
      setRandomContacts(dashboard.random_contacts);
      setFavoriteContacts(dashboard.favorites);
      setUpcomingReminders(dashboard.upcoming_reminders);
      setOverdueCadences(dashboard.overdue);
      setReachOutSuggestions(dashboard.reach_out_suggestions);
      setSyncConflicts(dashboard.contact_sync_conflicts);
    } catch (err) {
      const message = handleFetchError(err, 'loading dashboard data');
      setError(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadDashboardData();
  }, [loadDashboardData]);

  // attachKnownContactNames carries contact_name forward from the current
  // dashboard state onto a plain Reminder[] refetch (getUpcomingReminders
  // has no contact_name -- that's a dashboard-only enrichment, M3 design
  // decision 2). This refetch is deliberately kept as the plain endpoint
  // (interaction path, unrelated to the M3 composite) rather than
  // re-fetching the whole dashboard for one completed/skipped reminder.
  const attachKnownContactNames = (
    prev: DashboardReminder[],
    reminders: Awaited<ReturnType<typeof getUpcomingReminders>>,
  ): DashboardReminder[] => {
    const nameById = new Map(prev.map((r) => [r.ID, r.contact_name]));
    return reminders.map((r) => ({ ...r, contact_name: nameById.get(r.ID) || '' }));
  };

  const handleCompleteReminder = async (reminderId: number) => {
    try {
      await completeReminder(reminderId);
      // Reload reminders after completion
      const reminders = await getUpcomingReminders();
      setUpcomingReminders((prev) => attachKnownContactNames(prev, reminders));
    } catch (err) {
      handleError(err, { operation: 'completing reminder' });
    }
  };

  const handleSkipReminder = async (reminderId: number) => {
    if (!window.confirm(t('reminders.skipConfirm'))) {
      return;
    }
    try {
      await skipReminder(reminderId);
      // Reload reminders after skipping
      const reminders = await getUpcomingReminders();
      setUpcomingReminders((prev) => attachKnownContactNames(prev, reminders));
    } catch (err) {
      handleError(err, { operation: 'skipping reminder' });
    }
  };

  const handleDismissReachOutSuggestion = async (id: string) => {
    try {
      await dismissReachOutSuggestion(id);
      setReachOutSuggestions((prev) => prev.filter((s) => s.id !== id));
    } catch (err) {
      handleError(err, { operation: 'dismissing reach-out suggestion' });
    }
  };

  // Issue #395: a CardDAV sync overwrote a local edit. Restore re-applies the
  // local value; dismiss acknowledges the remote one. Both remove the notice.
  const handleRestoreSyncConflict = async (conflict: ContactSyncConflict) => {
    if (!window.confirm(t('syncConflicts.restoreConfirm'))) {
      return;
    }
    try {
      await restoreContactSyncConflict(conflict.id);
      setSyncConflicts((prev) => prev.filter((c) => c.id !== conflict.id));
    } catch (err) {
      handleError(err, { operation: 'restoring sync conflict' });
    }
  };

  const handleDismissSyncConflict = async (id: string) => {
    try {
      await dismissContactSyncConflict(id);
      setSyncConflicts((prev) => prev.filter((c) => c.id !== id));
    } catch (err) {
      handleError(err, { operation: 'dismissing sync conflict' });
    }
  };

  const isOverdue = (remindAt: string) => {
    return new Date(remindAt) < new Date();
  };

  const isBirthdayToday = (birthday: string | undefined) => {
    if (!birthday) return false;
    // Birthday is either "YYYY-MM-DD" or "--MM-DD"; compare month and day to today
    const parts = birthday.split('-').filter((p) => p !== '');
    if (parts.length < 2) return false;
    const month = parseInt(parts[parts.length - 2], 10);
    const day = parseInt(parts[parts.length - 1], 10);
    if (Number.isNaN(month) || Number.isNaN(day)) return false;
    const today = new Date();
    return today.getMonth() + 1 === month && today.getDate() === day;
  };

  const formatBirthday = (birthday: string | undefined) => {
    if (!birthday) return '';

    // Use the date format provider's birthday formatter
    const formattedDate = formatBirthdayDate(birthday);

    // Check if year is present to calculate age
    if (!birthday.startsWith('--')) {
      const parts = birthday.split('-');
      if (parts.length === 3 && parts[0].length === 4) {
        const birthYear = parseInt(parts[0], 10);
        if (!Number.isNaN(birthYear)) {
          const currentYear = new Date().getFullYear();
          const age = currentYear - birthYear;
          // Remove the year from the formatted date for dashboard display (just show DD.MM. or MM/DD)
          const dateWithoutYear = formatBirthdayDate(`--${parts[1]}-${parts[2]}`);
          return `${dateWithoutYear} ${t('dashboard.yearsOld', { age })}`;
        }
      }
    }

    return formattedDate;
  };

  const getContactName = (contact: Contact) => {
    if (contact.nickname) return `${contact.nickname} ${contact.lastname}`;
    return `${contact.firstname} ${contact.lastname}`;
  };

  if (loading) {
    return (
      <Box sx={{ maxWidth: 1400, mx: 'auto', mt: 2, p: 2 }}>
        <Typography variant="h5" component="h1" gutterBottom>
          {t('dashboard.title')}
        </Typography>
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', md: 'repeat(4, 1fr)' },
            gap: 3,
          }}
        >
          <Box>
            <ContactListSkeleton count={5} />
          </Box>
          <Box>
            <ContactListSkeleton count={5} />
          </Box>
          <Box>
            <ContactListSkeleton count={5} />
          </Box>
          <Box>
            <ContactListSkeleton count={5} />
          </Box>
        </Box>
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ maxWidth: 1400, mx: 'auto', mt: 2, p: 2 }}>
        <Alert severity="error">{error}</Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ maxWidth: 1400, mx: 'auto', mt: 2, p: 2 }}>
      <Typography variant="h5" component="h1" gutterBottom sx={{ mb: 2 }}>
        {t('dashboard.title')}
      </Typography>

      {/* Overdue cadences (T19) — the screen people actually live in. Only
          rendered when there is something to show; an all-clear dashboard
          stays clean. */}
      {overdueCadences.length > 0 && (
        <Box sx={{ mb: 3 }}>
          <OverdueCadenceList overdue={overdueCadences} loading={loading} error={null} />
        </Box>
      )}

      {/* Event-driven reach-out suggestions (issue #177) — an org/title/
          address change detected on a contact. Only rendered when there is
          something to show, same "all-clear dashboard stays clean" rule as
          OverdueCadenceList above. */}
      {reachOutSuggestions.length > 0 && (
        <Box sx={{ mb: 3 }}>
          <ReachOutSuggestionsList
            suggestions={reachOutSuggestions}
            loading={loading}
            error={null}
            onDismiss={handleDismissReachOutSuggestion}
          />
        </Box>
      )}

      {/* CardDAV sync conflicts (issue #395) — a remote change overwrote a
          local edit; the user can restore the local value or accept the
          remote one. Only rendered when there is something to show, same
          "all-clear dashboard stays clean" rule as the blocks above. */}
      {syncConflicts.length > 0 && (
        <Box sx={{ mb: 3 }}>
          <SyncConflictList
            conflicts={syncConflicts}
            loading={loading}
            error={null}
            onRestore={handleRestoreSyncConflict}
            onDismiss={handleDismissSyncConflict}
          />
        </Box>
      )}

      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', md: 'repeat(4, 1fr)' },
          gap: 2,
        }}
      >
        {/* Issue #173: Column 1 — Favorites (quick access). */}
        <Box>
          <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
            <StarIcon color="primary" fontSize="small" />
            <Typography
              variant="subtitle1"
              component="h2"
              sx={{
                fontWeight: 500,
              }}
            >
              {t('dashboard.favorites')}
            </Typography>
            <IconButton
              size="small"
              onClick={(e) => setFavoritesInfoAnchor(e.currentTarget)}
              aria-label={t('common.info')}
            >
              <InfoOutlinedIcon fontSize="small" />
            </IconButton>
          </Box>
          <Popover
            open={Boolean(favoritesInfoAnchor)}
            anchorEl={favoritesInfoAnchor}
            onClose={() => setFavoritesInfoAnchor(null)}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
          >
            <Box sx={{ p: 2, maxWidth: 320 }}>
              <Typography variant="body2">{t('dashboard.favoritesInfo')}</Typography>
            </Box>
          </Popover>

          {favoriteContacts.length === 0 ? (
            <Card>
              <CardContent sx={{ py: 2 }}>
                <Typography
                  variant="body2"
                  sx={{
                    color: 'text.secondary',
                  }}
                >
                  {t('dashboard.noFavorites')}
                </Typography>
              </CardContent>
            </Card>
          ) : (
            <Stack spacing={1.5}>
              {favoriteContacts.map((contact) => (
                <Card
                  key={contact.ID}
                  component={Link}
                  to={`/contacts/${contact.ID}`}
                  sx={{
                    textDecoration: 'none',
                    '&:hover': {
                      boxShadow: 2,
                      transform: 'translateY(-1px)',
                      transition: 'all 0.2s',
                    },
                  }}
                >
                  <CardContent sx={{ py: 1.5 }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                      {/* #196: decorative -- the name is in the text beside it.
                          Without aria-hidden the initial doubles into the
                          card link's accessible name ("AAlice Johnson"). */}
                      <Avatar
                        aria-hidden
                        src={contact.photo_thumbnail || undefined}
                        sx={{ bgcolor: 'warning.main', width: 40, height: 40 }}
                      >
                        {contact.firstname.charAt(0)}
                      </Avatar>
                      <Box sx={{ flexGrow: 1 }}>
                        <Typography
                          variant="body2"
                          sx={{
                            fontWeight: 500,
                          }}
                        >
                          {getContactName(contact)}
                        </Typography>
                        {(circleNamesByUid.get(contact.uid || '') || []).length > 0 && (
                          <Box sx={{ mt: 0.5 }}>
                            {(circleNamesByUid.get(contact.uid || '') || [])
                              .slice(0, 2)
                              .map((circle, idx) => (
                                <Chip
                                  // biome-ignore lint/suspicious/noArrayIndexKey: sliced lookup, no stable id
                                  key={idx}
                                  label={circle}
                                  size="small"
                                  variant="outlined"
                                  sx={{ mr: 0.5, height: 20, fontSize: '0.7rem' }}
                                />
                              ))}
                          </Box>
                        )}
                      </Box>
                    </Box>
                  </CardContent>
                </Card>
              ))}
            </Stack>
          )}
        </Box>

        {/* Column 2: Upcoming Birthdays */}
        <Box>
          <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
            <CakeIcon color="primary" fontSize="small" />
            <Typography
              variant="subtitle1"
              component="h2"
              sx={{
                fontWeight: 500,
              }}
            >
              {t('dashboard.upcomingBirthdays')}
            </Typography>
            <IconButton
              size="small"
              onClick={(e) => setBirthdaysInfoAnchor(e.currentTarget)}
              aria-label={t('common.info')}
            >
              <InfoOutlinedIcon fontSize="small" />
            </IconButton>
          </Box>
          <Popover
            open={Boolean(birthdaysInfoAnchor)}
            anchorEl={birthdaysInfoAnchor}
            onClose={() => setBirthdaysInfoAnchor(null)}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
          >
            <Box sx={{ p: 2, maxWidth: 320 }}>
              <Typography variant="body2">{t('dashboard.birthdaysInfo')}</Typography>
            </Box>
          </Popover>

          {birthdays.length === 0 ? (
            <Card>
              <CardContent sx={{ py: 2 }}>
                <Typography
                  variant="body2"
                  sx={{
                    color: 'text.secondary',
                  }}
                >
                  {t('dashboard.noBirthdays')}
                </Typography>
              </CardContent>
            </Card>
          ) : (
            <Stack spacing={1.5}>
              {birthdays.map((birthday, index) => {
                const today = isBirthdayToday(birthday.birthday);

                return (
                  <Card
                    // biome-ignore lint/suspicious/noArrayIndexKey: contact_id may repeat across sections
                    key={`${birthday.type}-${birthday.contact_id}-${index}`}
                    component={Link}
                    to={`/contacts/${birthday.contact_id}`}
                    sx={{
                      textDecoration: 'none',
                      border: '1px solid',
                      borderColor: today ? 'success.main' : 'divider',
                      '&:hover': {
                        boxShadow: 2,
                        transform: 'translateY(-1px)',
                        transition: 'all 0.2s',
                      },
                    }}
                  >
                    <CardContent sx={{ py: 1.5 }}>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                        {/* #196: decorative -- see the favorites Avatar above. */}
                        <Avatar
                          aria-hidden
                          src={birthday.photo_thumbnail}
                          sx={{ bgcolor: 'primary.main', width: 40, height: 40 }}
                        >
                          {birthday.name.charAt(0)}
                        </Avatar>
                        <Box sx={{ flexGrow: 1 }}>
                          <Typography
                            variant="body2"
                            sx={{
                              fontWeight: 500,
                            }}
                          >
                            {birthday.name}
                          </Typography>
                          <Typography
                            variant="caption"
                            sx={{
                              color: 'text.secondary',
                            }}
                          >
                            {formatBirthday(birthday.birthday)}
                          </Typography>
                        </Box>
                      </Box>
                    </CardContent>
                  </Card>
                );
              })}
            </Stack>
          )}
        </Box>

        {/* Column 2: Upcoming Reminders */}
        <Box>
          <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
            <NotificationsIcon color="primary" fontSize="small" />
            <Typography
              variant="subtitle1"
              component="h2"
              sx={{
                fontWeight: 500,
              }}
            >
              {t('dashboard.upcomingReminders')}
            </Typography>
            <IconButton
              size="small"
              onClick={(e) => setRemindersInfoAnchor(e.currentTarget)}
              aria-label={t('common.info')}
            >
              <InfoOutlinedIcon fontSize="small" />
            </IconButton>
          </Box>
          <Popover
            open={Boolean(remindersInfoAnchor)}
            anchorEl={remindersInfoAnchor}
            onClose={() => setRemindersInfoAnchor(null)}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
          >
            <Box sx={{ p: 2, maxWidth: 320 }}>
              <Typography variant="body2">{t('dashboard.remindersInfo')}</Typography>
            </Box>
          </Popover>

          {upcomingReminders.length === 0 ? (
            <Card>
              <CardContent sx={{ py: 2 }}>
                <Typography
                  variant="body2"
                  sx={{
                    color: 'text.secondary',
                  }}
                >
                  {t('dashboard.noReminders')}
                </Typography>
              </CardContent>
            </Card>
          ) : (
            <Stack spacing={1.5}>
              {upcomingReminders.map((reminder) => {
                const overdue = isOverdue(reminder.remind_at);

                return (
                  <Card
                    key={reminder.ID}
                    sx={{
                      border: '1px solid',
                      borderColor: overdue ? 'warning.main' : 'divider',
                      cursor: 'pointer',
                      '&:hover': {
                        boxShadow: 2,
                        transform: 'translateY(-1px)',
                        transition: 'all 0.2s',
                      },
                    }}
                    onClick={() => navigate(`/contacts/${reminder.contact_id}`)}
                  >
                    <CardContent sx={{ py: 1.5 }}>
                      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
                        <Box sx={{ flexGrow: 1 }}>
                          <Typography variant="body2" sx={{ fontWeight: 500 }}>
                            {reminder.message}
                          </Typography>
                          {reminder.contact_name && (
                            <Typography
                              variant="caption"
                              sx={{
                                color: 'text.secondary',
                              }}
                            >
                              {reminder.contact_name}
                            </Typography>
                          )}
                          <Box
                            sx={{
                              mt: 0.75,
                              display: 'flex',
                              gap: 0.5,
                              flexWrap: 'wrap',
                              alignItems: 'center',
                            }}
                          >
                            <Chip
                              icon={overdue ? <WarningIcon fontSize="small" /> : undefined}
                              label={formatDate(reminder.remind_at)}
                              size="small"
                              color={overdue ? 'warning' : 'default'}
                              sx={{ height: 20, fontSize: '0.7rem' }}
                            />
                            {reminder.recurrence !== 'once' && (
                              <Chip
                                icon={<RepeatIcon fontSize="small" />}
                                label={t(`reminders.recurrence.${reminder.recurrence}`)}
                                size="small"
                                variant="outlined"
                                sx={{ height: 20, fontSize: '0.7rem' }}
                              />
                            )}
                            {reminder.by_mail && (
                              <Chip
                                icon={<EmailIcon fontSize="small" />}
                                label={t('reminders.email')}
                                size="small"
                                variant="outlined"
                                sx={{ height: 20, fontSize: '0.7rem' }}
                              />
                            )}
                          </Box>
                        </Box>
                        <Box sx={{ display: 'flex', gap: 0.5 }}>
                          <Tooltip title={t('reminders.skip')}>
                            <IconButton
                              size="small"
                              color="default"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleSkipReminder(reminder.ID);
                              }}
                              aria-label={t('reminders.skip')}
                              sx={{
                                transition: 'transform 0.15s ease-in-out',
                                '&:hover': {
                                  transform: 'scale(1.15)',
                                },
                              }}
                            >
                              <SkipNextIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                          <Tooltip title={t('reminders.complete')}>
                            <IconButton
                              size="small"
                              color="success"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleCompleteReminder(reminder.ID);
                              }}
                              aria-label={t('reminders.complete')}
                              sx={{
                                transition:
                                  'transform 0.15s ease-in-out, box-shadow 0.15s ease-in-out',
                                '&:hover': {
                                  transform: 'scale(1.15)',
                                  boxShadow: '0 0 8px rgba(76, 175, 80, 0.5)',
                                },
                              }}
                            >
                              <CheckCircleIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        </Box>
                      </Box>
                    </CardContent>
                  </Card>
                );
              })}
            </Stack>
          )}
        </Box>

        {/* Column 3: Random Contacts (Stay in Touch) */}
        <Box>
          <Box sx={{ mb: 1.5, display: 'flex', alignItems: 'center', gap: 1 }}>
            <ShuffleIcon color="primary" fontSize="small" />
            <Typography
              variant="subtitle1"
              component="h2"
              sx={{
                fontWeight: 500,
              }}
            >
              {t('dashboard.randomContacts')}
            </Typography>
            <IconButton
              size="small"
              onClick={(e) => setStayInTouchInfoAnchor(e.currentTarget)}
              aria-label={t('common.info')}
            >
              <InfoOutlinedIcon fontSize="small" />
            </IconButton>
          </Box>
          <Popover
            open={Boolean(stayInTouchInfoAnchor)}
            anchorEl={stayInTouchInfoAnchor}
            onClose={() => setStayInTouchInfoAnchor(null)}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
          >
            <Box sx={{ p: 2, maxWidth: 320 }}>
              <Typography variant="body2">{t('dashboard.stayInTouchInfo')}</Typography>
            </Box>
          </Popover>

          {randomContacts.length === 0 ? (
            <Card>
              <CardContent sx={{ py: 2 }}>
                <Typography
                  variant="body2"
                  sx={{
                    color: 'text.secondary',
                  }}
                >
                  {t('dashboard.noContacts')}
                </Typography>
              </CardContent>
            </Card>
          ) : (
            <Stack spacing={1.5}>
              {randomContacts.map((contact) => (
                <Card
                  key={contact.ID}
                  component={Link}
                  to={`/contacts/${contact.ID}`}
                  sx={{
                    textDecoration: 'none',
                    '&:hover': {
                      boxShadow: 2,
                      transform: 'translateY(-1px)',
                      transition: 'all 0.2s',
                    },
                  }}
                >
                  <CardContent sx={{ py: 1.5 }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                      {/* #196: decorative -- see the favorites Avatar above. */}
                      <Avatar
                        aria-hidden
                        src={contact.photo_thumbnail || undefined}
                        sx={{ bgcolor: 'secondary.main', width: 40, height: 40 }}
                      >
                        {contact.firstname.charAt(0)}
                      </Avatar>
                      <Box sx={{ flexGrow: 1 }}>
                        <Typography
                          variant="body2"
                          sx={{
                            fontWeight: 500,
                          }}
                        >
                          {getContactName(contact)}
                        </Typography>
                        {(circleNamesByUid.get(contact.uid || '') || []).length > 0 && (
                          <Box sx={{ mt: 0.5 }}>
                            {(circleNamesByUid.get(contact.uid || '') || [])
                              .slice(0, 2)
                              .map((circle, idx) => (
                                <Chip
                                  // biome-ignore lint/suspicious/noArrayIndexKey: sliced lookup, no stable id
                                  key={idx}
                                  label={circle}
                                  size="small"
                                  variant="outlined"
                                  sx={{ mr: 0.5, height: 20, fontSize: '0.7rem' }}
                                />
                              ))}
                          </Box>
                        )}
                      </Box>
                    </Box>
                  </CardContent>
                </Card>
              ))}
            </Stack>
          )}
        </Box>
      </Box>
    </Box>
  );
}

export default DashboardPage;
