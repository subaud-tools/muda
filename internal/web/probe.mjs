// probe.mjs: the build-time browser check of muda's page. It seeds a real serve
// (serve-fixture.mjs) with galley's record, shortened, and checks at 1280 and 390
// wide, by what is visible: the front page lists the project; New project ticks
// two repositories from a temp folder; a card shows its evidence and answers;
// each of the four answers becomes a draft; a pick and a note stay drafts through
// a reload until Send delivers them as one Send, which the agent status lists; a
// second session opening the project takes its unanswered messages; no console
// errors; nothing wider than the window. Then, once each, the ways drafts could
// be lost: an edit during Send, a reload straight after typing, a failed save.
// Screenshots go to $PROBE_OUT.
//
// KIT_BROWSER=required: no chromium is a failure. BROWSER SAFETY: headless, and
// the serve is --no-open (serve-fixture.mjs).

import { mkdirSync, mkdtempSync, realpathSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  check,
  drawn,
  launchOrSkip,
  onCleanup,
  run,
  sleep,
  until,
} from './build/probe.mjs';
import { startServe } from './serve-fixture.mjs';

const out =
  process.env.PROBE_OUT ||
  mkdtempSync(join(realpathSync(tmpdir()), 'muda-probe-shots-'));
mkdirSync(out, { recursive: true });

const GALLEY = '/work/galley';
const cardHash = (id, group, card) =>
  `#/p/${id}/j/deploy/c/${encodeURIComponent(group)}/${card}`;

