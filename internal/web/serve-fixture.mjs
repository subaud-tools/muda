// serve-fixture.mjs: a seeded `muda serve` for the probe, and for looking at the
// page by hand: `node serve-fixture.mjs` prints the address and stays up.
//
// Builds the muda binary into a temp dir, starts it with its own state
// (MUDA_HOME and XDG_STATE_HOME under a temp dir, so nothing touches the real
// one) on a free port, and posts testdata/galley.json through the agent API as
// a session would. Makes a folder of Git repositories for New project to scan.
//
// BROWSER SAFETY: serve runs --foreground --no-open, and the agent API opens no
// browser; only `muda channel` does, and nothing here runs it.

import { execFileSync, spawn } from 'node:child_process';
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { until } from './build/probe.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '../..');
const SEED = join(here, 'testdata', 'galley.json');

function gitRepo(dir, origin, record) {
  mkdirSync(dir, { recursive: true });
  const git = (...a) => execFileSync('git', ['-C', dir, ...a]);
  git('init', '-q');
  git('remote', 'add', 'origin', origin);
  if (record) {
    mkdirSync(join(dir, '.muda'));
    writeFileSync(join(dir, '.muda', 'muda.toml'), 'schema = 1\n');
  }
}

/** Start a serve holding the seeded projects. Returns {url, base, token, agent,
 * folder, projects, stop}: agent(path, body) calls the API as a session would,
 * folder holds repositories to scan, projects are the seeded ones' ids by title. */
export async function startServe() {
  const work = mkdtempSync(join(realpathSync(tmpdir()), 'muda-probe-'));
  const state = join(work, 'state');
  mkdirSync(state);
  const env = { ...process.env, XDG_STATE_HOME: state, MUDA_HOME: state };
  const bin = process.env.MUDA_BIN || join(work, 'muda');
  if (!process.env.MUDA_BIN) {
    console.error('[serve-fixture] building muda…');
    execFileSync('go', ['build', '-o', bin, './cmd/muda'], {
      cwd: repoRoot,
      stdio: 'inherit',
    });
  }
  const folder = join(work, 'repos');
  gitRepo(join(folder, 'alpha'), 'git@github.com:acme/alpha.git', true);
  gitRepo(join(folder, 'beta'), 'https://github.com/acme/beta.git', false);
  gitRepo(
    join(folder, 'group', 'gamma'),
    'git@github.com:acme/gamma.git',
    false,
  );
  gitRepo(
    join(folder, 'elsewhere'),
    'https://gitlab.com/acme/elsewhere.git',
    false,
  );

  let stderr = '';
  const proc = spawn(bin, ['serve', '--foreground', '--no-open'], {
    env,
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  proc.stderr.on('data', (d) => {
    stderr += String(d);
  });
  const live = () => {
    const out = execFileSync('find', [state, '-name', 'serve.json'], {
      encoding: 'utf8',
    }).trim();
    const a = JSON.parse(readFileSync(out.split('\n')[0], 'utf8'));
    return a.url && a.token && a;
  };
  const adv = await until(live, 15000);
  if (!adv) {
    proc.kill();
    throw new Error(`muda serve did not start\nstderr: ${stderr}`);
  }
  const base = new URL(adv.url).origin;

  async function call(method, path, body) {
    const res = await fetch(base + path, {
      method,
      headers: {
        'X-Local-Token': adv.token,
        'Content-Type': 'application/json',
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
    return text ? JSON.parse(text) : null;
  }

  const projects = {};
  const seed = JSON.parse(readFileSync(SEED, 'utf8'));
  for (const p of seed.projects) {
    const session = 'seed';
    const opened = await call('POST', '/api/agent/open', {
      session,
      label: 'pi',
      title: p.title,
      groups: p.groups,
    });
    projects[p.title] = opened.project;
    await call('POST', '/api/agent/post', {
      session,
      project: opened.project,
      ...p.post,
    });
  }

  return {
    url: adv.url,
    base,
    token: adv.token,
    folder,
    projects,
    call,
    async stop() {
      if (proc.exitCode === null) {
        proc.kill('SIGTERM');
        await until(() => proc.exitCode !== null, 3000);
        if (proc.exitCode === null) proc.kill('SIGKILL');
      }
      rmSync(work, { recursive: true, force: true });
    },
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const s = await startServe();
  console.log(`\n  ${s.url}\n`);
  console.log(`  Repositories for New project: ${s.folder}`);
  console.log('  Stop with Ctrl-C.\n');
  const end = () => void s.stop().finally(() => process.exit(0));
  process.on('SIGINT', end);
  process.on('SIGTERM', end);
}
