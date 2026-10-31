// model.ts: what the page works out, with no DOM: its addresses, who a project
// waits on, each group's numbers, and an answer in words. A job, a stage and a
// group are the agent's words; nothing here knows what any of them is about.

import type {
  Answer,
  Answers,
  Card,
  DraftView,
  Group,
  Message,
  ProjectRow,
  ProjectView,
} from './wire.d.ts';

// ---- addresses

export type Route =
  | { readonly page: 'home' }
  | { readonly page: 'new' }
  | {
      readonly page: 'project';
      readonly id: number;
      readonly add?: boolean;
      readonly job?: string;
      readonly stage?: string;
      readonly card?: { readonly group: string; readonly id: string };
    };

const enc = encodeURIComponent;

/** The address of a route; the project's own is what muda_open hands out. */
export function hashOf(r: Route): string {
  if (r.page === 'home') return '#/';
  if (r.page === 'new') return '#/new';
  const base = `#/p/${r.id}`;
  if (r.add) return `${base}/add`;
  if (!r.job) return base;
  const job = `${base}/j/${enc(r.job)}`;
  if (r.stage) return `${job}/s/${enc(r.stage)}`;
  if (r.card) return `${job}/c/${enc(r.card.group)}/${enc(r.card.id)}`;
  return job;
}

/** The route an address names; one it can't read is the front page. */
export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  if (parts[0] === 'new') return { page: 'new' };
  const id = Number(parts[1]);
  if (parts[0] !== 'p' || !Number.isInteger(id) || id <= 0)
    return { page: 'home' };
  if (parts[2] === 'add') return { page: 'project', id, add: true };
  if (parts[2] !== 'j' || !parts[3]) return { page: 'project', id };
  const job = decodeURIComponent(parts[3]);
  if (parts[4] === 's' && parts[5])
    return { page: 'project', id, job, stage: decodeURIComponent(parts[5]) };
  if (parts[4] === 'c' && parts[5] && parts[6])
    return {
      page: 'project',
      id,
      job,
      card: {
        group: decodeURIComponent(parts[5]),
        id: decodeURIComponent(parts[6]),
      },
    };
  return { page: 'project', id, job };
}

/** What a tab tells serve it shows: the project's id, '' for the front page. */
export function pageKey(r: Route): string {
  return r.page === 'project' ? String(r.id) : '';
}

// ---- the front page

export type FrontGroup = 'needs' | 'agent' | 'quiet';

export const FRONT_GROUPS: {
  key: FrontGroup;
  title: string;
  empty: string;
  edge?: 'signal' | 'agent';
}[] = [
  {
    key: 'needs',
    title: 'Needs you',
    empty: 'Nothing is waiting on you.',
    edge: 'signal',
  },
  {
    key: 'agent',
    title: 'With the agent',
    empty: 'No project is with a session.',
    edge: 'agent',
  },
  { key: 'quiet', title: 'Quiet', empty: 'Every project has work waiting.' },
];

export function frontGroup(r: ProjectRow): FrontGroup {
  if (r.on_you > 0) return 'needs';
  if (r.on_agent > 0 || r.working) return 'agent';
  return 'quiet';
}

export interface Item {
  value: string;
  label: string;
  count: number;
}

export function statusItems(rows: ProjectRow[]): Item[] {
  return [
    { value: '', label: 'All', count: rows.length },
    ...FRONT_GROUPS.map((g) => ({
      value: g.key,
      label: g.title,
      count: rows.filter((r) => frontGroup(r) === g.key).length,
    })),
  ];
}

/** Every job's label with how many projects have it, by name. */
export function jobFilterItems(rows: ProjectRow[]): Item[] {
  const counts = new Map<string, number>();
  for (const r of rows)
    for (const j of r.jobs ?? [])
      counts.set(j.label, (counts.get(j.label) ?? 0) + 1);
  return [...counts.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([label, count]) => ({ value: label, label, count }));
}

