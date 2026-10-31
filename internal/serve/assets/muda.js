// entry.ts
import {
  ApiError as ApiError2,
  bar,
  createApi,
  initTheme,
  keepElsewhere,
  live
} from "/_kit/kit.js";

// home.ts
import {
  filterRow,
  frame,
  groupList,
  h as h2,
  stagePill,
  statusPills,
  workingLine
} from "/_kit/kit.js";

// model.ts
var enc = encodeURIComponent;
function hashOf(r) {
  if (r.page === "home") return "#/";
  if (r.page === "new") return "#/new";
  const base = `#/p/${r.id}`;
  if (r.add) return `${base}/add`;
  if (!r.job) return base;
  const job = `${base}/j/${enc(r.job)}`;
  if (r.stage) return `${job}/s/${enc(r.stage)}`;
  if (r.card) return `${job}/c/${enc(r.card.group)}/${enc(r.card.id)}`;
  return job;
}
function parseHash(hash) {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean);
  if (parts[0] === "new") return { page: "new" };
  const id = Number(parts[1]);
  if (parts[0] !== "p" || !Number.isInteger(id) || id <= 0)
    return { page: "home" };
  if (parts[2] === "add") return { page: "project", id, add: true };
  if (parts[2] !== "j" || !parts[3]) return { page: "project", id };
  const job = decodeURIComponent(parts[3]);
  if (parts[4] === "s" && parts[5])
    return { page: "project", id, job, stage: decodeURIComponent(parts[5]) };
  if (parts[4] === "c" && parts[5] && parts[6])
    return {
      page: "project",
      id,
      job,
      card: {
        group: decodeURIComponent(parts[5]),
        id: decodeURIComponent(parts[6])
      }
    };
  return { page: "project", id, job };
}
function pageKey(r) {
  return r.page === "project" ? String(r.id) : "";
}
var FRONT_GROUPS = [
  {
    key: "needs",
    title: "Needs you",
    empty: "Nothing is waiting on you.",
    edge: "signal"
  },
  {
    key: "agent",
    title: "With the agent",
    empty: "No project is with a session.",
    edge: "agent"
  },
  { key: "quiet", title: "Quiet", empty: "Every project has work waiting." }
];
function frontGroup(r) {
  if (r.on_you > 0) return "needs";
  if (r.on_agent > 0 || r.working) return "agent";
  return "quiet";
}
function statusItems(rows) {
  return [
    { value: "", label: "All", count: rows.length },
    ...FRONT_GROUPS.map((g) => ({
      value: g.key,
      label: g.title,
      count: rows.filter((r) => frontGroup(r) === g.key).length
    }))
  ];
}
function jobFilterItems(rows) {
  const counts = /* @__PURE__ */ new Map();
  for (const r of rows)
    for (const j of r.jobs ?? [])
      counts.set(j.label, (counts.get(j.label) ?? 0) + 1);
  return [...counts.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([label, count]) => ({ value: label, label, count }));
}
function filterByJob(rows, chosen) {
  if (!chosen.length) return rows;
  return rows.filter(
    (r) => (r.jobs ?? []).some((j) => chosen.includes(j.label))
  );
}
var waits = (n) => n === 1 ? "1 waits on you" : `${n} wait on you`;
function rowLine(r) {
  if (r.on_you > 0) return waits(r.on_you);
  if (r.on_agent > 0) return `${r.on_agent} with the agent`;
  return "";
}
function elsewhereRows(rows, here) {
  return rows.map((r) => ({
    id: r.id,
    label: r.title,
    detail: rowLine(r),
    tone: r.on_you > 0 ? "you" : r.on_agent > 0 || r.working ? "working" : "idle",
    current: r.id === here
  }));
}
function waitsOf(p) {
  const out = [];
  if (p.on_you > 0) out.push({ text: waits(p.on_you), who: "you" });
  if (p.on_agent > 0) {
    const who = p.owner_label || "the agent";
    out.push({ text: `${p.on_agent} with ${who}`, who });
  }
  return out;
}
var cardsOfJob = (v, job) => (v.cards ?? []).filter((c) => c.job === job);
function jobGroups(v, job) {
  const groups = v.groups ?? [];
  const cards = cardsOfJob(v, job);
  const kinds = new Set(
    groups.filter((g) => cards.some((c) => c.group === g.key)).map((g) => g.kind)
  );
  return kinds.size ? groups.filter((g) => kinds.has(g.kind)) : groups;
}
function groupStandings(v, job) {
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
        count: mine.filter((c) => c.stage === s.id).length
      })),
      baselines: mine.filter((c) => c.kind === "baseline")
    };
  });
}
function stageCards(v, job, stage) {
  const cards = cardsOfJob(v, job).filter((c) => c.stage === stage);
  return (v.groups ?? []).flatMap((g) => {
    const mine = cards.filter((c) => c.group === g.key);
    return mine.length ? [[g, mine]] : [];
  });
}
var threadOf = (v, thread2) => (v.messages ?? []).filter((m) => m.thread === String(thread2));
function optionLabel(options, id) {
  return options?.find((o) => o.id === id)?.label ?? id;
}
function orderOf(a, answer) {
  const ids = (a.rows ?? []).map((r) => r.id);
  const kept = (answer?.order ?? []).filter((id) => ids.includes(id));
  return [...kept, ...ids.filter((id) => !kept.includes(id))];
}
function answerText(a, answer) {
  switch (a.kind) {
    case "buttons":
      return optionLabel(a.options, answer.choice ?? "");
    case "rows":
      return (a.rows ?? []).filter((r) => answer.rows?.[r.id]).map((r) => `${r.label}: ${optionLabel(r.options, answer.rows[r.id])}`).join("; ");
    case "order":
      return `Order: ${orderOf(a, answer).map((id) => optionLabel(a.rows, id)).join(", ")}`;
    default:
      return (a.fields ?? []).filter((f) => answer.fields?.[f.id]?.trim()).map((f) => `${f.label}: ${answer.fields[f.id].trim()}`).join("; ");
  }
}
function defaultAnswer(a) {
  if (a.kind === "order") return { kind: "order", order: orderOf(a, null) };
  if (a.kind !== "rows") return null;
  const rows = {};
  for (const r of a.rows ?? []) if (r.chosen) rows[r.id] = r.chosen;
  return Object.keys(rows).length ? { kind: "rows", rows } : null;
}
function moved(ids, id, to) {
  if (to < 0 || to >= ids.length) return ids;
  const rest = ids.filter((x) => x !== id);
  rest.splice(to, 0, id);
  return rest;
}
function sortDrafts(ds, words) {
  const out = {
    picks: /* @__PURE__ */ new Map(),
    notes: /* @__PURE__ */ new Map(),
    questions: /* @__PURE__ */ new Map()
  };
  for (const d of ds) {
    if (d.answer) out.picks.set(d.thread, d);
    else if (words.includes(d.text))
      out.questions.set(d.thread, [...out.questions.get(d.thread) ?? [], d]);
    else out.notes.set(d.thread, d);
  }
  return out;
}
function sendCount(o) {
  return o.picks + o.questions + o.notes.filter((n) => n.trim()).length;
}
var SETTLED = ["answered", "declined", "failed", "no_reply"];
function questionsOf(messages, drafts, words) {
  return {
    sent: words.filter(
      (w) => messages.some(
        (m) => m.text === w && m.state && !SETTLED.includes(m.state)
      )
    ),
    drafted: words.filter((w) => drafts.some((d) => d.text === w))
  };
}
function tilde(path, home2) {
  if (home2 && (path === home2 || path.startsWith(`${home2}/`)))
    return `~${path.slice(home2.length)}`;
  return path;
}

