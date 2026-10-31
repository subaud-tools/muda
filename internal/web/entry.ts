// entry.ts: the esbuild entry point, and the router. The bar, its theme and its
// live pill stay through every move; the address says which page is under them,
// and a tab tells serve which project it shows.

import {
  ApiError,
  bar,
  createApi,
  initTheme,
  keepElsewhere,
  live,
  type LiveEvent,
} from '/_kit/kit.js';
import { home } from './home.ts';
import {
  elsewhereRows,
  hashOf,
  pageKey,
  parseHash,
  type Route,
} from './model.ts';
import { newProject } from './new.ts';
import { project } from './project.ts';
import type { ProjectList } from './wire.d.ts';
import type { Page, Shell } from './shell.ts';

const theBar = bar({
  brand: { name: 'muda', href: '#/' },
  staleText: 'restarted · reload this tab',
});
initTheme('muda', theBar.themeControl);
theBar.setLive('polling');

const root = document.getElementById('app')!;

const api = createApi({
  onStale: () => theBar.setLive('stale'),
  // A new serve answered: what this tab runs is out of date, and its drafts are
  // on the server.
  onNewBuild: () => location.reload(),
});

let route: Route = parseHash(location.hash);

const refreshList = keepElsewhere(theBar, async () => {
  const list = await api.get<ProjectList>('/projects');
  const here = route.page === 'project' ? route.id : null;
  return elsewhereRows(list.projects ?? [], here).map((r) => ({
    label: r.label,
    detail: r.detail,
    tone: r.tone,
    current: r.current,
    onClick: () => go({ page: 'project', id: r.id }),
  }));
});

function go(r: Route): void {
  location.hash = hashOf(r);
}

const shell: Shell = { root, bar: theBar, api, go };

let page: Page | null = null;
let pageId = '';

/** Which page an address belongs to: a move inside one keeps it. */
function idOf(r: Route): string {
  if (r.page !== 'project') return r.page;
  return r.add ? `add:${r.id}` : `p:${r.id}`;
}

function start(r: Route): void {
  page?.stop();
  theBar.setPrimary(null);
  theBar.setStatus('');
  root.replaceChildren(theBar.el);
  pageId = idOf(r);
  if (r.page === 'home') page = home(shell);
  else if (r.page === 'new') page = newProject(shell, null);
  else if (r.add) page = newProject(shell, r.id);
  else page = project(shell, r);
}

// The one id this page load gives serve: moving to another project moves the
// tab rather than opening a second.
const tabId = crypto.randomUUID();
let feed: ReturnType<typeof live> | null = null;
let feedPage: string | null = null;

function listen(r: Route): void {
  const key = pageKey(r);
  if (key === feedPage) return;
  const cursor = feed?.cursor();
  feed?.stop();
  feedPage = key;
  feed = live({
    events: '/api/events',
    poll: '/api/poll',
    page: key,
    cursor,
    randomUUID: () => tabId,
    onEvent(e: LiveEvent) {
      if (e.type !== 'project') return;
      void refreshList();
      page?.changed((e.data as { id: number }).id);
    },
    onStatus: (s) => theBar.setLive(s),
    onNewBuild: () =>
      theBar.setStatus(
        'A newer muda is installed; it takes over at the next channel call.',
        { tone: 'signal' },
      ),
    onShow: (p) =>
      go(p ? { page: 'project', id: Number(p) } : { page: 'home' }),
  });
}

addEventListener('hashchange', () => {
  route = parseHash(location.hash);
  listen(route);
  void refreshList();
  if (idOf(route) === pageId) page?.go?.(route);
  else start(route);
});

addEventListener('unhandledrejection', (e) => {
  if (e.reason instanceof ApiError) {
    e.preventDefault();
    if (!e.reason.isStale)
      theBar.setStatus(e.reason.message, { tone: 'danger' });
  }
});

start(route);
listen(route);
void refreshList();
