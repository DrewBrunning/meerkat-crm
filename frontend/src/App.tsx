// #211: the app-shell document-structure model. Every route inherits this;
// re-derive it per page and it will drift, which is exactly how the 17
// routes ended up with 17 different answers the first time. Do not
// re-litigate these six rules per page -- apply them.
//
// 1. Document identity. Every route sets document.title via
//    hooks/useDocumentTitle to "<page name> · Mycorrhizal CRM" (localised).
//    <html lang> always tracks the active i18next language (i18n/config.ts).
//
// 2. Landmark map -- exactly one of each per rendered page:
//      banner      the AppBar (implicit <header>, already correct)
//      navigation  the drawer's <List>, wrapped in <nav aria-label>
//      main        the content Box -- and the logged-out branch's Box too
//
// 3. Heading contract:
//      h1            the page's own title -- exactly one per route
//      h2            a section of that page
//      h3            a subsection inside a section, only where one genuinely
//                     exists
//      not a heading  titles of items in a list or timeline (use
//                     component="p"/"span" at the same variant -- do not
//                     renumber them to h3)
//    Visual size stays decoupled from level via `component`
//    (`<Typography variant="h5" component="h1">`) -- theme.ts documents h5 as
//    page-title-only, so this never collides with MuiDialogTitle (h6).
//
// 4. Location signalling. The active nav item carries aria-current="page"
//    and a visual indicator measuring >=3:1 against the adjacent unselected
//    background (a left border, not a background tint -- the old tint
//    measured 1.19:1).
//
// 5. Bypass. A skip link, first in the tab order, targeting #main-content.
//
// 6. Announcement channel. One polite live region, owned by
//    context/AnnouncerContext, reachable via useAnnouncer() by any component
//    that needs to announce a status change.

import { mdiGraphOutline, mdiNotebookOutline } from '@mdi/js';
import AccountCircleIcon from '@mui/icons-material/AccountCircle';
import ClearIcon from '@mui/icons-material/Clear';
import ContactsIcon from '@mui/icons-material/Contacts';
import DashboardIcon from '@mui/icons-material/Dashboard';
import EventNoteIcon from '@mui/icons-material/EventNote';
import GroupIcon from '@mui/icons-material/Group';
import HealthAndSafetyIcon from '@mui/icons-material/HealthAndSafety';
import HistoryIcon from '@mui/icons-material/History';
import HomeWorkIcon from '@mui/icons-material/HomeWork';
import LogoutIcon from '@mui/icons-material/Logout';
import MenuIcon from '@mui/icons-material/Menu';
import MonitorHeartIcon from '@mui/icons-material/MonitorHeart';
import PeopleIcon from '@mui/icons-material/People';
import SearchIcon from '@mui/icons-material/Search';
import SettingsIcon from '@mui/icons-material/Settings';
import ShareIcon from '@mui/icons-material/Share';
import StorageIcon from '@mui/icons-material/Storage';
import {
  AppBar,
  Autocomplete,
  Box,
  Button,
  Divider,
  Drawer,
  IconButton,
  InputAdornment,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  SvgIcon,
  TextField,
  Toolbar,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import { Suspense, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Link,
  Navigate,
  Route,
  RouterProvider,
  Routes,
  createBrowserRouter,
  useLocation,
  useNavigate,
  useSearchParams,
} from 'react-router';
import ActivitiesPage from './ActivitiesPage';
import AuditPage from './AuditPage';
import { type Contact, getContacts } from './api/contacts';
import { fetchAndCacheUserInfo, getToken, isAdmin, logoutAndRedirect } from './auth';
import CirclesTagsPage from './CirclesTagsPage';
import CircleTagTriagePage from './CircleTagTriagePage';
import ContactDetailPage from './ContactDetailPage';
import ContactSharesPage from './ContactSharesPage';
import ContactsPage from './ContactsPage';
import BrandLogo from './components/BrandLogo';
import ErrorBoundary from './components/ErrorBoundary';
import RouteLoadingFallback from './components/RouteLoadingFallback';
import DashboardPage from './DashboardPage';
import DataSettingsPage from './DataSettingsPage';
import HouseholdsPage from './HouseholdsPage';
import LoginPage from './LoginPage';
import NetworkPage from './NetworkPage';
import NotesPage from './NotesPage';
import PrepViewPage from './PrepViewPage';
import RegisterPage from './RegisterPage';
import SettingsPage from './SettingsPage';
import SystemEventsPage from './SystemEventsPage';
import SystemStatusPage from './SystemStatusPage';
import UsersPage from './UsersPage';
import './App.css';
import { AuthContext, useAuth } from './context/AuthContext';

// T98: 180 -> 256. The old 180 was tight enough that four of the five locales
// wrapped their longest nav label even at the pre-T98 16px (de/es/fr/it, at
// 99-128px against a 91px text slot), and raising the label to 1.0625rem made
// English wrap too.
//
// A ListItemButton spends 48px of its width on padding plus a 56px icon slot,
// so the usable text slot is drawerWidth - 104. The widest label across all
// five locales at 1.0625rem is Spanish's "Registro de auditoría" at 136.2px,
// which needs 240.2 -- i.e. 240 misses by a sixth of a pixel, and Italian
// clears it by only 1.5px. 256 gives a 152px slot: every locale fits with real
// headroom rather than sitting on the rounding boundary, where a font-rendering
// difference between platforms would flip it back to wrapping.
const drawerWidth = 256;

// T33 nav classification: which destinations are "primary" (kept as icons in
// the phone AppBar) and which are "account-level" (collapsed into the account
// menu). Everything else is secondary and lives in the hamburger drawer.
const PRIMARY_NAV_PATHS = ['/contacts', '/notes'];
const ACCOUNT_NAV_PATHS = ['/settings', '/settings/data', '/users'];

// Scroll to top on route change
function ScrollToTop() {
  const { pathname } = useLocation();

  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);

  return null;
}