// shell.ts
import { h } from "/_kit/kit.js";
function band(...children) {
  return h("header", { class: "muda-band" }, children);
}
var phone = () => matchMedia("(width <= 640px)").matches;

// home.ts
function keptList(key) {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}
function toggled(list, v) {
  if (!v) return [];
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}
function rowOf(r) {
  const line = rowLine(r);
  const who = r.owner ? `${r.owner_label || r.owner}${r.owner_present ? "" : " · away"}` : "no session yet";
  return h2(
    "li",
    { class: "muda-row", "data-project": r.id },
    h2(
      "a",
      { class: "muda-row-link", href: `#/p/${r.id}` },
      h2("h2", { class: "muda-row-title" }, r.title),
      h2(
        "div",
        { class: "muda-row-sub" },
        (r.jobs ?? []).map(
          (j) => stagePill({
            key: j.id,
            label: j.stage ? `${j.label}: ${j.stage}` : j.label,
            count: j.count
          })
        ),
        h2(
          "span",
          { class: "muda-row-meta" },
          `${r.groups} ${r.groups === 1 ? "group" : "groups"} · ${who}`
        )
      ),
      workingLine({ text: r.working || null })
    ),
    line ? h2(
      "span",
      {
        class: `kit-pill kit-badge ${r.on_you ? "t-you" : "t-wait"}`,
        "data-badge": r.on_you ? "you" : "agent"
      },
      line
    ) : null
  );
}
function home(s) {
  s.bar.setPrimary({ label: "New project", run: () => s.go({ page: "new" }) });
  const columns = frame({
    name: "muda",
    left: { panel: "filters", label: "Filters" },
    centre: { label: "Projects" }
  });
  const pills = h2("div", { class: "muda-pills" });
  columns.centre.classList.add("muda-wide");
  s.root.append(
    band(h2("h1", { class: "muda-title" }, "Projects"), pills),
    columns.left,
    columns.centre
  );
  let rows = [];
  let status = keptList("muda.home.status");
  let jobs = keptList("muda.home.job");
  const folded = new Set(keptList("muda.home.folded"));
  function draw() {
    const items = jobFilterItems(rows);
    jobs = jobs.filter((j) => items.some((i) => i.value === j));
    columns.left.hidden = items.length === 0;
    columns.leftBody.replaceChildren(
      filterRow({
        label: "Jobs",
        items,
        chosen: jobs,
        onToggle(v) {
          jobs = toggled(jobs, v);
          localStorage.setItem("muda.home.job", JSON.stringify(jobs));
          draw();
        }
      })
    );
    const shown = filterByJob(rows, jobs);
    pills.replaceChildren(
      statusPills({
        pills: statusItems(shown),
        chosen: status,
        onToggle(v) {
          status = toggled(status, v);
          localStorage.setItem("muda.home.status", JSON.stringify(status));
          draw();
        }
      })
    );
    if (!rows.length) {
      columns.centre.replaceChildren(
        h2(
          "div",
          { class: "muda-quiet" },
          h2("p", { class: "muda-quiet-lead" }, "No projects yet."),
          h2(
            "p",
            null,
            "A project is a set of repositories you pick. Start one with New project, or ask a session to open one with muda."
          )
        )
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
          rows: inGroup.map(rowOf)
        }
      ];
    });
    columns.centre.replaceChildren(
      groupList({
        groups,
        folded: [...folded],
        onFold(key, on) {
          if (on) folded.add(key);
          else folded.delete(key);
          localStorage.setItem("muda.home.folded", JSON.stringify([...folded]));
        }
      })
    );
  }
  let stopped = false;
  async function load() {
    const list = await s.api.get("/projects");
    if (stopped) return;
    rows = list.projects ?? [];
    draw();
  }
  void load();
  return {
    stop: () => {
      stopped = true;
    },
    changed: () => void load()
  };
}