async function atWidth(browser, width) {
  console.log(`\n== ${width} wide`);
  const serve = await startServe();
  onCleanup(() => serve.stop());
  const family = serve.projects['Family tools'];
  const view = (id = family) => serve.call('GET', `/api/projects/${id}`);
  const status = () => serve.call('GET', '/api/agent/status');

  const context = await browser.newContext({
    viewport: { width, height: 844 },
  });
  const page = await context.newPage();
  const problems = [];
  page.on('console', (m) => {
    if (m.type() === 'error') problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));

  const go = async (hash) => {
    await page.evaluate((h) => {
      location.hash = h;
    }, hash);
    await sleep(150);
  };
  const shot = async (name, full = width < 640) => {
    for (const scheme of ['light', 'dark']) {
      await page.emulateMedia({ colorScheme: scheme });
      await page.screenshot({
        path: join(out, `${name}-${width}-${scheme}.png`),
        fullPage: full,
      });
    }
    await page.emulateMedia({ colorScheme: 'light' });
  };
  // The middle of the window: clear of the send bar held at its foot.
  const reveal = (locator) =>
    locator.first().evaluate((e) => e.scrollIntoView({ block: 'center' }));
  const fits = async (where) => {
    const wide = await page.evaluate(() => {
      const w = innerWidth;
      if (document.documentElement.scrollWidth > w) return 'the page';
      for (const e of document.querySelectorAll('#app *')) {
        const r = e.getBoundingClientRect();
        if (r.width && r.height && r.right > w + 1 && e.checkVisibility())
          return `${e.tagName}.${e.className}`;
      }
      return '';
    });
    check(`${where}: nothing wider than the window`, !wide, wide);
  };

  // ---- the front page
  await page.goto(serve.url);
  const row = page.locator(`.muda-row[data-project="${family}"]`);
  await row.waitFor();
  check(
    'the front page lists the project under Needs you',
    await page
      .locator('[data-group="needs"] .muda-row-title', {
        hasText: 'Family tools',
      })
      .isVisible(),
  );
  check(
    'its row says who it waits on',
    /wait on you/.test(await row.locator('[data-badge="you"]').innerText()),
  );
  check(
    'the status pills count the groups',
    (await page.locator('.muda-pills .kit-stage').count()) >= 3,
  );
  await shot('front');
  await fits('front page');

  // ---- New project
  await page.getByRole('button', { name: 'New project' }).click();
  await page.locator('input[name="title"]').fill('Picked by hand');
  await page.locator('input[name="folder"]').fill(serve.folder);
  await page.locator('[data-act="scan"]').click();
  const tick = (name) => page.locator('.muda-tick', { hasText: name });
  await tick('acme/alpha').waitFor();
  check(
    'the scan lists the GitHub repositories to two levels, and only those',
    (await tick('acme/gamma').isVisible()) &&
      (await tick('elsewhere').count()) === 0,
  );
  check(
    'each says whether it has a record',
    /has a record/.test(await tick('acme/alpha').innerText()) &&
      /no record yet/.test(await tick('acme/beta').innerText()),
  );
  await tick('acme/alpha').locator('input').check();
  await tick('acme/beta').locator('input').check();
  await shot('new-project');
  await fits('New project');
  await page.getByRole('button', { name: 'Create project' }).click();
  await page.waitForFunction(() => location.hash.startsWith('#/p/'));
  const picked = Number(await page.evaluate(() => location.hash.split('/')[2]));
  const made = await view(picked);
  check(
    'New project made the project with the two ticked',
    made.title === 'Picked by hand' &&
      made.groups.map((g) => g.label).join() === 'acme/alpha,acme/beta',
    JSON.stringify(made.groups),
  );
  await page.locator('.muda-quiet').waitFor();
  await fits('an empty project');

  // ---- the switcher and a card
  await go(`#/p/${family}`);
  await page.locator('.muda-standing').first().waitFor();
  check(
    'each group has its numbers, and nothing adds them up',
    (await page.locator('.muda-standing').count()) === 2 &&
      !/total/i.test(await page.locator('.muda-centre').innerText()),
  );
  check(
    'the tabs show one per job',
    (await page.locator('.muda-tabs [data-job]').count()) === 2,
  );
  await shot('project');
  await fits('project');

  await go(cardHash(family, GALLEY, 'F-002'));
  const card = page.locator('.kit-card[data-card$="/F-002"]');
  await card.waitFor();
  const ev = card.locator('[data-part="evidence"] li');
  check('a card shows its evidence', (await ev.count()) === 4);
  check(
    'an evidence entry with a URL is a link',
    /^https:\/\/github.com\//.test(
      (await ev.first().locator('a').getAttribute('href')) ?? '',
    ),
  );
  check(
    'a card shows its class and basis',
    /W2/.test(await card.locator('.kit-card-head').innerText()) &&
      /measured/i.test(await card.locator('.kit-card-head').innerText()),
  );
  const approve = card.locator('[data-option="approve"]');
  check('a card shows its answers', await approve.isVisible());
  check(
    'an empty thread folds to its strip',
    await page.locator('.kit-right-col.kit-collapsed').isVisible(),
  );
  await fits('a card');

  // ---- a pick and a note, then a reload
  await approve.click();
  const note = card.locator('textarea.kit-draft');
  await note.fill('Pin it in package.json, please.');
  check(
    'a pick and a note wait to be saved as two drafts',
    await until(async () => (await view()).drafts.length === 2),
  );
  const held = (await view()).drafts;
  check(
    "the pick's draft carries its answer in words",
    held.some((d) => d.answer?.choice === 'approve' && d.text === 'Approve'),
  );
  check(
    'nothing has reached the agent',
    (await status()).unsettled.length === 0 &&
      (await view()).messages.length === 0,
  );
  check(
    'the send bar counts two',
    await page.getByRole('button', { name: 'Send 2' }).isVisible(),
  );
  await shot('card-drafts');
  await page.reload();
  await card.waitFor();
  check(
    'a reload keeps the pick and the note',
    (await approve.getAttribute('aria-pressed')) === 'true' &&
      (await note.inputValue()) === 'Pin it in package.json, please.',
  );

  // ---- changing a pick replaces it
  await card.locator('[data-option="exempt"]').click();
  await card.locator('[data-option="approve"]').click();
  await until(
    async () =>
      (await view()).drafts.filter((d) => d.answer?.choice === 'approve')
        .length === 1 && (await view()).drafts.length === 2,
  );
  check(
    'a changed pick is one draft, not two',
    (await view()).drafts.filter((d) => d.answer).length === 1,
  );

  // ---- the stage page and the other three answers
  await go(`#/p/${family}/j/deploy/s/findings`);
  const stage = page.locator('[data-stage-page="findings"]');
  await stage.waitFor();
  check(
    'a stage opens every card at it, answerable in place',
    (await stage.locator('.kit-card[data-card]').count()) === 4 &&
      (await stage.locator('[data-part="answers"]').count()) === 4,
  );
  check(
    'the pick made on the card shows on the stage page',
    (await stage
      .locator(
        '.kit-card[data-card$="/work/galley/F-002"] [data-option="approve"]',
      )
      .getAttribute('aria-pressed')) === 'true',
  );

  const order = stage.locator('[data-kind="order"]');
  const rows = order.locator('.muda-order-row');
  const first = await rows.first().getAttribute('data-row');
  await reveal(rows.last());
  await sleep(100);
  const from = await rows.last().locator('.muda-grip').boundingBox();
  const to = await rows.first().boundingBox();
  await page.mouse.move(from.x + 5, from.y + 5);
  await page.mouse.down();
  await page.mouse.move(from.x + 5, to.y + 2, { steps: 6 });
  await page.mouse.up();
  await until(async () =>
    (await view()).drafts.some((d) => d.answer?.kind === 'order'),
  );
  const ordered = (await view()).drafts.find((d) => d.answer?.kind === 'order');
  check(
    'dragging a row to the top makes an order draft',
    ordered?.answer.order?.[0] === 'F-001' && ordered.answer.order[1] === first,
    JSON.stringify(ordered?.answer),
  );

  const fields = stage.locator('[data-kind="fields"]');
  await fields.locator('[data-field="reason"]').fill('Dependabot comes first.');
  await fields.locator('[data-field="review-by"]').fill('2027-01-04');
  await until(async () =>
    (await view()).drafts.some(
      (d) => d.answer?.fields?.['review-by'] === '2027-01-04',
    ),
  );
  check(
    'fields become one draft with the reason and date',
    (await view()).drafts.filter((d) => d.answer?.kind === 'fields').length ===
      1,
  );
  check(
    'a field keeps its text through the save',
    (await page
      .locator('[data-kind="fields"] [data-field="reason"]')
      .inputValue()) === 'Dependabot comes first.',
  );
  await reveal(fields);
  await shot('stage-drafts');
  await fits('a stage page');

  await go(`#/p/${family}/j/deploy/s/baseline`);
  const menus = page.locator('[data-kind="rows"]').first();
  await menus.waitFor();
  check(
    'a choice per row shows as a menu per row',
    (await menus.locator('select').count()) === 4,
  );
  check(
    "a row's menu starts on the agent's choice",
    (await menus
      .locator('select[data-row=".github/workflows/ci.yml"]')
      .inputValue()) === 'gate',
  );
  await menus
    .locator('select[data-row="dynamic/dependabot/dependabot-updates"]')
    .selectOption('other');
  check(
    'a menu choice becomes a draft in words',
    await until(async () =>
      (await view()).drafts.some(
        (d) =>
          d.answer?.kind === 'rows' &&
          d.text.includes('dependabot-updates: Other'),
      ),
    ),
  );

  // Each answer kind, on its own card, for the look.
  for (const [name, id] of [
    ['answer-buttons', 'F-002'],
    ['answer-order', 'order'],
    ['answer-fields', 'F-003'],
    ['answer-rows', 'baseline'],
  ]) {
    await go(cardHash(family, GALLEY, id));
    const part = page.locator(
      `.kit-card[data-card$="/${id}"] [data-part="answers"]`,
    );
    await part.waitFor();
    await reveal(part);
    await shot(name);
    await fits(name);
  }

  // ---- Send
  await go(`#/p/${family}/j/deploy/s/findings`);
  await stage.waitFor();
  const before = (await view()).drafts.length;
  check('five drafts are held before Send', before === 5, String(before));
  await page.getByRole('button', { name: `Send ${before}` }).click();
  check(
    'Send empties the drafts',
    await until(async () => (await view()).drafts.length === 0),
  );
  const sent = await status();
  const mine = sent.unsettled.filter((u) => u.project === family);
  check(
    'Send delivers them as one Send, which the status lists',
    mine.length === 1 && mine[0].messages.length === before,
    JSON.stringify(mine),
  );
  check(
    'the page clears what Send took',
    await until(
      async () =>
        (await page
          .locator('[data-kind="fields"] [data-field="reason"]')
          .inputValue()) === '' &&
        (await page.getByRole('button', { name: 'Send 0' }).isVisible()),
    ),
  );

  // The owner hears it: the status names who has it.
  await serve.call('POST', '/api/agent/presence', {
    session: 'seed',
    label: 'pi',
  });
  const woke = await serve.call(
    'GET',
    '/api/agent/wait?session=seed&timeout=2',
  );
  check(
    'the owner reads all of it together',
    woke?.text.includes('Pin it in package.json') &&
      woke.text.includes('Dependabot comes first.'),
    woke?.text,
  );
  check(
    'the status says the owner holds the Send',
    (await status()).unsettled.find((u) => u.project === family)?.with ===
      'seed',
  );

  await go(cardHash(family, GALLEY, 'F-002'));
  await card.waitFor();
  const msg = page.locator('.kit-thread-msg.court');
  await msg.first().waitFor();
  check(
    'a thread with a message is open, its state on the message',
    (await page
      .locator('.kit-right-col')
      .evaluate((e) => !e.classList.contains('kit-collapsed'))) &&
      (await msg.first().locator('.kit-pill[data-state]').isVisible()),
  );
  await shot('card-thread');
  await fits('a card with its thread');

  // ---- a second session
  await serve.call('POST', '/api/agent/open', {
    session: 'later',
    label: 'claude',
    project: family,
  });
  check(
    'a second session opening it shows as the owner',
    await until(async () =>
      /claude/.test(await page.locator('.muda-owner').innerText()),
    ),
  );
  const taken = await serve.call(
    'GET',
    '/api/agent/wait?session=later&timeout=2',
  );
  check(
    'it takes the unanswered messages',
    taken?.text.includes('Pin it in package.json'),
    taken?.text,
  );
  await serve.call('POST', '/api/agent/reply', {
    session: 'later',
    sends: [taken.id],
    state: 'answered',
    text: 'Done: pinned, and the order is noted.',
  });
  check(
    "the agent's answer shows in the thread, and the message reads answered",
    await until(
      async () =>
        (await page.locator('.kit-thread-msg.agent').count()) > 0 &&
        (await msg.first().locator('[data-state="answered"]').isVisible()),
    ),
  );
  await shot('card-answered');
  check(
    'the status has nothing left to settle',
    (await status()).unsettled.every((u) => u.project !== family),
  );

  // ---- muda_open's address, and a tab reused
  const tab = await context.newPage();
  await tab.goto(`${serve.url}#/p/${serve.projects['Client delivery']}`);
  await tab.locator('.muda-standing,.muda-quiet').first().waitFor();
  check(
    "muda_open's address opens the project",
    /Client delivery/.test(await tab.locator('.kit-status').textContent()),
  );
  await tab.close();

  const front = await context.newPage();
  await front.goto(serve.url);
  await front.locator('.muda-row').first().waitFor();
  await sleep(300);
  await page.close();
  await serve.call('POST', '/api/agent/open', {
    session: 'third',
    label: 'pi',
    project: picked,
  });
  check(
    'a front page tab moves to a project an agent opens',
    await until(() => front.url().endsWith(`#/p/${picked}`)),
    front.url(),
  );

  check(
    'no console errors',
    problems.length === 0,
    problems.slice(0, 3).join(' | '),
  );
  check(
    'the window was drawn',
    drawn(await front.locator('.kit-bar').boundingBox()),
  );
  await context.close();
}