// T86: /search is folded into /contacts, so old bookmarks and deep links to
// /search?q=X keep working by redirecting to the canonical ?search= form. The
// `q` param is only ever read here, never written.
function SearchRedirect() {
  const [searchParams] = useSearchParams();
  const q = (searchParams.get('q') || '').trim();
  return <Navigate to={q ? `/contacts?search=${encodeURIComponent(q)}` : '/contacts'} replace />;
}

// Inner component that can use useLocation (must be inside Router)
function AppContent({
  token,
  setToken,
}: {
  token: string | null;
  setToken: (token: string | null) => void;
}) {
  const { t } = useTranslation();
  const theme = useTheme();
  const location = useLocation();
  const navigate = useNavigate();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'));
  // Below the `sm` breakpoint the AppBar itself is the crowded surface: even
  // title + live search + logout overflow ~360-414px. Swap to icon-only
  // primary destinations + an account menu (T33) once space is tight.
  const compactNav = useMediaQuery(theme.breakpoints.down('sm'));
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState(false);
  const [accountMenuAnchor, setAccountMenuAnchor] = useState<HTMLElement | null>(null);
  const accountMenuOpen = Boolean(accountMenuAnchor);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<Contact[]>([]);
  const [searchLoading, setSearchLoading] = useState(false);

  const handleDrawerToggle = () => {
    setMobileDrawerOpen(!mobileDrawerOpen);
  };

  // Debounced search for contacts
  useEffect(() => {
    if (!token || searchQuery.length < 2) {
      setSearchResults([]);
      return;
    }

    const timer = setTimeout(async () => {
      setSearchLoading(true);
      try {
        const result = await getContacts({ search: searchQuery, limit: 10 });
        setSearchResults(result.contacts || []);
      } catch (err) {
        console.error('Search error:', err);
        setSearchResults([]);
      } finally {
        setSearchLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery, token]);

  const handleLogout = async () => {
    await logoutAndRedirect();
  };

  const handleSearchSubmit = () => {
    if (searchQuery.trim()) {
      navigate(`/contacts?search=${encodeURIComponent(searchQuery.trim())}`);
      setSearchQuery('');
      setSearchResults([]);
    }
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps -- token changes trigger admin status recalculation
  const userIsAdmin = useMemo(() => isAdmin(), [token]);

  const mainNavItems = useMemo(() => {
    const items = [
      { text: t('nav.dashboard'), icon: <DashboardIcon />, path: '/' },
      { text: t('nav.contacts'), icon: <ContactsIcon />, path: '/contacts' },
      { text: t('nav.activities'), icon: <EventNoteIcon />, path: '/activities' },
      {
        text: t('nav.notes'),
        icon: (
          <SvgIcon>
            <path d={mdiNotebookOutline} />
          </SvgIcon>
        ),
        path: '/notes',
      },
      {
        text: t('nav.network'),
        icon: (
          <SvgIcon>
            <path d={mdiGraphOutline} />
          </SvgIcon>
        ),
        path: '/network',
      },
      { text: t('nav.households'), icon: <HomeWorkIcon />, path: '/households' },
      { text: t('nav.circlesTags'), icon: <GroupIcon />, path: '/circles' },
      { text: t('nav.shares'), icon: <ShareIcon />, path: '/shares' },
      { text: t('nav.audit'), icon: <HistoryIcon />, path: '/audit' },
      { text: t('nav.data'), icon: <StorageIcon />, path: '/settings/data' },
      { text: t('nav.profile'), icon: <SettingsIcon />, path: '/settings' },
    ];
    if (userIsAdmin) {
      items.push({ text: t('nav.users'), icon: <PeopleIcon />, path: '/users' });
      items.push({
        text: t('nav.systemEvents'),
        icon: <MonitorHeartIcon />,
        path: '/system-events',
      });
      items.push({
        text: t('nav.systemStatus'),
        icon: <HealthAndSafetyIcon />,
        path: '/system-status',
      });
    }
    return items;
  }, [t, userIsAdmin]);

  // T33 information-architecture pass: the nav bar had grown to ten
  // destinations. Each is classified so the mobile AppBar knows where it
  // lives:
  //   primary — used constantly, kept as visible icons in the AppBar below sm
  //              (Contacts, Notes)
  //   account — Settings, Data settings, User Management; collapsed into the
  //              account menu instead of the main nav row
  //   everything else is secondary and lives in the hamburger drawer.
  const primaryNavItems = useMemo(
    () => mainNavItems.filter((item) => PRIMARY_NAV_PATHS.includes(item.path)),
    [mainNavItems],
  );
  const accountNavItems = useMemo(
    () => mainNavItems.filter((item) => ACCOUNT_NAV_PATHS.includes(item.path)),
    [mainNavItems],
  );

  const drawerContent = (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Toolbar />
      <nav aria-label={t('nav.mainLabel')}>
        <List>
          {mainNavItems.map((item) => {
            const isSelected =
              item.path === '/'
                ? location.pathname === '/'
                : location.pathname.startsWith(item.path);
            return (
              <ListItem key={item.text} disablePadding>
                <ListItemButton
                  component={Link}
                  to={item.path}
                  onClick={isMobile ? handleDrawerToggle : undefined}
                  aria-current={isSelected ? 'page' : undefined}
                  selected={isSelected}
                  sx={{
                    // #211: a left border, not a background tint -- the tint
                    // measured 1.19:1, this measures 6.73:1 light / 6.88:1
                    // dark (see the model comment block at the top of this
                    // file).
                    borderLeft: '3px solid transparent',
                    '&.Mui-selected': {
                      backgroundColor: 'action.selected',
                      borderLeftColor: 'primary.main',
                    },
                    '&.Mui-selected:hover': {
                      backgroundColor: 'action.selected',
                    },
                  }}
                >
                  <ListItemIcon>{item.icon}</ListItemIcon>
                  {/* T98: Garamond has a smaller x-height than IBM Plex Sans, so
                      ListItemText's 1rem body1 default reads shrunken here rather
                      than merely different. T63 set the family and no size. */}
                  <ListItemText
                    primary={item.text}
                    slotProps={{
                      primary: {
                        sx: { fontFamily: '"EB Garamond", serif', fontSize: '1.0625rem' },
                      },
                    }}
                  />
                </ListItemButton>
              </ListItem>
            );
          })}
        </List>
      </nav>
      <Box sx={{ mt: 'auto', py: 2, px: 2 }}>
        <BrandLogo height="auto" width="100%" />
      </Box>
    </Box>
  );

  if (!token) {
    return (
      <Box component="main" sx={{ p: 2, width: '100%' }}>
        <Routes>
          <Route path="/register" element={<RegisterPage />} />
          <Route path="*" element={<LoginPage setToken={setToken} />} />
        </Routes>
      </Box>
    );
  }

  return (
    <>
      <Box
        component="a"
        href="#main-content"
        sx={{
          position: 'absolute',
          left: -9999,
          top: 0,
          zIndex: (theme) => theme.zIndex.drawer + 2,
          p: 2,
          bgcolor: 'background.paper',
          color: 'text.primary',
          '&:focus': { left: 0 },
        }}
      >
        {t('nav.skipToContent')}
      </Box>
      <AppBar
        position="fixed"
        sx={{
          zIndex: (theme) => theme.zIndex.drawer + 1,
        }}
      >
        <Toolbar>
          {isMobile && (
            <IconButton
              edge="start"
              color="inherit"
              aria-label="menu"
              onClick={handleDrawerToggle}
              sx={{ mr: 0.5 }}
            >
              <MenuIcon />
            </IconButton>
          )}
          <Typography
            variant="h6"
            component={Link}
            to="/"
            onClick={() => {
              setSearchQuery('');
              setSearchResults([]);
            }}
            sx={{
              flexGrow: 1,
              display: { xs: 'none', sm: 'block' },
              textDecoration: 'none',
              color: 'inherit',
              fontFamily: '"EB Garamond", serif',
              fontWeight: 600,
              '&:hover': { opacity: 0.8 },
            }}
          >
            {t('app.title')}
          </Typography>

          {compactNav ? (
            // Phone-width AppBar (T33): primary destinations stay directly
            // visible as icon-only buttons (each carries an aria-label — no
            // accessible name is dropped when the label is), secondary items
            // live in the hamburger drawer, and account-level items collapse
            // into the account menu.
            <>
              {primaryNavItems.map((item) => (
                <IconButton
                  key={item.path}
                  component={Link}
                  to={item.path}
                  color="inherit"
                  aria-label={item.text}
                  sx={{ mr: 0.5 }}
                >
                  {item.icon}
                </IconButton>
              ))}
              <Box sx={{ flexGrow: 1 }} />
              <IconButton
                color="inherit"
                aria-label={t('app.accountMenu')}
                onClick={(e) => setAccountMenuAnchor(e.currentTarget)}
              >
                <AccountCircleIcon />
              </IconButton>
              <Menu
                anchorEl={accountMenuAnchor}
                open={accountMenuOpen}
                onClose={() => setAccountMenuAnchor(null)}
              >
                {accountNavItems.map((item) => (
                  <MenuItem
                    key={item.path}
                    component={Link}
                    to={item.path}
                    onClick={() => setAccountMenuAnchor(null)}
                  >
                    <ListItemIcon>{item.icon}</ListItemIcon>
                    <ListItemText>{item.text}</ListItemText>
                  </MenuItem>
                ))}
                <Divider sx={{ my: 0.5 }} />
                <MenuItem onClick={handleLogout}>
                  <ListItemIcon>
                    <LogoutIcon />
                  </ListItemIcon>
                  <ListItemText>{t('app.logout')}</ListItemText>
                </MenuItem>
              </Menu>
            </>
          ) : (
            <>
              <Autocomplete
                freeSolo
                size="small"
                options={searchResults}
                getOptionLabel={(option) =>
                  typeof option === 'string' ? option : `${option.firstname} ${option.lastname}`
                }
                loading={searchLoading}
                onInputChange={(_, value) => setSearchQuery(value)}
                onChange={(_, value) => {
                  if (value && typeof value !== 'string') {
                    navigate(`/contacts/${value.ID}`);
                    setSearchQuery('');
                    setSearchResults([]);
                  }
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    handleSearchSubmit();
                  }
                }}
                inputValue={searchQuery}
                renderInput={(params) => (
                  <TextField
                    {...params}
                    placeholder={t('contacts.search')}
                    variant="outlined"
                    sx={{
                      width: { xs: 150, sm: 200, md: 250 },
                      mr: 2,
                      '& .MuiOutlinedInput-root': {
                        backgroundColor: 'rgba(255, 255, 255, 0.15)',
                        '&:hover': {
                          backgroundColor: 'rgba(255, 255, 255, 0.25)',
                        },
                        '& fieldset': {
                          borderColor: 'rgba(255, 255, 255, 0.3)',
                        },
                        '&:hover fieldset': {
                          borderColor: 'rgba(255, 255, 255, 0.5)',
                        },
                        // #186: checked per the issue's own instruction --
                        // rgba(255,255,255,0.7) on the mycelium AppBar (#3E543E)
                        // computes to 5.04:1, clearing the 3:1 floor. Not short,
                        // no change needed here.
                        '&.Mui-focused fieldset': {
                          borderColor: 'rgba(255, 255, 255, 0.7)',
                        },
                      },
                      '& .MuiInputBase-input': {
                        color: 'white',
                      },
                      '& .MuiInputBase-input::placeholder': {
                        color: 'rgba(255, 255, 255, 0.7)',
                        opacity: 1,
                      },
                    }}
                    slotProps={{
                      ...params.slotProps,

                      input: {
                        ...params.slotProps.input,
                        startAdornment: (
                          <InputAdornment position="start">
                            <IconButton
                              size="small"
                              onClick={handleSearchSubmit}
                              aria-label={t('app.submitSearch')}
                              sx={{ color: 'rgba(255, 255, 255, 0.7)', p: 0.5 }}
                            >
                              <SearchIcon />
                            </IconButton>
                          </InputAdornment>
                        ),
                        endAdornment: searchQuery ? (
                          <InputAdornment position="end">
                            <IconButton
                              size="small"
                              onClick={() => {
                                setSearchQuery('');
                                setSearchResults([]);
                              }}
                              aria-label={t('contacts.searchClear')}
                              sx={{ color: 'rgba(255, 255, 255, 0.7)', p: 0.5 }}
                            >
                              <ClearIcon fontSize="small" />
                            </IconButton>
                          </InputAdornment>
                        ) : null,
                      },
                    }}
                  />
                )}
                renderOption={(props, option) => (
                  <li {...props} key={option.ID}>
                    <Box>
                      <Typography variant="body1">
                        {option.firstname} {option.lastname}
                      </Typography>
                      {option.email && (
                        <Typography
                          variant="caption"
                          sx={{
                            color: 'text.secondary',
                          }}
                        >
                          {option.email}
                        </Typography>
                      )}
                    </Box>
                  </li>
                )}
              />
              <Button color="inherit" startIcon={<LogoutIcon />} onClick={handleLogout}>
                {t('app.logout')}
              </Button>
            </>
          )}
        </Toolbar>
      </AppBar>

      {/* Mobile drawer */}
      <Drawer
        variant="temporary"
        open={mobileDrawerOpen}
        onClose={handleDrawerToggle}
        sx={{
          display: { xs: 'block', md: 'none' },
          '& .MuiDrawer-paper': { boxSizing: 'border-box', width: drawerWidth },
        }}
      >
        {drawerContent}
      </Drawer>

      {/* Desktop drawer */}
      <Drawer
        variant="permanent"
        sx={{
          display: { xs: 'none', md: 'block' },
          width: drawerWidth,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: drawerWidth,
            boxSizing: 'border-box',
          },
        }}
      >
        {drawerContent}
      </Drawer>

      <Box
        component="main"
        id="main-content"
        tabIndex={-1}
        sx={{
          flexGrow: 1,
          p: 2,
          // `min-width: auto` (the flex default) stops this item shrinking below
          // its content's intrinsic width, so a wide child (the contact
          // timeline, the network controls, the settings tables) would force
          // the page to scroll horizontally at phone widths instead of the
          // child scrolling/containing itself. minWidth: 0 lets the page
          // column take the viewport and the children handle their own
          // overflow (T32/T33).
          minWidth: 0,
          width: { md: `calc(100% - ${drawerWidth}px)` },
          mt: 7,
        }}
      >
        {/* Issue #557: scoped to the routed page, not the whole app shell,
            so a crash here leaves the AppBar/drawer/search (and anything
            <SessionExpiredGate> is showing) intact -- "Try Again" retries
            just this subtree instead of forcing a full reload. Keyed by
            pathname so navigating to a different route recovers on its own;
            an ErrorBoundary's caught-error state does not otherwise clear
            just because its children changed. */}
        <ErrorBoundary key={location.pathname} name="Page">
          <Routes>
            <Route
              path="/contacts"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <ContactsPage />
                </Suspense>
              }
            />
            <Route
              path="/contacts/:id"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <ContactDetailPage />
                </Suspense>
              }
            />
            <Route
              path="/contacts/:id/prep"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <PrepViewPage />
                </Suspense>
              }
            />
            <Route
              path="/notes"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <NotesPage />
                </Suspense>
              }
            />
            <Route
              path="/activities"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <ActivitiesPage />
                </Suspense>
              }
            />
            <Route
              path="/settings"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <SettingsPage />
                </Suspense>
              }
            />
            <Route
              path="/settings/data"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <DataSettingsPage />
                </Suspense>
              }
            />
            <Route
              path="/network"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <NetworkPage />
                </Suspense>
              }
            />
            <Route
              path="/households"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <HouseholdsPage />
                </Suspense>
              }
            />
            <Route
              path="/circles"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <CirclesTagsPage />
                </Suspense>
              }
            />
            <Route path="/tags" element={<Navigate to="/circles?tab=tags" replace />} />
            <Route
              path="/shares"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <ContactSharesPage />
                </Suspense>
              }
            />
            <Route path="/search" element={<SearchRedirect />} />
            <Route
              path="/audit"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <AuditPage />
                </Suspense>
              }
            />
            <Route
              path="/users"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <UsersPage />
                </Suspense>
              }
            />
            <Route
              path="/system-events"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <SystemEventsPage />
                </Suspense>
              }
            />
            <Route
              path="/system-status"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <SystemStatusPage />
                </Suspense>
              }
            />
            <Route path="/api-tokens" element={<Navigate to="/settings" replace />} />
            <Route
              path="/circle-tag-triage"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <CircleTagTriagePage />
                </Suspense>
              }
            />
            <Route
              path="/"
              element={
                <Suspense fallback={<RouteLoadingFallback />}>
                  <DashboardPage />
                </Suspense>
              }
            />
            <Route path="/login" element={<Navigate to="/" replace />} />
            <Route path="/register" element={<Navigate to="/" replace />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </ErrorBoundary>
      </Box>
    </>
  );
}

