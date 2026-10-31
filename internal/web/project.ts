// project.ts: a project's page, in the family's shape. A band with the agent's
// line, who it waits on, a tab per job and that job's stages; the left index of
// cards under their groups; one card, a stage's every card, or each group's
// numbers in the centre; the thread on the right.
//
// What you say is a draft until Send: a pick is saved as soon as it is made, a
// note a moment after you stop typing, so a reload keeps both, and Send takes
// them all together. The server holds the drafts; until the saves have settled,
// this tab's own copy wins. What leaving the page finds unsaved waits in the
// browser's storage, and is saved when the project next opens. A save that fails
// keeps this tab's copy, says so and stops Send; the next save or Send tries again.

import {
  buttons,
  card as kitCard,
  createDrafts,
  fold,
  frame,
  h,
  indexRow,
  indexSection,
  QUESTIONS,
  questionButton,
  sendBar,
  stageTrack,
  thread,
  waitBadges,
  workingLine,
  type StageValue,
} from '/_kit/kit.js';
import { answerControls } from './answers.ts';
import {
  answerText,
  cardsOfJob,
  groupStandings,
  hashOf,
  jobGroups,
  questionsOf,
  sendCount,
  sortDrafts,
  stageCards,
  takenBySend,
  unsavedNotes,
  threadOf,
  waitsOf,
  type Route,
  type SortedDrafts,
} from './model.ts';
import { band, phone, type Page, type Shell } from './shell.ts';
import type {
  Answer,
  Card,
  DraftBody,
  DraftView,
  Group,
  ProjectView,
  SendResult,
} from './wire.d.ts';

type ProjectRoute = Extract<Route, { page: 'project' }>;

const WORDS = Object.values(QUESTIONS);
const SAVE_AFTER_MS = 500;

/** What left a project's page unsaved: pick (null: taken back) and note by thread. */
interface Unsaved {
  picks: [number, string | null][];
  notes: [number, string][];
  asks: Ask[];
}

/** A question asked on a card, not yet saved as a draft. */
interface Ask {
  thread: number;
  text: string;
}

// Storage can be off or full; the page works without it, as it did before.
function readUnsaved(key: string): Unsaved | null {
  try {
    return JSON.parse(localStorage.getItem(key) ?? 'null') as Unsaved | null;
  } catch {
    return null;
  }
}

function writeUnsaved(key: string, u: Unsaved | null): void {
  try {
    if (u) localStorage.setItem(key, JSON.stringify(u));
    else localStorage.removeItem(key);
  } catch {
    // As above.
  }
}

/** Replace el's content, keeping the focus on the control that had it. */
function put(el: HTMLElement, ...nodes: Node[]): void {
  const a = document.activeElement as HTMLInputElement | null;
  const name = a?.dataset?.focus ?? a?.dataset?.draft;
  const had = !!a && !!name && el.contains(a);
  let start: number | null = null;
  if (had) {
    try {
      start = a.selectionStart;
    } catch {
      start = null;
    }
  }
  el.replaceChildren(...nodes);
  if (!had) return;
  const next = el.querySelector<HTMLInputElement>(
    `[data-focus="${CSS.escape(name)}"],[data-draft="${CSS.escape(name)}"]`,
  );
  if (!next) return;
  next.focus();
  if (start !== null) {
    try {
      next.setSelectionRange(start, start);
    } catch {
      // A date or a select has no caret.
    }
  }
}

