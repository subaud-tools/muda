import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  answerText,
  defaultAnswer,
  elsewhereRows,
  filterByJob,
  frontGroup,
  groupStandings,
  hashOf,
  jobGroups,
  jobFilterItems,
  moved,
  orderOf,
  pageKey,
  parseHash,
  questionsOf,
  sendCount,
  sortDrafts,
  stageCards,
  statusItems,
  threadOf,
  tilde,
  waitsOf,
} from './model.ts';
import type {
  Answers,
  Card,
  DraftView,
  Message,
  ProjectRow,
  ProjectView,
} from './wire.d.ts';

function row(over: Partial<ProjectRow> = {}): ProjectRow {
  return {
    id: 1,
    title: 'Family tools',
    owner: '',
    owner_label: '',
    owner_present: false,
    working: '',
    on_you: 0,
    on_agent: 0,
    groups: 2,
    jobs: [],
    ...over,
  };
}

function card(over: Partial<Card> = {}): Card {
  return {
    job: 'deploy',
    group: '/w/a',
    stage: 'find',
    id: 'F1',
    title: 'Full clones',
    kind: 'finding',
    class: 'W3',
    basis: 'measured',
    evidence: [],
    decided: '',
    recommendation: '',
    answers: null,
    revision: '',
    posted_at: '',
    thread: 7,
    needs_you: false,
    ...over,
  };
}

function view(over: Partial<ProjectView> = {}): ProjectView {
  return {
    id: 1,
    title: 'Family tools',
    owner: 's1',
    owner_label: 'pi',
    owner_present: true,
    working: '',
    on_you: 0,
    on_agent: 0,
    cursor: '',
    thread: 1,
    jobs: [
      {
        id: 'deploy',
        label: 'Deployments',
        stages: [
          { id: 'base', label: 'Baseline', state: 'done', count: '' },
          { id: 'find', label: 'Findings', state: 'now', count: '' },
        ],
      },
    ],
    groups: [
      { kind: 'repo', key: '/w/a', label: 'a', added_by: 'you' },
      { kind: 'repo', key: '/w/b', label: 'b', added_by: 'you' },
    ],
    cards: [],
    messages: [],
    drafts: [],
    ...over,
  };
}

const draft = (over: Partial<DraftView>): DraftView => ({
  id: 1,
  thread: 7,
  text: 'x',
  answer: null,
  ...over,
});

test('an address and a route read each other, a group key holding slashes', () => {
  const routes = [
    { page: 'home' },
    { page: 'new' },
    { page: 'project', id: 3 },
    { page: 'project', id: 3, add: true },
    { page: 'project', id: 3, job: 'deploy' },
    { page: 'project', id: 3, job: 'deploy', stage: 'find' },
    {
      page: 'project',
      id: 3,
      job: 'deploy',
      card: { group: '/w/a b', id: 'F-1' },
    },
  ] as const;
  for (const r of routes) assert.deepEqual(parseHash(hashOf(r)), r);
});

test('the address muda_open hands out opens the project', () => {
  assert.deepEqual(parseHash('#/p/12'), { page: 'project', id: 12 });
  assert.deepEqual(parseHash(''), { page: 'home' });
  assert.deepEqual(parseHash('#/nonsense'), { page: 'home' });
});

test("a tab's page key is the project's id as text, the front page's empty", () => {
  assert.equal(pageKey({ page: 'project', id: 12 }), '12');
  assert.equal(pageKey({ page: 'home' }), '');
  assert.equal(pageKey({ page: 'new' }), '');
});

test('a project sits in the group of who it waits on, you first', () => {
  assert.equal(frontGroup(row({ on_you: 1, on_agent: 3 })), 'needs');
  assert.equal(frontGroup(row({ on_agent: 3 })), 'agent');
  assert.equal(frontGroup(row({ working: 'reading a run' })), 'agent');
  assert.equal(frontGroup(row()), 'quiet');
});

test('the status pills count each group, All first with every project', () => {
  const items = statusItems([
    row({ on_you: 1 }),
    row({ id: 2 }),
    row({ id: 3 }),
  ]);
  assert.deepEqual(
    items.map((i) => [i.value, i.count]),
    [
      ['', 3],
      ['needs', 1],
      ['agent', 0],
      ['quiet', 2],
    ],
  );
});