// new.ts
import { h as h3 } from "/_kit/kit.js";
var FOLDER_KEY = "muda.scan.folder";
function field(label, control) {
  return h3(
    "label",
    { class: "muda-field" },
    h3("span", { class: "kit-label" }, label),
    control
  );
}
function newProject(s, into) {
  const main = h3("main", {
    class: "kit-read kit-col kit-mid muda-full",
    "aria-label": into ? "Add repositories" : "New project"
  });
  const heading = h3(
    "h1",
    { class: "muda-title" },
    into ? "Add repositories" : "New project"
  );
  s.root.append(band(heading), main);
  s.bar.setStatus(into ? "" : "New project");
  let home2 = "";
  let found = [];
  let scanned = "";
  const ticked = /* @__PURE__ */ new Map();
  const have = /* @__PURE__ */ new Set();
  const title = h3("input", {
    class: "kit-note",
    name: "title",
    type: "text",
    placeholder: "Family tools",
    "aria-label": "The project name"
  });
  const folder = h3("input", {
    class: "kit-note",
    name: "folder",
    type: "text",
    placeholder: "~/GitHub",
    "aria-label": "A folder you work in"
  });
  folder.value = localStorage.getItem(FOLDER_KEY) ?? "";
  const why = h3("p", { class: "muda-why", role: "alert" });
  why.hidden = true;
  const list = h3("div", { class: "muda-scan" });
  const save = h3(
    "button",
    { class: "kit-btn fill", type: "submit" },
    into ? "Add" : "Create project"
  );
  function say(text) {
    why.textContent = text;
    why.hidden = !text;
  }
  function tickRow(r) {
    const owned = have.has(r.path);
    return h3(
      "li",
      null,
      h3(
        "label",
        { class: "muda-tick", "data-repo": r.path },
        h3("input", {
          type: "checkbox",
          checked: owned || ticked.has(r.path),
          disabled: owned,
          onchange: (e) => {
            if (e.target.checked) ticked.set(r.path, r);
            else ticked.delete(r.path);
            drawList();
          }
        }),
        h3(
          "span",
          { class: "muda-tick-text" },
          h3("b", null, r.name),
          h3("span", { class: "kit-key" }, tilde(r.path, home2))
        ),
        h3(
          "span",
          { class: `kit-pill${r.has_record ? " ok" : ""}` },
          owned ? "in this project" : r.has_record ? "has a record" : "no record yet"
        )
      )
    );
  }
  function drawList() {
    const elsewhere = [...ticked.values()].filter(
      (r) => !found.some((f) => f.path === r.path)
    );
    save.disabled = !ticked.size;
    list.replaceChildren(
      ...[
        elsewhere.length ? h3(
          "section",
          { "data-section": "ticked" },
          h3("div", { class: "kit-label" }, "Ticked in another folder"),
          h3("ul", { class: "muda-ticks" }, elsewhere.map(tickRow))
        ) : null,
        scanned ? h3(
          "section",
          { "data-section": "found" },
          h3(
            "div",
            { class: "kit-label muda-found" },
            `Found in ${tilde(scanned, home2)} · ${found.length}`
          ),
          found.length ? h3("ul", { class: "muda-ticks" }, found.map(tickRow)) : h3(
            "p",
            { class: "kit-empty" },
            "No GitHub repositories within two levels of it."
          )
        ) : null
      ].filter((x) => !!x)
    );
  }
  async function scan() {
    const f = folder.value.trim();
    if (!f) {
      say("Name a folder to look in.");
      return;
    }
    say("");
    try {
      const r = await s.api.get("/scan", { folder: f });
      found = r.repos ?? [];
      scanned = r.folder;
      localStorage.setItem(FOLDER_KEY, f);
    } catch (err) {
      found = [];
      scanned = "";
      say(err.message);
    }
    drawList();
  }
  async function submit(e) {
    e.preventDefault();
    const groups = [...ticked.values()].map((r) => ({
      kind: "repo",
      key: r.path,
      label: r.name,
      added_by: ""
    }));
    say("");
    save.disabled = true;
    try {
      if (into) {
        const body = { title: "", groups };
        await s.api.post(`/projects/${into}/groups`, body);
        s.go({ page: "project", id: into });
      } else {
        const name = title.value.trim();
        if (!name) {
          say("Give the project a name.");
          save.disabled = false;
          title.focus();
          return;
        }
        const body = { title: name, groups };
        const p = await s.api.post("/projects", body);
        s.go({ page: "project", id: p.id });
      }
    } catch (err) {
      say(err.message);
      save.disabled = false;
    }
  }
  const form = h3(
    "form",
    { class: "kit-card muda-form", onsubmit: (e) => void submit(e) },
    into ? null : field("Name", title),
    h3(
      "div",
      { class: "muda-folder" },
      field(into ? "Another folder" : "A folder you work in", folder),
      h3(
        "button",
        {
          class: "kit-btn",
          type: "button",
          "data-act": "scan",
          onclick: () => void scan()
        },
        "Scan"
      )
    ),
    why,
    list,
    h3(
      "div",
      { class: "kit-btns" },
      save,
      h3(
        "button",
        {
          class: "kit-btn",
          type: "button",
          onclick: () => s.go(into ? { page: "project", id: into } : { page: "home" })
        },
        "Cancel"
      )
    )
  );
  folder.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      void scan();
    }
  });
  main.append(form);
  drawList();
  void (async () => {
    const l = await s.api.get("/projects");
    home2 = l.home;
    if (into) {
      const v = await s.api.get(`/projects/${into}`);
      for (const g of v.groups ?? []) have.add(g.key);
      s.bar.setStatus(v.title);
    }
    if (folder.value) void scan();
    else drawList();
  })();
  return { stop() {
  }, changed() {
  } };
}

// project.ts
import {
  buttons,
  card as kitCard,
  createDrafts,
  fold,
  frame as frame2,
  h as h5,
  indexRow,
  indexSection,
  QUESTIONS,
  questionButton,
  sendBar,
  stageTrack,
  thread,
  waitBadges,
  workingLine as workingLine2
} from "/_kit/kit.js";