export function project(s: Shell, first: ProjectRoute): Page {
  const id = first.id;
  const base = `/projects/${id}`;
  let route = first;
  let view: ProjectView | null = null;
  let stopped = false;
  // The card a stage page is on: its thread is on the right.
  let focus: Card | null = null;

  const columns = frame({
    name: 'muda',
    left: { panel: 'index', label: 'Cards', folded: phone() },
    centre: { label: 'Cards' },
    right: { panel: 'thread', label: 'Thread' },
  });
  const bandBox = h('div', { class: 'muda-band-box' });
  const centre = h('div', { class: 'muda-centre' });
  const foot = h('div', { class: 'muda-foot' });
  columns.centre.append(centre, foot);
  s.root.append(bandBox, columns.left!, columns.centre, columns.right!);
  s.bar.setPrimary({
    label: 'Add repositories',
    run: () => s.go({ page: 'project', id, add: true }),
  });

  // ---- drafts ----------------------------------------------------------------
  const drafts = createDrafts();
  const noteKey = (thread: number) => `note:${thread}`;
  let server: SortedDrafts = sortDrafts([], WORDS);
  let chain: Promise<unknown> = Promise.resolve();
  let busy = 0;
  let edits = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  // Picks this tab holds that the server doesn't yet, with the card to save each.
  const unsavedPicks = new Map<number, Card>();
  let asks: Ask[] = [];
  // The last save failed, or the last Send or Discard: the bar says so.
  let saveError = '';
  let refused = '';
  const unsavedKey = `muda:unsaved:${id}`;
  let restoring = readUnsaved(unsavedKey);

  const pickOf = (thread: number): Answer | null => {
    const raw = drafts.picks.get(String(thread));
    return raw ? (JSON.parse(raw) as Answer) : null;
  };

  /** The bar's line: what went wrong, or the project's title. */
  function say(): void {
    if (stopped) return;
    const wrong = saveError || refused;
    if (wrong) s.bar.setStatus(wrong, { tone: 'danger' });
    else s.bar.setStatus(view?.title ?? '');
  }

  function saveFailed(err: Error): void {
    saveError = `Not saved: ${err.message}. Your drafts stay here and nothing was sent; Send tries again.`;
    say();
  }

  function enqueue(
    fn: () => Promise<unknown>,
    failed: (err: Error) => void = saveFailed,
  ): Promise<unknown> {
    edits++;
    busy++;
    chain = chain
      .then(fn)
      .catch(failed)
      .finally(() => {
        busy--;
        if (!busy && !timer) void load();
      });
    return chain;
  }

  /** Make the server's pick for a card what this tab holds. */
  async function savePick(c: Card): Promise<void> {
    const raw = drafts.picks.get(String(c.thread));
    const old = server.picks.get(c.thread);
    if (old && raw === JSON.stringify(old.answer)) return;
    if (old) {
      await s.api.del(`${base}/drafts/${old.id}`);
      server.picks.delete(c.thread);
    }
    if (!raw || !c.answers) return;
    const answer = JSON.parse(raw) as Answer;
    const body: DraftBody = {
      thread: c.thread,
      text: answerText(c.answers, answer),
      answer,
    };
    // Held as this tab wrote it, so comparing it with the tab's copy means what
    // it says; the server's copy may spell the same answer differently.
    const saved = await s.api.post<DraftView>(`${base}/drafts`, body);
    server.picks.set(c.thread, { ...saved, answer });
  }

  /** Make the server's notes what the boxes hold. */
  async function saveNotes(): Promise<void> {
    for (const [key, text] of drafts.notes('note:')) {
      const t = Number(key);
      const words = text.trim();
      const old = server.notes.get(t);
      if (old && !words) {
        await s.api.del(`${base}/drafts/${old.id}`);
        server.notes.delete(t);
      } else if (old && old.text !== words) {
        const body: DraftBody = { thread: t, text: words, answer: null };
        await s.api.put(`${base}/drafts/${old.id}`, body);
        server.notes.set(t, { ...old, text: words });
      } else if (!old && words) {
        const body: DraftBody = { thread: t, text: words, answer: null };
        server.notes.set(
          t,
          await s.api.post<DraftView>(`${base}/drafts`, body),
        );
      }
    }
  }

  /** Save every pick and note that differs from the server's. */
  async function saveChanges(): Promise<void> {
    for (const [t, c] of [...unsavedPicks]) {
      const raw = drafts.picks.get(String(t));
      await savePick(c);
      if (drafts.picks.get(String(t)) === raw) unsavedPicks.delete(t);
    }
    while (asks.length) {
      const { thread, text } = asks[0];
      const body: DraftBody = { thread, text, answer: null };
      const d = await s.api.post<DraftView>(`${base}/drafts`, body);
      asks = asks.slice(1);
      server.questions.set(thread, [
        ...(server.questions.get(thread) ?? []),
        d,
      ]);
    }
    await saveNotes();
    saveError = '';
    say();
    keepUnsaved();
  }

  /** Put what isn't saved where the next page load finds it. */
  function keepUnsaved(): void {
    if (restoring) return;
    const picks = [...unsavedPicks.keys()].map((t): [number, string | null] => [
      t,
      drafts.picks.get(String(t)) ?? null,
    ]);
    const notes = unsavedNotes(drafts.notes('note:'), server);
    writeUnsaved(
      unsavedKey,
      picks.length || notes.length || asks.length
        ? { picks, notes, asks }
        : null,
    );
  }

  /** Lay what the last page load left unsaved over the server's drafts. */
  function restore(v: ProjectView, u: Unsaved): void {
    for (const [t, raw] of u.picks) {
      const c = v.cards?.find((x) => x.thread === t);
      if (!c) continue;
      if (raw) drafts.picks.set(String(t), raw);
      else drafts.picks.delete(String(t));
      unsavedPicks.set(t, c);
    }
    for (const [t, text] of u.notes) drafts.restore(noteKey(t), text);
    asks = u.asks;
    void flush();
  }

  // Typing, in a note or a field: saved a moment after the keys stop.
  function saveSoon(): void {
    edits++;
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      void flush();
    }, SAVE_AFTER_MS);
  }
  function flush(): Promise<unknown> {
    if (timer) clearTimeout(timer);
    timer = null;
    return enqueue(saveChanges);
  }

  function setPick(c: Card, a: Answer | null, typing = false): void {
    focus = c;
    const key = String(c.thread);
    if (a) drafts.picks.set(key, JSON.stringify(a));
    else drafts.picks.delete(key);
    unsavedPicks.set(c.thread, c);
    if (typing) {
      saveSoon();
      drawFoot();
      return;
    }
    void enqueue(saveChanges);
    draw();
  }

  function ask(c: Card, words: string): void {
    focus = c;
    asks.push({ thread: c.thread, text: words });
    void enqueue(saveChanges);
    draw();
  }

  function sends(): number {
    return sendCount({
      picks: drafts.picks.size,
      questions: [...server.questions.values()].flat().length + asks.length,
      notes: [...drafts.notes('note:').values()],
    });
  }

  function send(): void {
    refused = '';
    void flush();
    void enqueue(
      async () => {
        // A save behind this failed: what's on the server isn't what you wrote.
        if (saveError) return;
        // The queue holds every save back while this runs, so what the server has
        // now is what goes. Anything else in this tab's copy stays a draft, and
        // its own save, queued or on its timer, writes it after.
        const r = await s.api.post<SendResult>(`${base}/send`);
        const taken = takenBySend(server, drafts.picks, drafts.notes('note:'));
        for (const t of taken.picks) drafts.picks.delete(t);
        for (const t of taken.notes) drafts.take(`note:${t}`);
        server = sortDrafts([], WORDS);
        flashSent(r.sent);
        draw();
      },
      (err: Error) => {
        refused = `Not sent: ${err.message}. Your drafts stay here.`;
        say();
      },
    );
  }

  function flashSent(n: number): void {
    const words = `${n} sent to the agent.`;
    s.bar.setStatus(words, { tone: 'signal' });
    setTimeout(say, 4000);
  }

  function discard(): void {
    unsavedPicks.clear();
    asks = [];
    saveError = '';
    refused = '';
    writeUnsaved(unsavedKey, null);
    if (timer) clearTimeout(timer);
    timer = null;
    drafts.discard('note:');
    server = sortDrafts([], WORDS);
    void enqueue(
      () => s.api.del(`${base}/drafts`),
      (err: Error) => {
        refused = `Not discarded: ${err.message}.`;
        say();
      },
    );
    draw();
  }

  // Whatever is typed in a note box counts at once, and is saved soon.
  columns.centre.addEventListener('input', (e) => {
    if (!(e.target as HTMLElement).matches('.kit-draft')) return;
    saveSoon();
    drawFoot();
  });

  // ---- drawing ---------------------------------------------------------------
  // A press in progress keeps what is under it: a redraw between its down and up
  // would lose the click.
  let pressing = false;
  let pending = false;
  const stopping = new AbortController();
  const { signal } = stopping;
  document.addEventListener(
    'pointerdown',
    () => {
      pressing = true;
    },
    { signal },
  );
  for (const ev of ['pointerup', 'pointercancel'])
    document.addEventListener(
      ev,
      () =>
        setTimeout(() => {
          pressing = false;
          if (pending) draw();
        }, 30),
      { signal },
    );

  function jobId(v: ProjectView): string {
    return route.job ?? v.jobs?.[0]?.id ?? '';
  }

  const groupLabel = (v: ProjectView, key: string) =>
    v.groups?.find((g) => g.key === key)?.label || key;

  function open(next: ProjectRoute): void {
    s.go(next);
    if (phone()) {
      const toggle =
        columns.left!.querySelector<HTMLElement>('.kit-col-toggle');
      if (!columns.left!.classList.contains('kit-collapsed')) toggle?.click();
    }
  }

  function drawBand(v: ProjectView): void {
    const job = jobId(v);
    const stages = v.jobs?.find((j) => j.id === job)?.stages ?? [];
    const values: StageValue[] = stages.map((st) => ({
      key: st.id,
      label: st.label,
      count: st.count,
      state: (st.state || '') as StageValue['state'],
      live: st.state === 'now' && !!v.working,
      opens: st.id,
    }));
    const track = stages.length
      ? stageTrack({
          stages: values,
          label: 'Stages',
          open: (st) => open({ page: 'project', id, job, stage: st }),
          after: waitBadges(waitsOf(v)),
        })
      : h('div', { class: 'kit-stages' }, waitBadges(waitsOf(v)));
    if (route.stage)
      track
        .querySelector(`[data-stage="${CSS.escape(route.stage)}"]`)
        ?.classList.add('muda-stage-open');
    const who = v.owner
      ? `${v.owner_label || v.owner}${v.owner_present ? '' : ' · away'}`
      : 'no session yet';
    const needs = (j: string) =>
      cardsOfJob(v, j).filter((c) => c.needs_you).length;
    put(
      bandBox,
      band(
        h(
          'div',
          { class: 'muda-band-line' },
          h('h1', { class: 'muda-title muda-title-narrow' }, v.title),
          h(
            'nav',
            { class: 'muda-tabs', 'aria-label': 'Jobs' },
            (v.jobs ?? []).map((j) => {
              const n = needs(j.id);
              return h(
                'a',
                {
                  class: `kit-chip${j.id === job ? ' on' : ''}`,
                  href: hashOf({ page: 'project', id, job: j.id }),
                  'data-job': j.id,
                  'aria-current': j.id === job ? 'page' : null,
                },
                j.label,
                n ? h('span', { class: 'kit-n' }, String(n)) : null,
              );
            }),
          ),
          h(
            'div',
            { class: 'kit-agent-slot' },
            workingLine({ text: v.working || null }),
          ),
          h('span', { class: 'muda-owner', 'data-owner': v.owner }, who),
        ),
        track,
      ),
    );
  }

  function drawLeft(v: ProjectView): void {
    const job = jobId(v);
    const on = route.card;
    const rows = (g: Group) =>
      cardsOfJob(v, job)
        .filter((c) => c.group === g.key)
        .map((c) =>
          indexRow({
            id: `${c.group}/${c.id}`,
            title: c.title,
            meta: `${v.jobs?.find((j) => j.id === job)?.stages?.find((x) => x.id === c.stage)?.label ?? c.stage} · ${c.kind}`,
            on: on?.group === c.group && on.id === c.id,
            attention: c.needs_you,
            draft:
              drafts.picks.has(String(c.thread)) ||
              !!drafts.get(noteKey(c.thread)).trim(),
            select: () =>
              open({
                page: 'project',
                id,
                job,
                card: { group: c.group, id: c.id },
              }),
          }),
        );
    put(
      columns.leftBody!,
      h(
        'div',
        { class: 'muda-left' },
        indexSection({
          name: 'Project',
          empty: '',
          rows: [
            indexRow({
              id: 'overview',
              title: 'Each group’s numbers',
              on: !route.card && !route.stage,
              select: () =>
                open({ page: 'project', id, job: job || undefined }),
            }),
          ],
        }),
        jobGroups(v, job).map((g) =>
          indexSection({
            name: g.label || g.key,
            rows: rows(g),
            empty: 'Nothing here yet.',
          }),
        ),
      ),
    );
  }

  function noteBox(thread: number, label: string): HTMLElement {
    return drafts.box(noteKey(thread), label, 2).el;
  }

  function evidenceList(c: Card): HTMLElement {
    return h(
      'ul',
      { class: 'muda-evidence' },
      (c.evidence ?? []).map((e) =>
        h(
          'li',
          { 'data-evidence': e.ref },
          h(
            'div',
            { class: 'muda-evidence-head' },
            e.url
              ? h(
                  'a',
                  { href: e.url, target: '_blank', rel: 'noopener noreferrer' },
                  e.ref,
                )
              : h('span', null, e.ref),
            h('span', { class: 'kit-meta' }, e.kind.replace('_', ' ')),
          ),
          e.text ? h('p', null, e.text) : null,
        ),
      ),
    );
  }

  function cardEl(v: ProjectView, c: Card, inPlace: boolean): HTMLElement {
    const answer = pickOf(c.thread);
    const held = !!answer || !!drafts.get(noteKey(c.thread)).trim();
    const pill = (text: string, cls = '') =>
      h('span', { class: `kit-pill ${cls}` }, text);
    const sendable = questionsOf(
      threadOf(v, c.thread),
      [
        ...(server.questions.get(c.thread) ?? []),
        ...asks.filter((a) => a.thread === c.thread),
      ],
      WORDS,
    );
    const job = c.job;
    const openCard = () =>
      open({
        page: 'project',
        id,
        job,
        card: { group: c.group, id: c.id },
      });
    const body = h(
      'div',
      { class: 'muda-card-body' },
      inPlace
        ? h(
            'button',
            {
              class: 'muda-card-title muda-linkish',
              type: 'button',
              onclick: openCard,
            },
            c.title,
          )
        : h('h2', { class: 'muda-card-title' }, c.title),
      c.revision ? h('p', { class: 'muda-revision' }, c.revision) : null,
      c.decided
        ? h(
            'section',
            { 'data-part': 'decided' },
            h('div', { class: 'kit-label' }, 'What you already decided'),
            h('p', { class: 'muda-decided' }, `✓ ${c.decided}`),
          )
        : null,
      c.evidence?.length
        ? inPlace
          ? fold(`Evidence · ${c.evidence.length}`, evidenceList(c))
          : h(
              'section',
              { 'data-part': 'evidence' },
              h(
                'div',
                { class: 'kit-label' },
                `Evidence · ${c.evidence.length}`,
              ),
              evidenceList(c),
            )
        : null,
      c.recommendation
        ? h(
            'section',
            { 'data-part': 'recommendation' },
            h('div', { class: 'kit-label' }, 'The agent recommends'),
            h('p', null, c.recommendation),
          )
        : null,
      c.answers
        ? h(
            'section',
            { 'data-part': 'answers', 'data-kind': c.answers.kind },
            h('div', { class: 'kit-label' }, 'Your answer'),
            answerControls(c.answers, {
              answer,
              key: `answer:${c.thread}`,
              set: (a, typing) => setPick(c, a, typing),
            }),
          )
        : null,
      h(
        'div',
        { class: 'muda-asks' },
        buttons(
          WORDS.map((w) => {
            if (sendable.drafted.includes(w))
              return { label: `✓ In your drafts: ${w}`, disabled: true };
            return questionButton({
              label: w,
              asked: sendable.sent.includes(w),
              run: () => ask(c, w),
            });
          }),
        ),
      ),
      noteBox(
        c.thread,
        'A note: it goes with your answer, or on its own, on Send.',
      ),
    );
    body.addEventListener('focusin', () => {
      if (focus?.thread !== c.thread && inPlace) {
        focus = c;
        drawRight(v);
      }
    });
    const el = kitCard({
      edge: held ? 'wait' : c.needs_you ? 'signal' : undefined,
      // kit.d.ts says string; dom.js takes a node too (a gap in the types).
      head: h(
        'span',
        { class: 'muda-card-head' },
        h(
          'span',
          { class: 'kit-card-crumb' },
          `${c.kind} · ${groupLabel(v, c.group)} · ${c.id}`,
        ),
        c.class ? pill(c.class) : null,
        c.basis ? pill(c.basis, c.basis === 'measured' ? 'ok' : 'wait') : null,
        answer
          ? pill('draft', 'wait')
          : c.needs_you
            ? pill('needs you', 'muda-you')
            : null,
      ) as unknown as string,
      body,
    });
    el.dataset.card = `${c.group}/${c.id}`;
    return el;
  }

  function overview(v: ProjectView, job: string): HTMLElement {
    if (!v.jobs?.length)
      return h(
        'div',
        { class: 'muda-quiet' },
        h('p', { class: 'muda-quiet-lead' }, 'Nothing posted yet.'),
        h(
          'p',
          null,
          'Ask a session in one of these repositories to open this project with muda: its findings and plans show here, for you to answer.',
        ),
        h(
          'ul',
          { class: 'muda-plain' },
          (v.groups ?? []).map((g) =>
            h('li', { class: 'kit-key' }, g.label || g.key),
          ),
        ),
      );
    return h(
      'div',
      { class: 'muda-stack' },
      h('div', { class: 'kit-label' }, 'Each group’s own numbers'),
      h(
        'div',
        { class: 'muda-standings' },
        groupStandings(v, job).map((g) =>
          h(
            'section',
            { class: 'kit-card muda-standing', 'data-group': g.group.key },
            h('h2', { class: 'muda-card-title' }, g.group.label || g.group.key),
            h(
              'p',
              { class: 'kit-meta' },
              g.needsYou ? `${g.needsYou} wait on you` : 'Nothing waits on you',
            ),
            h(
              'ul',
              { class: 'muda-plain muda-counts' },
              g.stages.map((st) =>
                h(
                  'li',
                  null,
                  h(
                    'a',
                    {
                      href: hashOf({ page: 'project', id, job, stage: st.id }),
                    },
                    st.label,
                  ),
                  h('span', { class: 'kit-meta' }, String(st.count)),
                ),
              ),
            ),
            g.baselines.map((c) =>
              h(
                'div',
                { class: 'muda-baseline' },
                h(
                  'a',
                  {
                    href: hashOf({
                      page: 'project',
                      id,
                      job,
                      card: { group: c.group, id: c.id },
                    }),
                  },
                  c.title,
                ),
                evidenceList(c),
              ),
            ),
          ),
        ),
      ),
      h('div', { class: 'kit-label' }, 'A note on the whole project'),
      noteBox(v.thread, 'A note on no one card: it goes on Send.'),
    );
  }

  function centreOf(v: ProjectView): HTMLElement {
    const job = jobId(v);
    const c = route.card
      ? v.cards?.find(
          (x) =>
            x.job === job &&
            x.group === route.card?.group &&
            x.id === route.card?.id,
        )
      : null;
    if (c) return cardEl(v, c, false);
    if (route.stage) {
      const stage = v.jobs
        ?.find((j) => j.id === job)
        ?.stages?.find((x) => x.id === route.stage);
      const groups = stageCards(v, job, route.stage);
      return h(
        'div',
        { class: 'muda-stack', 'data-stage-page': route.stage },
        h('h2', { class: 'muda-page-title' }, stage?.label ?? route.stage),
        groups.length
          ? groups.map(([g, cards]) =>
              h(
                'section',
                { class: 'muda-stage-group', 'data-group': g.key },
                h('div', { class: 'kit-label' }, g.label || g.key),
                cards.map((x) => cardEl(v, x, true)),
              ),
            )
          : h('p', { class: 'kit-empty' }, 'Nothing at this stage yet.'),
      );
    }
    return overview(v, job);
  }

  /** The card whose thread is on the right, null for the project's own. */
  function threadCard(v: ProjectView): Card | null {
    const job = jobId(v);
    if (route.card)
      return (
        v.cards?.find(
          (x) =>
            x.job === job &&
            x.group === route.card?.group &&
            x.id === route.card?.id,
        ) ?? null
      );
    if (!route.stage) return null;
    const at = stageCards(v, job, route.stage).flatMap(([, cs]) => cs);
    const f = focus && at.find((x) => x.thread === focus?.thread);
    return f ?? at.find((x) => x.needs_you) ?? at[0] ?? null;
  }

  function drawRight(v: ProjectView): void {
    const c = threadCard(v);
    const threadId = c?.thread ?? v.thread;
    const messages = threadOf(v, threadId);
    put(
      columns.rightBody!,
      thread({
        id: String(threadId),
        name: c?.title ?? 'The whole project',
        key: c ? `${groupLabel(v, c.group)} · ${c.id}` : null,
        link: c
          ? hashOf({
              page: 'project',
              id,
              job: c.job,
              card: { group: c.group, id: c.id },
            }).slice(1)
          : undefined,
        messages: messages.map((m) => ({
          id: m.id,
          from: m.who === 'you' ? ('court' as const) : ('agent' as const),
          at: m.when,
          words: m.who === 'you' ? '' : m.who,
          state: m.who === 'you' ? m.state : undefined,
          stateLine: m.line,
          text: m.text,
        })),
      }),
    );
    columns.setRightEmpty(!messages.length, `${id}/${threadId}`);
  }

  function drawFoot(): void {
    if (!view) return;
    const n = sends();
    const job = jobId(view);
    const at = route.stage
      ? stageCards(view, job, route.stage).flatMap(([, cs]) => cs)
      : [];
    const asked = at.filter((c) => c.answers);
    const done = asked.filter(
      (c) => !c.needs_you || drafts.picks.has(String(c.thread)),
    ).length;
    foot.classList.toggle('held', n > 0);
    if (!n && !route.stage) {
      foot.replaceChildren();
      return;
    }
    put(
      foot,
      sendBar({
        count: asked.length
          ? `${done} of ${asked.length} answered`
          : n === 1
            ? '1 draft'
            : `${n} drafts`,
        sends: n,
        hint: view.owner_present
          ? `Goes to ${view.owner_label || 'the session'} together, on Send.`
          : 'Goes together, on Send; it waits for a session to open this project.',
        discard,
        send,
      }),
    );
  }

  function draw(): void {
    if (stopped) return;
    if (pressing) {
      pending = true;
      return;
    }
    pending = false;
    const v = view;
    if (!v) return;
    drawBand(v);
    drawLeft(v);
    put(centre, centreOf(v));
    drawRight(v);
    drawFoot();
  }

  /** Take the server's drafts as the truth, when no save of ours is on the way. */
  function adopt(v: ProjectView): void {
    server = sortDrafts(v.drafts ?? [], WORDS);
    drafts.picks.clear();
    for (const [t, d] of server.picks)
      drafts.picks.set(String(t), JSON.stringify(d.answer));
    const seen = new Set<string>();
    for (const [t, d] of server.notes) {
      seen.add(String(t));
      if (drafts.get(noteKey(t)) !== d.text) drafts.restore(noteKey(t), d.text);
    }
    for (const [t, text] of drafts.notes('note:'))
      if (!seen.has(t) && text.trim()) drafts.restore(`note:${t}`, '');
  }

  async function load(): Promise<void> {
    // The server's drafts are taken only from a read that no save overlapped:
    // one fetched while a save was running can hold a pick the save replaced.
    const at = edits;
    const settled = !busy && !timer && !saveError;
    const v = await s.api.get<ProjectView>(base);
    if (stopped) return;
    view = v;
    say();
    if (settled && at === edits && !busy && !timer && !saveError) {
      adopt(v);
      const u = restoring;
      restoring = null;
      if (u) restore(v, u);
    }
    draw();
  }

  addEventListener('pagehide', keepUnsaved, { signal });

  void load();
  return {
    stop() {
      stopped = true;
      stopping.abort();
      if (drafts.dirty() || unsavedPicks.size || asks.length)
        void flush().then(keepUnsaved);
    },
    changed(p) {
      if (p === id || p === 0) void load();
    },
    go(r) {
      route = r as ProjectRoute;
      focus = null;
      draw();
      columns.centre.scrollTo({ top: 0 });
    },
  };
}