test('the job filter counts the projects with each job, and any pick matches', () => {
  const dep = { id: 'd', label: 'Deployments', stage: '', count: '' };
  const cost = { id: 'c', label: 'Cost', stage: '', count: '' };
  const rows = [
    row({ jobs: [dep, cost] }),
    row({ id: 2, jobs: [dep] }),
    row({ id: 3 }),
  ];
  assert.deepEqual(jobFilterItems(rows), [
    { value: 'Cost', label: 'Cost', count: 1 },
    { value: 'Deployments', label: 'Deployments', count: 2 },
  ]);
  assert.deepEqual(
    filterByJob(rows, ['Cost']).map((r) => r.id),
    [1],
  );
  assert.equal(filterByJob(rows, []).length, 3);
});

test('the switcher lists every project, marks this one and says who it waits on', () => {
  const rows = [
    row({ id: 1, on_you: 2 }),
    row({ id: 2, title: 'Client', on_agent: 1 }),
    row({ id: 3, title: 'Quiet' }),
  ];
  assert.deepEqual(elsewhereRows(rows, 2), [
    {
      id: 1,
      label: 'Family tools',
      detail: '2 wait on you',
      tone: 'you',
      current: false,
    },
    {
      id: 2,
      label: 'Client',
      detail: '1 with the agent',
      tone: 'working',
      current: true,
    },
    { id: 3, label: 'Quiet', detail: '', tone: 'idle', current: false },
  ]);
});

test('the band waits on you first, and on the owner by name', () => {
  assert.deepEqual(waitsOf({ on_you: 1, on_agent: 2, owner_label: 'pi' }), [
    { text: '1 waits on you', who: 'you' },
    { text: '2 with pi', who: 'pi' },
  ]);
  assert.deepEqual(waitsOf({ on_you: 0, on_agent: 0, owner_label: '' }), []);
});

test("each group's numbers stand apart per stage, with no total", () => {
  const v = view({
    cards: [
      card({ id: 'F1', needs_you: true }),
      card({ id: 'F2' }),
      card({ id: 'B', stage: 'base', group: '/w/b', kind: 'baseline' }),
    ],
  });
  const out = groupStandings(v, 'deploy');
  assert.deepEqual(
    out.map((g) => [g.group.key, g.needsYou, g.stages.map((s) => s.count)]),
    [
      ['/w/a', 1, [0, 2]],
      ['/w/b', 0, [1, 0]],
    ],
  );
  assert.equal(out[1].baselines[0].id, 'B');
  assert.equal('total' in out[0], false);
});

test('a job shows the kinds of group it has cards in, and every group before it has any', () => {
  const groups = [
    { kind: 'repo', key: '/w/a', label: 'a', added_by: '' },
    { kind: 'repo', key: '/w/b', label: 'b', added_by: '' },
    { kind: 'account', key: 'dev', label: 'Dev', added_by: '' },
  ];
  const v = view({
    groups,
    cards: [card({ group: '/w/a' }), card({ job: 'cost', group: 'dev' })],
  });
  assert.deepEqual(
    jobGroups(v, 'deploy').map((g) => g.key),
    ['/w/a', '/w/b'],
  );
  assert.deepEqual(
    jobGroups(v, 'cost').map((g) => g.key),
    ['dev'],
  );
  assert.equal(jobGroups(v, 'none').length, 3);
});

test("a stage's page holds every card at it, in its job, under each group", () => {
  const v = view({
    cards: [
      card({ id: 'F1' }),
      card({ id: 'F2', group: '/w/b' }),
      card({ id: 'B', stage: 'base' }),
      card({ id: 'X', job: 'cost' }),
    ],
  });
  assert.deepEqual(
    stageCards(v, 'deploy', 'find').map(([g, cs]) => [
      g.key,
      cs.map((c) => c.id),
    ]),
    [
      ['/w/a', ['F1']],
      ['/w/b', ['F2']],
    ],
  );
});

test('a thread is its messages by thread id, read from text', () => {
  const m = (id: number, thread: string): Message => ({
    id,
    thread,
    who: 'you',
    text: '',
    when: '',
  });
  const v = view({ messages: [m(1, '7'), m(2, '8'), m(3, '7')] });
  assert.deepEqual(
    threadOf(v, 7).map((x) => x.id),
    [1, 3],
  );
});