export function filterByJob(
  rows: ProjectRow[],
  chosen: string[],
): ProjectRow[] {
  if (!chosen.length) return rows;
  return rows.filter((r) =>
    (r.jobs ?? []).some((j) => chosen.includes(j.label)),
  );
}

const waits = (n: number) => (n === 1 ? '1 waits on you' : `${n} wait on you`);

/** One line on who a project waits on, '' when nobody. */
export function rowLine(r: ProjectRow): string {
  if (r.on_you > 0) return waits(r.on_you);
  if (r.on_agent > 0) return `${r.on_agent} with the agent`;
  return '';
}

/** The top bar's switcher, as plain values. */
export function elsewhereRows(rows: ProjectRow[], here: number | null) {
  return rows.map((r) => ({
    id: r.id,
    label: r.title,
    detail: rowLine(r),
    tone:
      r.on_you > 0
        ? ('you' as const)
        : r.on_agent > 0 || r.working
          ? ('working' as const)
          : ('idle' as const),
    current: r.id === here,
  }));
}

// ---- the band

export function waitsOf(p: {
  on_you: number;
  on_agent: number;
  owner_label: string;
}): { text: string; who: string }[] {
  const out: { text: string; who: string }[] = [];
  if (p.on_you > 0) out.push({ text: waits(p.on_you), who: 'you' });
  if (p.on_agent > 0) {
    const who = p.owner_label || 'the agent';
    out.push({ text: `${p.on_agent} with ${who}`, who });
  }
  return out;
}

// ---- a project's cards

export const cardsOfJob = (v: ProjectView, job: string): Card[] =>
  (v.cards ?? []).filter((c) => c.job === job);

/** The groups a job shows: a repository's job leaves accounts out, and the other
 * way round; a job with no cards yet shows them all. */
export function jobGroups(v: ProjectView, job: string): Group[] {
  const groups = v.groups ?? [];
  const cards = cardsOfJob(v, job);
  const kinds = new Set(
    groups
      .filter((g) => cards.some((c) => c.group === g.key))
      .map((g) => g.kind),
  );
  return kinds.size ? groups.filter((g) => kinds.has(g.kind)) : groups;
}

export interface GroupStanding {
  group: Group;
  needsYou: number;
  /** A count per stage of the job, in its order. */
  stages: { id: string; label: string; count: number }[];
  baselines: Card[];
}

/** Each group's own numbers, side by side. Nothing adds them up: their windows
 * differ. */
export function groupStandings(v: ProjectView, job: string): GroupStanding[] {
  const stages = (v.jobs ?? []).find((j) => j.id === job)?.stages ?? [];
  const cards = cardsOfJob(v, job);
  return jobGroups(v, job).map((group) => {
    const mine = cards.filter((c) => c.group === group.key);
    return {
      group,
      needsYou: mine.filter((c) => c.needs_you).length,
      stages: stages.map((s) => ({
        id: s.id,
        label: s.label,
        count: mine.filter((c) => c.stage === s.id).length,
      })),
      baselines: mine.filter((c) => c.kind === 'baseline'),
    };
  });
}

/** Every card at a stage, under its group; a group with none is left out. */
export function stageCards(
  v: ProjectView,
  job: string,
  stage: string,
): [Group, Card[]][] {
  const cards = cardsOfJob(v, job).filter((c) => c.stage === stage);
  return (v.groups ?? []).flatMap((g): [Group, Card[]][] => {
    const mine = cards.filter((c) => c.group === g.key);
    return mine.length ? [[g, mine]] : [];
  });
}

export const threadOf = (v: ProjectView, thread: number): Message[] =>
  (v.messages ?? []).filter((m) => m.thread === String(thread));

// ---- answers

function optionLabel(
  options: { id: string; label: string }[] | undefined,
  id: string,
): string {
  return options?.find((o) => o.id === id)?.label ?? id;
}

export function orderOf(a: Answers, answer: Answer | null): string[] {
  const ids = (a.rows ?? []).map((r) => r.id);
  const kept = (answer?.order ?? []).filter((id) => ids.includes(id));
  return [...kept, ...ids.filter((id) => !kept.includes(id))];
}

