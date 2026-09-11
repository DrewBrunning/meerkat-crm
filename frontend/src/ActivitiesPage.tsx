import { mdiCalendarPlus } from '@mdi/js';
import EditIcon from '@mui/icons-material/Edit';
import EventIcon from '@mui/icons-material/Event';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import {
  Timeline,
  TimelineConnector,
  TimelineContent,
  TimelineDot,
  TimelineItem,
  TimelineOppositeContent,
  TimelineSeparator,
} from '@mui/lab';
import {
  Box,
  Button,
  Chip,
  IconButton,
  Paper,
  Popover,
  SvgIcon,
  TextField,
  Typography,
} from '@mui/material';
import { type MouseEvent, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router';
import { type Activity, createActivity, deleteActivity, updateActivity } from './api/activities';
import { type Contact, getAllContacts } from './api/contacts';
import AddActivityDialog from './components/AddActivityDialog';
import EditTimelineItemDialog from './components/EditTimelineItemDialog';
import { ListSkeleton } from './components/LoadingSkeletons';
import { useAnnouncer } from './context/AnnouncerContext';
import { useDateFormat } from './DateFormatProvider';
import { useActivities } from './hooks/useActivities';
import { useDebouncedValue } from './hooks/useDebounce';
import { useDocumentTitle } from './hooks/useDocumentTitle';

const ActivitiesPage: React.FC = () => {
  const { t, i18n } = useTranslation();
  useDocumentTitle(t('nav.activities'));
  const { announce } = useAnnouncer();
  const { formatDate, getDatePlaceholder } = useDateFormat();

  const [searchInput, setSearchInput] = useState('');
  const debouncedSearch = useDebouncedValue(searchInput, 400);
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const ACTIVITIES_PER_PAGE = 25;

  // T17: cursor pagination — the timeline pages by (updated_at, id) and a
  // "load more" button appends the next_cursor page. There is no page number
  // or exact total anymore.
  const activityParams = useMemo(
    () => ({
      includeContacts: true,
      limit: ACTIVITIES_PER_PAGE,
      search: debouncedSearch.trim() || undefined,
      fromDate: fromDate || undefined,
      toDate: toDate || undefined,
    }),
    [debouncedSearch, fromDate, toDate],
  );

  const { activities, nextCursor, loading, refetch, loadMore } = useActivities(activityParams);

  // #211: result-count announcement, once per settled fetch.
  useEffect(() => {
    if (!loading) announce(t('common.resultsCount', { count: activities.length }));
  }, [loading, activities.length, announce, t]);
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editingActivity, setEditingActivity] = useState<Activity | null>(null);
  const [editValues, setEditValues] = useState<{
    activityTitle?: string;
    activityDescription?: string;
    activityLocation?: string;
    activityDate?: string;
    activityContacts?: Contact[];
  }>({});
  const [allContacts, setAllContacts] = useState<Contact[]>([]);
  const [infoAnchorEl, setInfoAnchorEl] = useState<HTMLElement | null>(null);
  const infoOpen = Boolean(infoAnchorEl);
  const infoPopoverId = infoOpen ? 'activities-info-popover' : undefined;

  const handleSearchChange = (value: string) => {
    setSearchInput(value);
  };

  const handleFromDateChange = (value: string) => {
    setFromDate(value);
  };

  const handleToDateChange = (value: string) => {
    setToDate(value);
  };

  const handleAddActivity = () => {
    setAddDialogOpen(true);
  };

  const handleActivitySave = async (activity: {
    title: string;
    description: string;
    location: string;
    date: string;
    contact_ids: number[];
  }) => {
    try {
      await createActivity({
        ...activity,
        date: new Date(activity.date).toISOString(),
      });
      setAddDialogOpen(false);
      refetch();
    } catch (err) {
      console.error('Failed to create activity:', err);
      throw err;
    }
  };

  const handleEditClick = async (activity: Activity) => {
    setEditingActivity(activity);

    // Fetch all contacts if not already loaded
    if (allContacts.length === 0) {
      try {
        const all = await getAllContacts({ limit: 1000 });
        setAllContacts(all || []);
      } catch (err) {
        console.error('Failed to fetch contacts:', err);
      }
    }

    setEditValues({
      activityTitle: activity.title || '',
      activityDescription: activity.description || '',
      activityLocation: activity.location || '',
      activityDate: activity.date ? new Date(activity.date).toISOString().split('T')[0] : '',
      activityContacts: activity.contacts || [],
    });
  };

  const handleSaveEdit = async () => {
    if (!editingActivity || !editValues.activityTitle?.trim()) return;

    try {
      await updateActivity(editingActivity.ID, {
        title: editValues.activityTitle,
        description: editValues.activityDescription || '',
        location: editValues.activityLocation || '',
        date: editValues.activityDate
          ? new Date(editValues.activityDate).toISOString()
          : new Date().toISOString(),
        contact_ids: editValues.activityContacts?.map((c) => c.ID) || [],
      });
      setEditingActivity(null);
      setEditValues({});
      refetch();
    } catch (err) {
      console.error('Failed to update activity:', err);
    }
  };

  const handleCancelEdit = () => {
    setEditingActivity(null);
    setEditValues({});
  };

  const handleDeleteActivity = async () => {
    if (!editingActivity) return;

    try {
      await deleteActivity(editingActivity.ID);
      setEditingActivity(null);
      setEditValues({});
      refetch();
    } catch (err) {
      console.error('Failed to delete activity:', err);
    }
  };

  const handleInfoClick = (event: MouseEvent<HTMLElement>) => {
    setInfoAnchorEl(event.currentTarget);
  };

  const handleInfoClose = () => {
    setInfoAnchorEl(null);
  };

  const isInitialLoading = loading && activities.length === 0;
  const hasFilters = searchInput.trim().length > 0 || fromDate || toDate;

  return (
    <Box sx={{ maxWidth: 1200, mx: 'auto', mt: 2, p: 2 }}>
      <Box
        sx={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          mb: 2,
        }}
      >
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1,
          }}
        >
          <Typography variant="h5" component="h1">
            {t('activities.title')}
          </Typography>
          <IconButton
            size="small"
            aria-describedby={infoPopoverId}
            aria-label={t('common.info')}
            onClick={handleInfoClick}
          >
            <InfoOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>
        <Button
          variant="contained"
          color="primary"
          startIcon={
            <SvgIcon>
              <path d={mdiCalendarPlus} />
            </SvgIcon>
          }
          onClick={handleAddActivity}
        >
          {t('activities.addActivity')}
        </Button>
      </Box>

      <Popover
        id={infoPopoverId}
        open={infoOpen}
        anchorEl={infoAnchorEl}
        onClose={handleInfoClose}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      >
        <Box sx={{ p: 2, maxWidth: 320 }}>
          <Typography variant="body2">{t('activities.allContactsInfo')}</Typography>
        </Box>
      </Popover>

      <Paper sx={{ p: 1.5, mb: 2 }}>
        <Box
          sx={{
            display: 'flex',
            gap: 2,
            flexWrap: 'wrap',
          }}
        >
          <TextField
            size="small"
            label={t('activities.search')}
            value={searchInput}
            onChange={(e) => handleSearchChange(e.target.value)}
            variant="outlined"
            sx={{ flex: 1, minWidth: 200 }}
          />
          <TextField
            size="small"
            label={t('activities.fromDate')}
            type="date"
            value={fromDate}
            onChange={(e) => handleFromDateChange(e.target.value)}
            variant="outlined"
            slotProps={{
              inputLabel: { shrink: true },
              input: { placeholder: getDatePlaceholder(), lang: i18n.language },
            }}
            sx={{ width: 160 }}
          />
          <TextField
            size="small"
            label={t('activities.toDate')}
            type="date"
            value={toDate}
            onChange={(e) => handleToDateChange(e.target.value)}
            variant="outlined"
            slotProps={{
              inputLabel: { shrink: true },
              input: { placeholder: getDatePlaceholder(), lang: i18n.language },
            }}
            sx={{ width: 160 }}
          />
        </Box>
      </Paper>

      {isInitialLoading ? (
        <ListSkeleton count={8} />
      ) : activities.length === 0 ? (
        <Paper sx={{ p: 4, textAlign: 'center' }}>
          <Typography
            variant="body1"
            sx={{
              color: 'text.secondary',
            }}
          >
            {hasFilters ? t('activities.noResults') : t('activities.noActivities')}
          </Typography>
        </Paper>
      ) : (
        <Timeline position="right">
          {activities.map((activity, index) => (
            <TimelineItem key={activity.ID}>
              <TimelineOppositeContent
                sx={{
                  color: 'text.secondary',
                  flex: 0.2,
                }}
              >
                <Typography variant="body2">{formatDate(activity.date)}</Typography>
              </TimelineOppositeContent>
              <TimelineSeparator>
                <TimelineDot color="primary">
                  <EventIcon />
                </TimelineDot>
                {index < activities.length - 1 && <TimelineConnector />}
              </TimelineSeparator>
              <TimelineContent sx={{ flex: 0.8 }}>
                <Paper
                  elevation={2}
                  sx={{
                    p: 2,
                    // #196: :focus-within reveals the actions once a button
                    // inside is keyboard-focused, not just on mouse hover.
                    '&:hover .edit-actions, &:focus-within .edit-actions': {
                      opacity: 1,
                    },
                  }}
                >
                  <Box>
                    <Box
                      sx={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'flex-start',
                      }}
                    >
                      <Box sx={{ flex: 1, pr: 2 }}>
                        {activity.title && (
                          // T98: subtitle1 matches the activity title size on
                          // the contact timeline. The color override is gone
                          // with body2 -- it only existed to undo body2's
                          // themed soil color (theme.ts).
                          // #211: a per-item title in a list, not a page heading.
                          <Typography
                            variant="subtitle1"
                            component="p"
                            sx={{
                              fontWeight: 600,
                              wordBreak: 'break-word',
                            }}
                          >
                            {activity.title}
                          </Typography>
                        )}
                        <Typography
                          variant="body2"
                          sx={{ whiteSpace: 'pre-wrap', mt: activity.title ? 0.5 : 0 }}
                        >
                          {activity.description}
                        </Typography>
                        {activity.location && (
                          <Typography
                            variant="caption"
                            sx={{
                              color: 'text.secondary',
                              mt: 1,
                              display: 'block',
                            }}
                          >
                            📍 {activity.location}
                          </Typography>
                        )}
                      </Box>
                      <Box
                        className="edit-actions"
                        sx={{ opacity: 0, transition: 'opacity 0.2s', display: 'flex', gap: 1 }}
                      >
                        <IconButton
                          size="small"
                          onClick={() => handleEditClick(activity)}
                          aria-label={t('contactDetail.editActivity')}
                        >
                          <EditIcon fontSize="small" />
                        </IconButton>
                      </Box>
                    </Box>
                    {activity.contacts && activity.contacts.length > 0 && (
                      <Box
                        sx={{
                          mt: 1,
                          display: 'flex',
                          flexWrap: 'wrap',
                          gap: 0.5,
                        }}
                      >
                        {activity.contacts.map((contact: Contact) => (
                          <Chip
                            key={contact.ID}
                            label={`${contact.firstname} ${contact.lastname}`}
                            size="small"
                            variant="outlined"
                            component={RouterLink}
                            to={`/contacts/${contact.ID}`}
                            clickable
                          />
                        ))}
                      </Box>
                    )}
                  </Box>
                </Paper>
              </TimelineContent>
            </TimelineItem>
          ))}
        </Timeline>
      )}

      {nextCursor && (
        <Box
          sx={{
            display: 'flex',
            justifyContent: 'center',
            mt: 3,
          }}
        >
          <Button variant="outlined" onClick={loadMore} disabled={loading}>
            {t('common.loadMore')}
          </Button>
        </Box>
      )}

      <AddActivityDialog
        open={addDialogOpen}
        onClose={() => setAddDialogOpen(false)}
        onSave={handleActivitySave}
      />

      {editingActivity && (
        <EditTimelineItemDialog
          open={!!editingActivity}
          onClose={handleCancelEdit}
          onSave={handleSaveEdit}
          onDelete={handleDeleteActivity}
          type="activity"
          values={editValues}
          onChange={setEditValues}
          allContacts={allContacts}
        />
      )}
    </Box>
  );
};

export default ActivitiesPage;