// Drafts that must survive: each on its own serve, with the network held or
// failed by the browser so the timing is the check's.
async function draftsPage(browser) {
  const serve = await startServe();
  onCleanup(() => serve.stop());
  const family = serve.projects['Family tools'];
  const context = await browser.newContext({
    viewport: { width: 1280, height: 844 },
  });
  const page = await context.newPage();
  const card = page.locator('.kit-card[data-card$="/F-002"]');
  await page.goto(`${serve.url}${cardHash(family, GALLEY, 'F-002')}`);
  await card.waitFor();
  return {
    page,
    card,
    family,
    note: card.locator('textarea.kit-draft'),
    pressed: (option) =>
      card.locator(`[data-option="${option}"]`).getAttribute('aria-pressed'),
    view: () => serve.call('GET', `/api/projects/${family}`),
    status: () => serve.call('GET', '/api/agent/status'),
    close: () => context.close(),
  };
}

async function editsDuringSend(browser) {
  console.log('\n== edits made while Send runs');
  const d = await draftsPage(browser);
  await d.card.locator('[data-option="approve"]').click();
  await d.note.fill('The note that went.');
  await until(async () => (await d.view()).drafts.length === 2);
  let release;
  const held = new Promise((r) => (release = r));
  await d.page.route('**/send', async (route) => {
    const response = await route.fetch();
    await held;
    await route.fulfill({ response });
  });
  await d.page.getByRole('button', { name: 'Send 2' }).click();
  await until(async () => (await d.view()).messages.length === 2);
  // The server has taken the batch; its answer is still on the way.
  await d.card.locator('[data-option="exempt"]').click();
  await d.note.fill('The note written after.');
  release();
  await until(async () => (await d.view()).drafts.length === 2);
  await sleep(700);
  const v = await d.view();
  const got = {
    sent: v.messages.map((m) => m.text).sort(),
    drafts: v.drafts.map((x) => x.answer?.choice ?? x.text).sort(),
    box: await d.note.inputValue(),
    pressed: await d.pressed('exempt'),
  };
  check(
    'an edit made while Send runs stays a draft and is saved',
    JSON.stringify(got) ===
      JSON.stringify({
        sent: ['Approve', 'The note that went.'],
        drafts: ['The note written after.', 'exempt'],
        box: 'The note written after.',
        pressed: 'true',
      }),
    JSON.stringify(got),
  );
  await d.close();
}

