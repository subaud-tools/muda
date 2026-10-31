// new.ts: New project, and adding repositories to a project. A folder you name
// is scanned for GitHub repositories, each with whether it has a record, and you
// tick the ones you want; another folder's ticks stay with the ones you have.

import { ApiError, h } from '/_kit/kit.js';
import { tilde } from './model.ts';
import { band, type Page, type Shell } from './shell.ts';
import type {
  Project,
  ProjectBody,
  ProjectList,
  ProjectView,
  ScannedRepo,
  ScanResult,
} from './wire.d.ts';

const FOLDER_KEY = 'muda.scan.folder';

function field(label: string, control: HTMLElement): HTMLElement {
  return h(
    'label',
    { class: 'muda-field' },
    h('span', { class: 'kit-label' }, label),
    control,
  );
}

export function newProject(s: Shell, into: number | null): Page {
  const main = h('main', {
    class: 'kit-read kit-col kit-mid muda-full',
    'aria-label': into ? 'Add repositories' : 'New project',
  });
  const heading = h(
    'h1',
    { class: 'muda-title' },
    into ? 'Add repositories' : 'New project',
  );
  s.root.append(band(heading), main);
  s.bar.setStatus(into ? '' : 'New project');

  let home = '';
  let found: ScannedRepo[] = [];
  let scanned = '';
  const ticked = new Map<string, ScannedRepo>();
  const have = new Set<string>();

  const title = h('input', {
    class: 'kit-note',
    name: 'title',
    type: 'text',
    placeholder: 'Family tools',
    'aria-label': 'The project name',
  }) as HTMLInputElement;
  const folder = h('input', {
    class: 'kit-note',
    name: 'folder',
    type: 'text',
    placeholder: '~/GitHub',
    'aria-label': 'A folder you work in',
  }) as HTMLInputElement;
  folder.value = localStorage.getItem(FOLDER_KEY) ?? '';
  const why = h('p', { class: 'muda-why', role: 'alert' });
  why.hidden = true;
  const list = h('div', { class: 'muda-scan' });
  const save = h(
    'button',
    { class: 'kit-btn fill', type: 'submit' },
    into ? 'Add' : 'Create project',
  ) as HTMLButtonElement;

  function say(text: string): void {
    why.textContent = text;
    why.hidden = !text;
  }

  function tickRow(r: ScannedRepo): HTMLElement {
    const owned = have.has(r.path);
    return h(
      'li',
      null,
      h(
        'label',
        { class: 'muda-tick', 'data-repo': r.path },
        h('input', {
          type: 'checkbox',
          checked: owned || ticked.has(r.path),
          disabled: owned,
          onchange: (e: Event) => {
            if ((e.target as HTMLInputElement).checked) ticked.set(r.path, r);
            else ticked.delete(r.path);
            drawList();
          },
        }),
        h(
          'span',
          { class: 'muda-tick-text' },
          h('b', null, r.name),
          h('span', { class: 'kit-key' }, tilde(r.path, home)),
        ),
        h(
          'span',
          { class: `kit-pill${r.has_record ? ' ok' : ''}` },
          owned
            ? 'in this project'
            : r.has_record
              ? 'has a record'
              : 'no record yet',
        ),
      ),
    );
  }

  function drawList(): void {
    const elsewhere = [...ticked.values()].filter(
      (r) => !found.some((f) => f.path === r.path),
    );
    save.disabled = !ticked.size;
    list.replaceChildren(
      ...[
        elsewhere.length
          ? h(
              'section',
              { 'data-section': 'ticked' },
              h('div', { class: 'kit-label' }, 'Ticked in another folder'),
              h('ul', { class: 'muda-ticks' }, elsewhere.map(tickRow)),
            )
          : null,
        scanned
          ? h(
              'section',
              { 'data-section': 'found' },
              h(
                'div',
                { class: 'kit-label muda-found' },
                `Found in ${tilde(scanned, home)} · ${found.length}`,
              ),
              found.length
                ? h('ul', { class: 'muda-ticks' }, found.map(tickRow))
                : h(
                    'p',
                    { class: 'kit-empty' },
                    'No GitHub repositories within two levels of it.',
                  ),
            )
          : null,
      ].filter((x): x is HTMLElement => !!x),
    );
  }

  async function scan(): Promise<void> {
    const f = folder.value.trim();
    if (!f) {
      say('Name a folder to look in.');
      return;
    }
    say('');
    try {
      const r = await s.api.get<ScanResult>('/scan', { folder: f });
      found = r.repos ?? [];
      scanned = r.folder;
      localStorage.setItem(FOLDER_KEY, f);
    } catch (err) {
      found = [];
      scanned = '';
      say((err as ApiError).message);
    }
    drawList();
  }

  async function submit(e: Event): Promise<void> {
    e.preventDefault();
    const groups = [...ticked.values()].map((r) => ({
      kind: 'repo',
      key: r.path,
      label: r.name,
      added_by: '',
    }));
    say('');
    save.disabled = true;
    try {
      if (into) {
        const body: ProjectBody = { title: '', groups };
        await s.api.post(`/projects/${into}/groups`, body);
        s.go({ page: 'project', id: into });
      } else {
        const name = title.value.trim();
        if (!name) {
          say('Give the project a name.');
          save.disabled = false;
          title.focus();
          return;
        }
        const body: ProjectBody = { title: name, groups };
        const p = await s.api.post<Project>('/projects', body);
        s.go({ page: 'project', id: p.id });
      }
    } catch (err) {
      say((err as ApiError).message);
      save.disabled = false;
    }
  }

  const form = h(
    'form',
    { class: 'kit-card muda-form', onsubmit: (e: Event) => void submit(e) },
    into ? null : field('Name', title),
    h(
      'div',
      { class: 'muda-folder' },
      field(into ? 'Another folder' : 'A folder you work in', folder),
      h(
        'button',
        {
          class: 'kit-btn',
          type: 'button',
          'data-act': 'scan',
          onclick: () => void scan(),
        },
        'Scan',
      ),
    ),
    why,
    list,
    h(
      'div',
      { class: 'kit-btns' },
      save,
      h(
        'button',
        {
          class: 'kit-btn',
          type: 'button',
          onclick: () =>
            s.go(into ? { page: 'project', id: into } : { page: 'home' }),
        },
        'Cancel',
      ),
    ),
  );
  folder.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void scan();
    }
  });
  main.append(form);
  drawList();

  void (async () => {
    const l = await s.api.get<ProjectList>('/projects');
    home = l.home;
    if (into) {
      const v = await s.api.get<ProjectView>(`/projects/${into}`);
      for (const g of v.groups ?? []) have.add(g.key);
      s.bar.setStatus(v.title);
    }
    if (folder.value) void scan();
    else drawList();
  })();

  return { stop() {}, changed() {} };
}
