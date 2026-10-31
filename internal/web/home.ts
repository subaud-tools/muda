// home.ts: the front page, every project with where it stands and who it waits
// on. The status pills and the job rows narrow the list; a press turns a pick on
// or off, All clears its set, and the browser keeps what was set.

import {
  filterRow,
  frame,
  groupList,
  h,
  stagePill,
  statusPills,
  workingLine,
} from '/_kit/kit.js';
import {
  filterByJob,
  FRONT_GROUPS,
  frontGroup,
  jobFilterItems,
  rowLine,
  statusItems,
} from './model.ts';
import { band, type Page, type Shell } from './shell.ts';
import type { ProjectList, ProjectRow } from './wire.d.ts';

function keptList(key: string): string[] {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key) ?? '[]');
    return Array.isArray(v) ? (v as string[]) : [];
  } catch {
    return [];
  }
}

function toggled(list: string[], v: string): string[] {
  if (!v) return [];
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

function rowOf(r: ProjectRow): HTMLElement {
  const line = rowLine(r);
  const who = r.owner
    ? `${r.owner_label || r.owner}${r.owner_present ? '' : ' · away'}`
    : 'no session yet';
  return h(
    'li',
    { class: 'muda-row', 'data-project': r.id },
    h(
      'a',
      { class: 'muda-row-link', href: `#/p/${r.id}` },
      h('h2', { class: 'muda-row-title' }, r.title),
      h(
        'div',
        { class: 'muda-row-sub' },
        (r.jobs ?? []).map((j) =>
          stagePill({
            key: j.id,
            label: j.stage ? `${j.label}: ${j.stage}` : j.label,
            count: j.count,
          }),
        ),
        h(
          'span',
          { class: 'muda-row-meta' },
          `${r.groups} ${r.groups === 1 ? 'group' : 'groups'} · ${who}`,
        ),
      ),
      workingLine({ text: r.working || null }),
    ),
    line
      ? h(
          'span',
          {
            class: `kit-pill kit-badge ${r.on_you ? 't-you' : 't-wait'}`,
            'data-badge': r.on_you ? 'you' : 'agent',
          },
          line,
        )
      : null,
  );
}

export function home(s: Shell): Page {
  s.bar.setPrimary({ label: 'New project', run: () => s.go({ page: 'new' }) });
  const columns = frame({
    name: 'muda',
    left: { panel: 'filters', label: 'Filters' },
    centre: { label: 'Projects' },
  });
  const pills = h('div', { class: 'muda-pills' });
  columns.centre.classList.add('muda-wide');
  s.root.append(
    band(h('h1', { class: 'muda-title' }, 'Projects'), pills),
    columns.left!,
    columns.centre,
  );

  let rows: ProjectRow[] = [];
  let status = keptList('muda.home.status');
  let jobs = keptList('muda.home.job');
  const folded = new Set(keptList('muda.home.folded'));

  function draw(): void {
    const items = jobFilterItems(rows);
    jobs = jobs.filter((j) => items.some((i) => i.value === j));
    columns.left!.hidden = items.length === 0;
    columns.leftBody!.replaceChildren(
      filterRow({
        label: 'Jobs',
        items,
        chosen: jobs,
        onToggle(v) {
          jobs = toggled(jobs, v);
          localStorage.setItem('muda.home.job', JSON.stringify(jobs));
          draw();
        },
      }),
    );
    const shown = filterByJob(rows, jobs);
    pills.replaceChildren(
      statusPills({
        pills: statusItems(shown),
        chosen: status,
        onToggle(v) {
          status = toggled(status, v);
          localStorage.setItem('muda.home.status', JSON.stringify(status));
          draw();
        },
      }),
    );
    if (!rows.length) {
      columns.centre.replaceChildren(
        h(
          'div',
          { class: 'muda-quiet' },
          h('p', { class: 'muda-quiet-lead' }, 'No projects yet.'),
          h(
            'p',
            null,
            'A project is a set of repositories you pick. Start one with New project, or ask a session to open one with muda.',
          ),
        ),
      );
      return;
    }
    const groups = FRONT_GROUPS.flatMap((g) => {
      const chosen = status.includes(g.key);
      if (status.length && !chosen) return [];
      const inGroup = shown.filter((r) => frontGroup(r) === g.key);
      if (!inGroup.length && !chosen) return [];
      return [
        {
          key: g.key,
          title: g.title,
          empty: g.empty,
          edge: g.edge,
          rows: inGroup.map(rowOf),
        },
      ];
    });
    columns.centre.replaceChildren(
      groupList({
        groups,
        folded: [...folded],
        onFold(key, on) {
          if (on) folded.add(key);
          else folded.delete(key);
          localStorage.setItem('muda.home.folded', JSON.stringify([...folded]));
        },
      }),
    );
  }

  let stopped = false;
  async function load(): Promise<void> {
    const list = await s.api.get<ProjectList>('/projects');
    if (stopped) return;
    rows = list.projects ?? [];
    draw();
  }
  void load();
  return {
    stop: () => {
      stopped = true;
    },
    changed: () => void load(),
  };
}