async function typedThenReload(browser) {
  console.log('\n== a reload straight after typing');
  const d = await draftsPage(browser);
  const stage = d.page.locator('[data-stage-page="findings"]');
  await d.page.evaluate((h) => {
    location.hash = h;
  }, `#/p/${d.family}/j/deploy/s/findings`);
  await stage.waitFor();
  const note = stage
    .locator('.kit-card[data-card$="/F-002"] textarea.kit-draft')
    .first();
  const reason = () => d.page.locator('[data-field="reason"]').first();
  await note.fill('Typed just before the reload.');
  await reason().fill('Also just before.');
  await d.page.reload();
  await stage.waitFor();
  await until(async () => (await d.view()).drafts.length === 2);
  await sleep(700);
  const got = {
    note: await note.inputValue(),
    reason: await reason().inputValue(),
    saved: (await d.view()).drafts
      .map((x) => x.answer?.fields?.reason ?? x.text)
      .sort(),
  };
  check(
    'a reload straight after typing keeps the note and the field, and saves them',
    JSON.stringify(got) ===
      JSON.stringify({
        note: 'Typed just before the reload.',
        reason: 'Also just before.',
        saved: ['Also just before.', 'Typed just before the reload.'],
      }),
    JSON.stringify(got),
  );
  await d.close();
}