// answers.ts
import { h as h4 } from "/_kit/kit.js";
function buttonsKind(a, s) {
  const options = a.options ?? [];
  return h4(
    "div",
    { class: "muda-options-box" },
    h4(
      "div",
      { class: "kit-btns", role: "group" },
      options.map((o) => {
        const on = s.answer?.choice === o.id;
        return h4(
          "button",
          {
            class: `kit-btn${on ? " fill" : ""}`,
            type: "button",
            "data-option": o.id,
            "data-focus": `${s.key}:${o.id}`,
            "aria-pressed": String(on),
            onclick: () => s.set(on ? null : { kind: "buttons", choice: o.id })
          },
          o.label
        );
      })
    ),
    options.filter((o) => o.detail).map(
      (o) => h4("small", { class: "muda-option-detail" }, `${o.label}: ${o.detail}`)
    )
  );
}
function keepButton(a, s, label) {
  const kept = defaultAnswer(a);
  if (s.answer || !kept) return null;
  return h4(
    "button",
    {
      class: "kit-btn",
      type: "button",
      "data-act": "keep",
      onclick: () => s.set(kept)
    },
    label
  );
}
function rowsKind(a, s) {
  const proposed = defaultAnswer(a)?.rows ?? {};
  const now = s.answer?.rows ?? proposed;
  return h4(
    "div",
    { class: "muda-menus" },
    (a.rows ?? []).map((r) => {
      const select = h4(
        "select",
        {
          class: "kit-note",
          "data-row": r.id,
          "data-focus": `${s.key}:${r.id}`,
          "aria-label": r.label,
          onchange: (e) => {
            const rows = { ...now };
            const v = e.target.value;
            if (v) rows[r.id] = v;
            else delete rows[r.id];
            s.set(Object.keys(rows).length ? { kind: "rows", rows } : null);
          }
        },
        h4("option", { value: "" }, "Choose…"),
        (r.options ?? []).map((o) => h4("option", { value: o.id }, o.label))
      );
      select.value = now[r.id] ?? "";
      return h4(
        "label",
        { class: "muda-menu" },
        h4("span", { class: "muda-menu-label" }, r.label),
        select
      );
    }),
    keepButton(a, s, "Keep these")
  );
}
function orderKind(a, s) {
  const ids = orderOf(a, s.answer);
  const labels = new Map((a.rows ?? []).map((r) => [r.id, r.label]));
  const list = h4("ol", { class: "muda-order" });
  const commit = (next) => {
    if (next.join("\n") !== ids.join("\n") || !s.answer)
      s.set({ kind: "order", order: next });
  };
  const current = () => [...list.children].map((li) => li.dataset.row ?? "");
  for (const id of ids) {
    const li = h4(
      "li",
      { class: "muda-order-row", "data-row": id },
      null,
      h4("span", { class: "muda-order-text" }, labels.get(id) ?? id)
    );
    const handle = h4(
      "button",
      {
        class: "muda-grip",
        type: "button",
        "data-focus": `${s.key}:${id}`,
        "aria-label": `Move ${labels.get(id) ?? id}: drag, or the up and down keys`,
        title: "Drag to move"
      },
      "⠿"
    );
    handle.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
      if (!d) return;
      e.preventDefault();
      const next = moved(ids, id, ids.indexOf(id) + d);
      if (next !== ids) commit(next);
    });
    handle.addEventListener("pointerdown", (e) => {
      e.preventDefault();
      handle.setPointerCapture(e.pointerId);
      li.classList.add("dragging");
      const onMove = (m) => {
        const mid = (el) => {
          const r = el.getBoundingClientRect();
          return r.top + r.height / 2;
        };
        for (; ; ) {
          const up = li.previousElementSibling;
          const down = li.nextElementSibling;
          if (up && m.clientY < mid(up)) li.after(up);
          else if (down && m.clientY > mid(down)) li.before(down);
          else return;
        }
      };
      const onEnd = () => {
        handle.removeEventListener("pointermove", onMove);
        handle.removeEventListener("pointerup", onEnd);
        handle.removeEventListener("pointercancel", onEnd);
        li.classList.remove("dragging");
        const next = current();
        if (next.join("\n") !== ids.join("\n")) commit(next);
      };
      handle.addEventListener("pointermove", onMove);
      handle.addEventListener("pointerup", onEnd);
      handle.addEventListener("pointercancel", onEnd);
    });
    li.prepend(handle);
    list.append(li);
  }
  return h4(
    "div",
    { class: "muda-order-box" },
    list,
    keepButton(a, s, "Keep this order")
  );
}
function fieldsKind(a, s) {
  const values = {};
  for (const f of a.fields ?? [])
    values[f.id] = s.answer?.fields?.[f.id] ?? f.value ?? "";
  const held = () => Object.values(values).some((v) => v.trim()) ? { kind: "fields", fields: { ...values } } : null;
  return h4(
    "div",
    { class: "muda-fields" },
    (a.fields ?? []).map((f) => {
      const control = h4(f.type === "long" ? "textarea" : "input", {
        class: "kit-note",
        "data-field": f.id,
        "data-focus": `${s.key}:${f.id}`,
        ...f.type === "long" ? { rows: 3 } : { type: f.type === "date" ? "date" : "text" }
      });
      control.value = values[f.id];
      control.addEventListener("input", () => {
        values[f.id] = control.value;
        s.set(held(), true);
      });
      control.addEventListener("change", () => {
        values[f.id] = control.value;
        s.set(held());
      });
      return h4(
        "label",
        { class: "muda-field" },
        h4("span", { class: "kit-label" }, f.label),
        control
      );
    })
  );
}
function answerControls(a, s) {
  switch (a.kind) {
    case "buttons":
      return buttonsKind(a, s);
    case "rows":
      return rowsKind(a, s);
    case "order":
      return orderKind(a, s);
    default:
      return fieldsKind(a, s);
  }
}