/** An answer in words: what the thread and the agent read. */
export function answerText(a: Answers, answer: Answer): string {
  switch (a.kind) {
    case 'buttons':
      return optionLabel(a.options, answer.choice ?? '');
    case 'rows':
      return (a.rows ?? [])
        .filter((r) => answer.rows?.[r.id])
        .map((r) => `${r.label}: ${optionLabel(r.options, answer.rows![r.id])}`)
        .join('; ');
    case 'order':
      return `Order: ${orderOf(a, answer)
        .map((id) => optionLabel(a.rows, id))
        .join(', ')}`;
    default:
      return (a.fields ?? [])
        .filter((f) => answer.fields?.[f.id]?.trim())
        .map((f) => `${f.label}: ${answer.fields![f.id].trim()}`)
        .join('; ');
  }
}

/** The answer that keeps what the agent proposed, null when it proposed none. */
export function defaultAnswer(a: Answers): Answer | null {
  if (a.kind === 'order') return { kind: 'order', order: orderOf(a, null) };
  if (a.kind !== 'rows') return null;
  const rows: Record<string, string> = {};
  for (const r of a.rows ?? []) if (r.chosen) rows[r.id] = r.chosen;
  return Object.keys(rows).length ? { kind: 'rows', rows } : null;
}

/** ids with id at index to; an index off the list leaves them as they are. */
export function moved(ids: string[], id: string, to: number): string[] {
  if (to < 0 || to >= ids.length) return ids;
  const rest = ids.filter((x) => x !== id);
  rest.splice(to, 0, id);
  return rest;
}

// ---- drafts

export interface SortedDrafts {
  picks: Map<number, DraftView>;
  notes: Map<number, DraftView>;
  questions: Map<number, DraftView[]>;
}

/** The server's drafts by what each is: a pick carries an answer, a question is
 * one of the words given, and anything else is a note. */
export function sortDrafts(ds: DraftView[], words: string[]): SortedDrafts {
  const out: SortedDrafts = {
    picks: new Map(),
    notes: new Map(),
    questions: new Map(),
  };
  for (const d of ds) {
    if (d.answer) out.picks.set(d.thread, d);
    else if (words.includes(d.text))
      out.questions.set(d.thread, [...(out.questions.get(d.thread) ?? []), d]);
    else out.notes.set(d.thread, d);
  }
  return out;
}

export function sendCount(o: {
  picks: number;
  questions: number;
  notes: string[];
}): number {
  return o.picks + o.questions + o.notes.filter((n) => n.trim()).length;
}

/** What Send took out of a tab's copy, by thread: only the picks and notes that
 * still read as the server held them when it went. A newer one stays a draft. */
export function takenBySend(
  sent: SortedDrafts,
  picks: Map<string, string>,
  notes: Map<string, string>,
): { picks: string[]; notes: string[] } {
  return {
    picks: [...picks]
      .filter(([t, raw]) => raw === JSON.stringify(sent.picks.get(+t)?.answer))
      .map(([t]) => t),
    notes: [...notes]
      .filter(([t, text]) => text.trim() === sent.notes.get(+t)?.text)
      .map(([t]) => t),
  };
}

const SETTLED = ['answered', 'declined', 'failed', 'no_reply'];

/** Which questions a card has out: held as drafts, or sent and not answered. */
export function questionsOf(
  messages: Message[],
  drafts: DraftView[],
  words: string[],
): { sent: string[]; drafted: string[] } {
  return {
    sent: words.filter((w) =>
      messages.some(
        (m) => m.text === w && m.state && !SETTLED.includes(m.state),
      ),
    ),
    drafted: words.filter((w) => drafts.some((d) => d.text === w)),
  };
}

// ---- words

export function tilde(path: string, home: string): string {
  if (home && (path === home || path.startsWith(`${home}/`)))
    return `~${path.slice(home.length)}`;
  return path;
}