const rowsAnswers: Answers = {
  kind: 'rows',
  rows: [
    {
      id: 'ci',
      label: 'ci.yml',
      chosen: 'gate',
      options: [
        { id: 'gate', label: 'Gate' },
        { id: 'deploy', label: 'Deploy' },
      ],
    },
    {
      id: 'rel',
      label: 'release.yml',
      options: [{ id: 'deploy', label: 'Deploy' }],
    },
  ],
};
const orderAnswers: Answers = {
  kind: 'order',
  rows: [
    { id: 'F1', label: 'Cold cache' },
    { id: 'F2', label: 'Floating tool' },
    { id: 'F3', label: 'Floating action' },
  ],
};
const fieldAnswers: Answers = {
  kind: 'fields',
  fields: [
    { id: 'reason', label: 'Reason' },
    { id: 'review', label: 'Review by', type: 'date' },
  ],
};
const buttonAnswers: Answers = {
  kind: 'buttons',
  options: [{ id: 'ok', label: 'Approve' }],
};

test('an answer says itself in words, for each of the four kinds', () => {
  assert.equal(
    answerText(buttonAnswers, { kind: 'buttons', choice: 'ok' }),
    'Approve',
  );
  assert.equal(
    answerText(rowsAnswers, {
      kind: 'rows',
      rows: { ci: 'deploy', rel: 'deploy' },
    }),
    'ci.yml: Deploy; release.yml: Deploy',
  );
  assert.equal(
    answerText(orderAnswers, { kind: 'order', order: ['F2', 'F1', 'F3'] }),
    'Order: Floating tool, Cold cache, Floating action',
  );
  assert.equal(
    answerText(fieldAnswers, {
      kind: 'fields',
      fields: { reason: 'Kept on purpose', review: '2027-01-04' },
    }),
    'Reason: Kept on purpose; Review by: 2027-01-04',
  );
});

test("the agent's proposed choices can be kept as they stand", () => {
  assert.deepEqual(defaultAnswer(rowsAnswers), {
    kind: 'rows',
    rows: { ci: 'gate' },
  });
  assert.deepEqual(defaultAnswer(orderAnswers), {
    kind: 'order',
    order: ['F1', 'F2', 'F3'],
  });
  assert.equal(defaultAnswer(buttonAnswers), null);
});

test('an order holds every row once, whatever a draft said', () => {
  assert.deepEqual(orderOf(orderAnswers, null), ['F1', 'F2', 'F3']);
  assert.deepEqual(
    orderOf(orderAnswers, { kind: 'order', order: ['F3', 'gone', 'F1'] }),
    ['F3', 'F1', 'F2'],
  );
});

test('dragging puts a row at its new place', () => {
  assert.deepEqual(moved(['a', 'b', 'c'], 'c', 0), ['c', 'a', 'b']);
  assert.deepEqual(moved(['a', 'b', 'c'], 'a', 2), ['b', 'c', 'a']);
  assert.deepEqual(moved(['a', 'b', 'c'], 'a', -1), ['a', 'b', 'c']);
});

test("the server's drafts part into picks, questions and notes", () => {
  const pick = draft({ id: 1, answer: { kind: 'buttons', choice: 'ok' } });
  const note = draft({ id: 2, text: 'why this order?' });
  const asked = draft({ id: 3, text: 'Explain this more' });
  const out = sortDrafts([pick, note, asked], ['Explain this more']);
  assert.equal(out.picks.get(7)?.id, 1);
  assert.equal(out.notes.get(7)?.id, 2);
  assert.deepEqual(out.questions.get(7), [asked]);
});

test('Send counts the picks and questions held and the notes with words', () => {
  assert.equal(
    sendCount({ picks: 1, questions: 2, notes: ['why?', '   ', ''] }),
    4,
  );
});

test('a question reads asked while it is a draft or waits on an answer', () => {
  const words = ['Explain this more', 'What breaks?'];
  const q = (text: string, state: Message['state']): Message => ({
    id: 1,
    thread: '7',
    who: 'you',
    text,
    when: '',
    state,
  });
  const got = questionsOf(
    [q('Explain this more', 'with_agent'), q('What breaks?', 'answered')],
    [],
    words,
  );
  assert.deepEqual(got, { sent: ['Explain this more'], drafted: [] });
  assert.deepEqual(
    questionsOf([], [draft({ text: 'What breaks?' })], words).drafted,
    ['What breaks?'],
  );
});

test('a path under home shows as ~', () => {
  assert.equal(tilde('/Users/c/GitHub/x', '/Users/c'), '~/GitHub/x');
  assert.equal(tilde('/w/x', '/Users/c'), '/w/x');
  assert.equal(tilde('/Users/cc/x', '/Users/c'), '/Users/cc/x');
});