// project.ts
var WORDS = Object.values(QUESTIONS);
var SAVE_AFTER_MS = 500;
function put(el, ...nodes) {
  const a = document.activeElement;
  const name = a?.dataset?.focus ?? a?.dataset?.draft;
  const had = !!a && !!name && el.contains(a);
  let start2 = null;
  if (had) {
    try {
      start2 = a.selectionStart;
    } catch {
      start2 = null;
    }
  }
  el.replaceChildren(...nodes);
  if (!had) return;
  const next = el.querySelector(
    `[data-focus="${CSS.escape(name)}"],[data-draft="${CSS.escape(name)}"]`
  );
  if (!next) return;
  next.focus();
  if (start2 !== null) {
    try {
      next.setSelectionRange(start2, start2);
    } catch {
    }
  }
}
function project(s, first) {
  const id = first.id;
  const base = `/projects/${id}`;
  let route2 = first;
  let view = null;
  let stopped = false;
  let focus = null;
  const columns = frame2({
    name: "muda",
    left: { panel: "index", label: "Cards", folded: phone() },
    centre: { label: "Cards" },
    right: { panel: "thread", label: "Thread" }
  });
  const bandBox = h5("div", { class: "muda-band-box" });
  const centre = h5("div", { class: "muda-centre" });
  const foot = h5("div", { class: "muda-foot" });
  columns.centre.append(centre, foot);
  s.root.append(bandBox, columns.left, columns.centre, columns.right);
  s.bar.setPrimary({
    label: "Add repositories",
    run: () => s.go({ page: "project", id, add: true })
  });
  const drafts = createDrafts();
  const noteKey = (thread2) => `note:${thread2}`;
  let server = sortDrafts([], WORDS);
  let chain = Promise.resolve();
  let busy = 0;
  let edits = 0;
  let timer = null;
  const pickOf = (thread2) => {
    const raw = drafts.picks.get(String(thread2));
    return raw ? JSON.parse(raw) : null;
  };
  function enqueue(fn) {
    edits++;
    busy++;
    chain = chain.then(fn).catch((err) => s.bar.setStatus(err.message, { tone: "danger" })).finally(() => {
      busy--;
      if (!busy && !timer) void load();
    });
    return chain;
  }
  async function savePick(c) {
    const raw = drafts.picks.get(String(c.thread));
    const old = server.picks.get(c.thread);
    if (old && raw === JSON.stringify(old.answer)) return;
    if (old) {
      await s.api.del(`${base}/drafts/${old.id}`);
      server.picks.delete(c.thread);
    }
    if (!raw || !c.answers) return;
    const answer = JSON.parse(raw);
    const body = {
      thread: c.thread,
      text: answerText(c.answers, answer),
      answer
    };
    server.picks.set(
      c.thread,
      await s.api.post(`${base}/drafts`, body)
    );
  }
  async function saveNotes() {
    for (const [key, text] of drafts.notes("note:")) {
      const t = Number(key);
      const words = text.trim();
      const old = server.notes.get(t);
      if (old && !words) {
        await s.api.del(`${base}/drafts/${old.id}`);
        server.notes.delete(t);
      } else if (old && old.text !== words) {
        const body = { thread: t, text: words, answer: null };
        await s.api.put(`${base}/drafts/${old.id}`, body);
        server.notes.set(t, { ...old, text: words });
      } else if (!old && words) {
        const body = { thread: t, text: words, answer: null };
        server.notes.set(
          t,
          await s.api.post(`${base}/drafts`, body)
        );
      }
    }
  }
  const typedPicks = /* @__PURE__ */ new Map();
  function saveSoon() {
    edits++;
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      void flush();
    }, SAVE_AFTER_MS);
  }
  function flush() {
    if (timer) clearTimeout(timer);
    timer = null;
    const cards = [...typedPicks.values()];
    typedPicks.clear();
    return enqueue(async () => {
      for (const c of cards) await savePick(c);
      await saveNotes();
    });
  }
  function setPick(c, a, typing = false) {
    focus = c;
    const key = String(c.thread);
    if (a) drafts.picks.set(key, JSON.stringify(a));
    else drafts.picks.delete(key);
    if (typing) {
      typedPicks.set(c.thread, c);
      saveSoon();
      drawFoot();
      return;
    }
    typedPicks.delete(c.thread);
    void enqueue(() => savePick(c));
    draw();
  }
  function ask(c, words) {
    focus = c;
    void enqueue(async () => {
      const body = { thread: c.thread, text: words, answer: null };
      const d = await s.api.post(`${base}/drafts`, body);
      server.questions.set(c.thread, [
        ...server.questions.get(c.thread) ?? [],
        d
      ]);
      draw();
    });
  }
  function sends() {
    return sendCount({
      picks: drafts.picks.size,
      questions: [...server.questions.values()].flat().length,
      notes: [...drafts.notes("note:").values()]
    });
  }
  function send() {
    void flush();
    void enqueue(async () => {
      const r = await s.api.post(`${base}/send`);
      drafts.discard("note:");
      server = sortDrafts([], WORDS);
      s.bar.setStatus(view?.title ?? "");
      flashSent(r.sent);
    });
  }
  function flashSent(n) {
    const words = `${n} sent to the agent.`;
    s.bar.setStatus(words, { tone: "signal" });
    setTimeout(() => {
      if (!stopped) s.bar.setStatus(view?.title ?? "");
    }, 4e3);
  }
  function discard() {
    typedPicks.clear();
    if (timer) clearTimeout(timer);
    timer = null;
    drafts.discard("note:");
    server = sortDrafts([], WORDS);
    void enqueue(() => s.api.del(`${base}/drafts`));
    draw();
  }
  columns.centre.addEventListener("input", (e) => {
    if (!e.target.matches(".kit-draft")) return;
    saveSoon();
    drawFoot();
  });
  let pressing = false;
  let pending = false;
  const stopping = new AbortController();
  const { signal } = stopping;
  document.addEventListener(
    "pointerdown",
    () => {
      pressing = true;
    },
    { signal }
  );
  for (const ev of ["pointerup", "pointercancel"])
    document.addEventListener(
      ev,
      () => setTimeout(() => {
        pressing = false;
        if (pending) draw();
      }, 30),
      { signal }
    );
  function jobId(v) {
    return route2.job ?? v.jobs?.[0]?.id ?? "";
  }
  const groupLabel = (v, key) => v.groups?.find((g) => g.key === key)?.label || key;
  function open(next) {
    s.go(next);
    if (phone()) {
      const toggle = columns.left.querySelector(".kit-col-toggle");
      if (!columns.left.classList.contains("kit-collapsed")) toggle?.click();
    }
  }
  function drawBand(v) {
    const job = jobId(v);
    const stages = v.jobs?.find((j) => j.id === job)?.stages ?? [];
    const values = stages.map((st) => ({
      key: st.id,
      label: st.label,
      count: st.count,
      state: st.state || "",
      live: st.state === "now" && !!v.working,
      opens: st.id
    }));
    const track = stages.length ? stageTrack({
      stages: values,
      label: "Stages",
      open: (st) => open({ page: "project", id, job, stage: st }),
      after: waitBadges(waitsOf(v))
    }) : h5("div", { class: "kit-stages" }, waitBadges(waitsOf(v)));
    if (route2.stage)
      track.querySelector(`[data-stage="${CSS.escape(route2.stage)}"]`)?.classList.add("muda-stage-open");
    const who = v.owner ? `${v.owner_label || v.owner}${v.owner_present ? "" : " · away"}` : "no session yet";
    const needs = (j) => cardsOfJob(v, j).filter((c) => c.needs_you).length;
    put(
      bandBox,
      band(
        h5(
          "div",
          { class: "muda-band-line" },
          h5("h1", { class: "muda-title muda-title-narrow" }, v.title),
          h5(
            "nav",
            { class: "muda-tabs", "aria-label": "Jobs" },
            (v.jobs ?? []).map((j) => {
              const n = needs(j.id);
              return h5(
                "a",
                {
                  class: `kit-chip${j.id === job ? " on" : ""}`,
                  href: hashOf({ page: "project", id, job: j.id }),
                  "data-job": j.id,
                  "aria-current": j.id === job ? "page" : null
                },
                j.label,
                n ? h5("span", { class: "kit-n" }, String(n)) : null
              );
            })
          ),
          h5(
            "div",
            { class: "kit-agent-slot" },
            workingLine2({ text: v.working || null })
          ),
          h5("span", { class: "muda-owner", "data-owner": v.owner }, who)
        ),
        track
      )
    );
  }
  function drawLeft(v) {
    const job = jobId(v);
    const on = route2.card;
    const rows = (g) => cardsOfJob(v, job).filter((c) => c.group === g.key).map(
      (c) => indexRow({
        id: `${c.group}/${c.id}`,
        title: c.title,
        meta: `${v.jobs?.find((j) => j.id === job)?.stages?.find((x) => x.id === c.stage)?.label ?? c.stage} · ${c.kind}`,
        on: on?.group === c.group && on.id === c.id,
        attention: c.needs_you,
        draft: drafts.picks.has(String(c.thread)) || !!drafts.get(noteKey(c.thread)).trim(),
        select: () => open({
          page: "project",
          id,
          job,
          card: { group: c.group, id: c.id }
        })
      })
    );
    put(
      columns.leftBody,
      h5(
        "div",
        { class: "muda-left" },
        indexSection({
          name: "Project",
          empty: "",
          rows: [
            indexRow({
              id: "overview",
              title: "Each group’s numbers",
              on: !route2.card && !route2.stage,
              select: () => open({ page: "project", id, job: job || void 0 })
            })
          ]
        }),
        jobGroups(v, job).map(
          (g) => indexSection({
            name: g.label || g.key,
            rows: rows(g),
            empty: "Nothing here yet."
          })
        )
      )
    );
  }
  function noteBox(thread2, label) {
    return drafts.box(noteKey(thread2), label, 2).el;
  }
  function evidenceList(c) {
    return h5(
      "ul",
      { class: "muda-evidence" },
      (c.evidence ?? []).map(
        (e) => h5(
          "li",
          { "data-evidence": e.ref },
          h5(
            "div",
            { class: "muda-evidence-head" },
            e.url ? h5(
              "a",
              { href: e.url, target: "_blank", rel: "noopener noreferrer" },
              e.ref
            ) : h5("span", null, e.ref),
            h5("span", { class: "kit-meta" }, e.kind.replace("_", " "))
          ),
          e.text ? h5("p", null, e.text) : null
        )
      )
    );
  }
  function cardEl(v, c, inPlace) {
    const answer = pickOf(c.thread);
    const held = !!answer || !!drafts.get(noteKey(c.thread)).trim();
    const pill = (text, cls = "") => h5("span", { class: `kit-pill ${cls}` }, text);
    const sendable = questionsOf(
      threadOf(v, c.thread),
      server.questions.get(c.thread) ?? [],
      WORDS
    );
    const job = c.job;
    const openCard = () => open({
      page: "project",
      id,
      job,
      card: { group: c.group, id: c.id }
    });
    const body = h5(
      "div",
      { class: "muda-card-body" },
      inPlace ? h5(
        "button",
        {
          class: "muda-card-title muda-linkish",
          type: "button",
          onclick: openCard
        },
        c.title
      ) : h5("h2", { class: "muda-card-title" }, c.title),
      c.revision ? h5("p", { class: "muda-revision" }, c.revision) : null,
      c.decided ? h5(
        "section",
        { "data-part": "decided" },
        h5("div", { class: "kit-label" }, "What you already decided"),
        h5("p", { class: "muda-decided" }, `✓ ${c.decided}`)
      ) : null,
      c.evidence?.length ? inPlace ? fold(`Evidence · ${c.evidence.length}`, evidenceList(c)) : h5(
        "section",
        { "data-part": "evidence" },
        h5(
          "div",
          { class: "kit-label" },
          `Evidence · ${c.evidence.length}`
        ),
        evidenceList(c)
      ) : null,
      c.recommendation ? h5(
        "section",
        { "data-part": "recommendation" },
        h5("div", { class: "kit-label" }, "The agent recommends"),
        h5("p", null, c.recommendation)
      ) : null,
      c.answers ? h5(
        "section",
        { "data-part": "answers", "data-kind": c.answers.kind },
        h5("div", { class: "kit-label" }, "Your answer"),
        answerControls(c.answers, {
          answer,
          key: `answer:${c.thread}`,
          set: (a, typing) => setPick(c, a, typing)
        })
      ) : null,
      h5(
        "div",
        { class: "muda-asks" },
        buttons(
          WORDS.map((w) => {
            if (sendable.drafted.includes(w))
              return { label: `✓ In your drafts: ${w}`, disabled: true };
            return questionButton({
              label: w,
              asked: sendable.sent.includes(w),
              run: () => ask(c, w)
            });
          })
        )
      ),
      noteBox(
        c.thread,
        "A note: it goes with your answer, or on its own, on Send."
      )
    );
    body.addEventListener("focusin", () => {
      if (focus?.thread !== c.thread && inPlace) {
        focus = c;
        drawRight(v);
      }
    });
    const el = kitCard({
      edge: held ? "wait" : c.needs_you ? "signal" : void 0,
      // kit.d.ts says string; dom.js takes a node too (a gap in the types).
      head: h5(
        "span",
        { class: "muda-card-head" },
        h5(
          "span",
          { class: "kit-card-crumb" },
          `${c.kind} · ${groupLabel(v, c.group)} · ${c.id}`
        ),
        c.class ? pill(c.class) : null,
        c.basis ? pill(c.basis, c.basis === "measured" ? "ok" : "wait") : null,
        answer ? pill("draft", "wait") : c.needs_you ? pill("needs you", "muda-you") : null
      ),
      body
    });
    el.dataset.card = `${c.group}/${c.id}`;
    return el;
  }
  function overview(v, job) {
    if (!v.jobs?.length)
      return h5(
        "div",
        { class: "muda-quiet" },
        h5("p", { class: "muda-quiet-lead" }, "Nothing posted yet."),
        h5(
          "p",
          null,
          "Ask a session in one of these repositories to open this project with muda: its findings and plans show here, for you to answer."
        ),
        h5(
          "ul",
          { class: "muda-plain" },
          (v.groups ?? []).map(
            (g) => h5("li", { class: "kit-key" }, g.label || g.key)
          )
        )
      );
    return h5(
      "div",
      { class: "muda-stack" },
      h5("div", { class: "kit-label" }, "Each group’s own numbers"),
      h5(
        "div",
        { class: "muda-standings" },
        groupStandings(v, job).map(
          (g) => h5(
            "section",
            { class: "kit-card muda-standing", "data-group": g.group.key },
            h5("h2", { class: "muda-card-title" }, g.group.label || g.group.key),
            h5(
              "p",
              { class: "kit-meta" },
              g.needsYou ? `${g.needsYou} wait on you` : "Nothing waits on you"
            ),
            h5(
              "ul",
              { class: "muda-plain muda-counts" },
              g.stages.map(
                (st) => h5(
                  "li",
                  null,
                  h5(
                    "a",
                    {
                      href: hashOf({ page: "project", id, job, stage: st.id })
                    },
                    st.label
                  ),
                  h5("span", { class: "kit-meta" }, String(st.count))
                )
              )
            ),
            g.baselines.map(
              (c) => h5(
                "div",
                { class: "muda-baseline" },
                h5(
                  "a",
                  {
                    href: hashOf({
                      page: "project",
                      id,
                      job,
                      card: { group: c.group, id: c.id }
                    })
                  },
                  c.title
                ),
                evidenceList(c)
              )
            )
          )
        )
      ),
      h5("div", { class: "kit-label" }, "A note on the whole project"),
      noteBox(v.thread, "A note on no one card: it goes on Send.")
    );
  }
  function centreOf(v) {
    const job = jobId(v);
    const c = route2.card ? v.cards?.find(
      (x) => x.job === job && x.group === route2.card?.group && x.id === route2.card?.id
    ) : null;
    if (c) return cardEl(v, c, false);
    if (route2.stage) {
      const stage = v.jobs?.find((j) => j.id === job)?.stages?.find((x) => x.id === route2.stage);
      const groups = stageCards(v, job, route2.stage);
      return h5(
        "div",
        { class: "muda-stack", "data-stage-page": route2.stage },
        h5("h2", { class: "muda-page-title" }, stage?.label ?? route2.stage),
        groups.length ? groups.map(
          ([g, cards]) => h5(
            "section",
            { class: "muda-stage-group", "data-group": g.key },
            h5("div", { class: "kit-label" }, g.label || g.key),
            cards.map((x) => cardEl(v, x, true))
          )
        ) : h5("p", { class: "kit-empty" }, "Nothing at this stage yet.")
      );
    }
    return overview(v, job);
  }
  function threadCard(v) {
    const job = jobId(v);
    if (route2.card)
      return v.cards?.find(
        (x) => x.job === job && x.group === route2.card?.group && x.id === route2.card?.id
      ) ?? null;
    if (!route2.stage) return null;
    const at = stageCards(v, job, route2.stage).flatMap(([, cs]) => cs);
    const f = focus && at.find((x) => x.thread === focus?.thread);
    return f ?? at.find((x) => x.needs_you) ?? at[0] ?? null;
  }
  function drawRight(v) {
    const c = threadCard(v);
    const threadId = c?.thread ?? v.thread;
    const messages = threadOf(v, threadId);
    put(
      columns.rightBody,
      thread({
        id: String(threadId),
        name: c?.title ?? "The whole project",
        key: c ? `${groupLabel(v, c.group)} · ${c.id}` : null,
        link: c ? hashOf({
          page: "project",
          id,
          job: c.job,
          card: { group: c.group, id: c.id }
        }).slice(1) : void 0,
        messages: messages.map((m) => ({
          id: m.id,
          from: m.who === "you" ? "court" : "agent",
          at: m.when,
          words: m.who === "you" ? "" : m.who,
          state: m.who === "you" ? m.state : void 0,
          stateLine: m.line,
          text: m.text
        }))
      })
    );
    columns.setRightEmpty(!messages.length, `${id}/${threadId}`);
  }
  function drawFoot() {
    if (!view) return;
    const n = sends();
    const job = jobId(view);
    const at = route2.stage ? stageCards(view, job, route2.stage).flatMap(([, cs]) => cs) : [];
    const asked = at.filter((c) => c.answers);
    const done = asked.filter(
      (c) => !c.needs_you || drafts.picks.has(String(c.thread))
    ).length;
    foot.classList.toggle("held", n > 0);
    if (!n && !route2.stage) {
      foot.replaceChildren();
      return;
    }
    put(
      foot,
      sendBar({
        count: asked.length ? `${done} of ${asked.length} answered` : n === 1 ? "1 draft" : `${n} drafts`,
        sends: n,
        hint: view.owner_present ? `Goes to ${view.owner_label || "the session"} together, on Send.` : "Goes together, on Send; it waits for a session to open this project.",
        discard,
        send
      })
    );
  }
  function draw() {
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
  function adopt(v) {
    server = sortDrafts(v.drafts ?? [], WORDS);
    drafts.picks.clear();
    for (const [t, d] of server.picks)
      drafts.picks.set(String(t), JSON.stringify(d.answer));
    const seen = /* @__PURE__ */ new Set();
    for (const [t, d] of server.notes) {
      seen.add(String(t));
      if (drafts.get(noteKey(t)) !== d.text) drafts.restore(noteKey(t), d.text);
    }
    for (const [t, text] of drafts.notes("note:"))
      if (!seen.has(t) && text.trim()) drafts.restore(`note:${t}`, "");
  }
  async function load() {
    const at = edits;
    const v = await s.api.get(base);
    if (stopped) return;
    view = v;
    s.bar.setStatus(v.title);
    if (at === edits && !busy && !timer) adopt(v);
    draw();
  }
  void load();
  return {
    stop() {
      stopped = true;
      stopping.abort();
      if (drafts.dirty()) void flush();
    },
    changed(p) {
      if (p === id || p === 0) void load();
    },
    go(r) {
      route2 = r;
      focus = null;
      draw();
      columns.centre.scrollTo({ top: 0 });
    }
  };
}