async function failedSave(browser) {
  console.log('\n== a save that fails');
  const d = await draftsPage(browser);
  let failing = false;
  let sends = 0;
  await d.page.route('**/api/projects/*/drafts**', (route) =>
    failing
      ? route.fulfill({ status: 500, json: { error: 'disk full' } })
      : route.continue(),
  );
  await d.page.route('**/send', (route) => {
    sends++;
    return route.continue();
  });
  await d.note.fill('This one saved.');
  await until(async () => (await d.view()).drafts.length === 1);
  failing = true;
  await d.card.locator('[data-option="approve"]').click();
  await sleep(400);
  const said = () => d.page.locator('.kit-status').textContent();
  const held = async () => ({
    said: /disk full/.test(await said()),
    pressed: await d.pressed('approve'),
    sends,
    drafts: (await d.view()).drafts.length,
  });
  const after = await held();
  await d.page.getByRole('button', { name: /^Send \d/ }).click();
  await sleep(400);
  const refused = await held();
  await d.page.reload();
  await d.card.waitFor();
  await sleep(400);
  const reloaded = await held();
  failing = false;
  await d.page
    .getByRole('button', { name: 'Send 2' })
    .click({ timeout: 3000 })
    .catch(() => {});
  await until(async () => (await d.view()).messages.length === 2);
  const went = (await d.view()).messages.map((m) => m.text).sort();
  const want = { said: true, pressed: 'true', sends: 0, drafts: 1 };
  const got = { after, refused, reloaded, went };
  check(
    'a failed save stops Send, says so and keeps the pick until it is saved and sent',
    JSON.stringify(got) ===
      JSON.stringify({
        after: want,
        refused: want,
        reloaded: want,
        went: ['Approve', 'This one saved.'],
      }),
    JSON.stringify(got),
  );
  await d.close();
}

run(async () => {
  const browser = await launchOrSkip();
  if (!browser) return;
  for (const width of [1280, 390]) await atWidth(browser, width);
  await editsDuringSend(browser);
  await typedThenReload(browser);
  await failedSave(browser);
  console.log(`\nscreenshots: ${out}`);
});