// Issue #805: react-router's useBlocker (the discard guards' in-app route
// navigation protection) only works under a *data* router -- the plain
// <BrowserRouter>+<Routes> this file used never installed the DataRouterContext
// useBlocker reads. The route tree is therefore moved onto a data router,
// created once at module scope (RouterProvider refuses to own a router that is
// recreated on render). AppContent still renders the declarative <Routes>
// tree inside it, so the ~800-line logged-in/logged-out shell and its two
// per-auth route branches move over without a behavioural rewrite -- the
// top-level match is deliberately a single catch-all. What changes is the
// context RouterProvider now installs: useDiscardGuard's navigation blocker
// is live, so a dirty dialog intercepts a drawer link, a programmatic
// navigate, or a browser Back/Forward instead of silently unmounting.
function AppRoot() {
  const { token, setToken } = useAuth();
  return (
    <>
      <ScrollToTop />
      <Box sx={{ display: 'flex' }}>
        <AppContent token={token} setToken={setToken} />
      </Box>
    </>
  );
}

const router = createBrowserRouter([{ path: '*', element: <AppRoot /> }]);

function App() {
  const [token, setToken] = useState(getToken());

  useEffect(() => {
    // Restore session after OIDC redirect: the server sets the auth cookie but
    // localStorage is empty, so we fetch user info once to populate it.
    if (!getToken()) {
      fetchAndCacheUserInfo().then((info) => {
        if (info) setToken(getToken());
      });
    }
  }, []);

  useEffect(() => {
    // Listen for storage changes (e.g., logout in another tab)
    const onStorage = (e: StorageEvent) => {
      if (e.key === 'user_info') {
        setToken(getToken());
      }
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  return (
    <AuthContext.Provider value={{ token, setToken }}>
      <RouterProvider router={router} />
    </AuthContext.Provider>
  );
}

export default App;