// entry.ts
var theBar = bar({
  brand: { name: "muda", href: "#/" },
  staleText: "restarted · reload this tab"
});
initTheme("muda", theBar.themeControl);
theBar.setLive("polling");
var root = document.getElementById("app");
var api = createApi({
  onStale: () => theBar.setLive("stale"),
  // A new serve answered: what this tab runs is out of date, and its drafts are
  // on the server.
  onNewBuild: () => location.reload()
});
var route = parseHash(location.hash);
var refreshList = keepElsewhere(theBar, async () => {
  const list = await api.get("/projects");
  const here = route.page === "project" ? route.id : null;
  return elsewhereRows(list.projects ?? [], here).map((r) => ({
    label: r.label,
    detail: r.detail,
    tone: r.tone,
    current: r.current,
    onClick: () => go({ page: "project", id: r.id })
  }));
});
function go(r) {
  location.hash = hashOf(r);
}
var shell = { root, bar: theBar, api, go };
var page = null;
var pageId = "";
function idOf(r) {
  if (r.page !== "project") return r.page;
  return r.add ? `add:${r.id}` : `p:${r.id}`;
}
function start(r) {
  page?.stop();
  theBar.setPrimary(null);
  theBar.setStatus("");
  root.replaceChildren(theBar.el);
  pageId = idOf(r);
  if (r.page === "home") page = home(shell);
  else if (r.page === "new") page = newProject(shell, null);
  else if (r.add) page = newProject(shell, r.id);
  else page = project(shell, r);
}
var tabId = crypto.randomUUID();
var feed = null;
var feedPage = null;
function listen(r) {
  const key = pageKey(r);
  if (key === feedPage) return;
  const cursor = feed?.cursor();
  feed?.stop();
  feedPage = key;
  feed = live({
    events: "/api/events",
    poll: "/api/poll",
    page: key,
    cursor,
    randomUUID: () => tabId,
    onEvent(e) {
      if (e.type !== "project") return;
      void refreshList();
      page?.changed(e.data.id);
    },
    onStatus: (s) => theBar.setLive(s),
    onNewBuild: () => theBar.setStatus(
      "A newer muda is installed; it takes over at the next channel call.",
      { tone: "signal" }
    ),
    onShow: (p) => go(p ? { page: "project", id: Number(p) } : { page: "home" })
  });
}
addEventListener("hashchange", () => {
  route = parseHash(location.hash);
  listen(route);
  void refreshList();
  if (idOf(route) === pageId) page?.go?.(route);
  else start(route);
});
addEventListener("unhandledrejection", (e) => {
  if (e.reason instanceof ApiError2) {
    e.preventDefault();
    if (!e.reason.isStale)
      theBar.setStatus(e.reason.message, { tone: "danger" });
  }
});
start(route);
listen(route);
void refreshList();
