// answers.ts: a card's answer, in the four kinds it can take: buttons, a menu per
// row, rows to drag into order, and fields. The rows and words are the agent's.
// Every change goes through set, which keeps it as a draft until Send.

import { h } from '/_kit/kit.js';
import { defaultAnswer, moved, orderOf } from './model.ts';
import type { Answer, Answers } from './wire.d.ts';

export interface AnswerSink {
  /** What is held for this card, null for nothing. */
  answer: Answer | null;
  /** Hold a new answer. typing: more keys are coming, so don't redraw. */
  set(a: Answer | null, typing?: boolean): void;
  /** Makes the focus keys of this card's controls unique on the page. */
  key: string;
}

function buttonsKind(a: Answers, s: AnswerSink): HTMLElement {
  const options = a.options ?? [];
  return h(
    'div',
    { class: 'muda-options-box' },
    h(
      'div',
      { class: 'kit-btns', role: 'group' },
      options.map((o) => {
        const on = s.answer?.choice === o.id;
        return h(
          'button',
          {
            class: `kit-btn${on ? ' fill' : ''}`,
            type: 'button',
            'data-option': o.id,
            'data-focus': `${s.key}:${o.id}`,
            'aria-pressed': String(on),
            onclick: () => s.set(on ? null : { kind: 'buttons', choice: o.id }),
          },
          o.label,
        );
      }),
    ),
    options
      .filter((o) => o.detail)
      .map((o) =>
        h('small', { class: 'muda-option-detail' }, `${o.label}: ${o.detail}`),
      ),
  );
}

function keepButton(
  a: Answers,
  s: AnswerSink,
  label: string,
): HTMLElement | null {
  const kept = defaultAnswer(a);
  if (s.answer || !kept) return null;
  return h(
    'button',
    {
      class: 'kit-btn',
      type: 'button',
      'data-act': 'keep',
      onclick: () => s.set(kept),
    },
    label,
  );
}

function rowsKind(a: Answers, s: AnswerSink): HTMLElement {
  const proposed = defaultAnswer(a)?.rows ?? {};
  const now = s.answer?.rows ?? proposed;
  return h(
    'div',
    { class: 'muda-menus' },
    (a.rows ?? []).map((r) => {
      const select = h(
        'select',
        {
          class: 'kit-note',
          'data-row': r.id,
          'data-focus': `${s.key}:${r.id}`,
          'aria-label': r.label,
          onchange: (e: Event) => {
            const rows = { ...now };
            const v = (e.target as HTMLSelectElement).value;
            if (v) rows[r.id] = v;
            else delete rows[r.id];
            s.set(Object.keys(rows).length ? { kind: 'rows', rows } : null);
          },
        },
        h('option', { value: '' }, 'Choose…'),
        (r.options ?? []).map((o) => h('option', { value: o.id }, o.label)),
      ) as HTMLSelectElement;
      select.value = now[r.id] ?? '';
      return h(
        'label',
        { class: 'muda-menu' },
        h('span', { class: 'muda-menu-label' }, r.label),
        select,
      );
    }),
    keepButton(a, s, 'Keep these'),
  );
}

// Drag to order: a gap in the kit. A second tool would want this; move it there
// once one does. Pointer events, so a finger drags too; the arrow keys on a
// handle do the same without one.
function orderKind(a: Answers, s: AnswerSink): HTMLElement {
  const ids = orderOf(a, s.answer);
  const labels = new Map((a.rows ?? []).map((r) => [r.id, r.label]));
  const list = h('ol', { class: 'muda-order' });
  const commit = (next: string[]) => {
    if (next.join('\n') !== ids.join('\n') || !s.answer)
      s.set({ kind: 'order', order: next });
  };
  const current = () =>
    [...list.children].map((li) => (li as HTMLElement).dataset.row ?? '');
  for (const id of ids) {
    const li = h(
      'li',
      { class: 'muda-order-row', 'data-row': id },
      null,
      h('span', { class: 'muda-order-text' }, labels.get(id) ?? id),
    );
    const handle = h(
      'button',
      {
        class: 'muda-grip',
        type: 'button',
        'data-focus': `${s.key}:${id}`,
        'aria-label': `Move ${labels.get(id) ?? id}: drag, or the up and down keys`,
        title: 'Drag to move',
      },
      '⠿',
    );
    handle.addEventListener('keydown', (e) => {
      const d = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
      if (!d) return;
      e.preventDefault();
      const next = moved(ids, id, ids.indexOf(id) + d);
      if (next !== ids) commit(next);
    });
    handle.addEventListener('pointerdown', (e) => {
      e.preventDefault();
      handle.setPointerCapture(e.pointerId);
      li.classList.add('dragging');
      // Its neighbours move past it, never the row itself: taking the row out of
      // the page would end the pointer's capture.
      const onMove = (m: PointerEvent) => {
        const mid = (el: Element) => {
          const r = el.getBoundingClientRect();
          return r.top + r.height / 2;
        };
        for (;;) {
          const up = li.previousElementSibling;
          const down = li.nextElementSibling;
          if (up && m.clientY < mid(up)) li.after(up);
          else if (down && m.clientY > mid(down)) li.before(down);
          else return;
        }
      };
      const onEnd = () => {
        handle.removeEventListener('pointermove', onMove);
        handle.removeEventListener('pointerup', onEnd);
        handle.removeEventListener('pointercancel', onEnd);
        li.classList.remove('dragging');
        const next = current();
        if (next.join('\n') !== ids.join('\n')) commit(next);
      };
      handle.addEventListener('pointermove', onMove);
      handle.addEventListener('pointerup', onEnd);
      handle.addEventListener('pointercancel', onEnd);
    });
    li.prepend(handle);
    list.append(li);
  }
  return h(
    'div',
    { class: 'muda-order-box' },
    list,
    keepButton(a, s, 'Keep this order'),
  );
}

function fieldsKind(a: Answers, s: AnswerSink): HTMLElement {
  const values: Record<string, string> = {};
  for (const f of a.fields ?? [])
    values[f.id] = s.answer?.fields?.[f.id] ?? f.value ?? '';
  const held = (): Answer | null =>
    Object.values(values).some((v) => v.trim())
      ? { kind: 'fields', fields: { ...values } }
      : null;
  return h(
    'div',
    { class: 'muda-fields' },
    (a.fields ?? []).map((f) => {
      const control = h(f.type === 'long' ? 'textarea' : 'input', {
        class: 'kit-note',
        'data-field': f.id,
        'data-focus': `${s.key}:${f.id}`,
        ...(f.type === 'long'
          ? { rows: 3 }
          : { type: f.type === 'date' ? 'date' : 'text' }),
      }) as HTMLInputElement;
      control.value = values[f.id];
      control.addEventListener('input', () => {
        values[f.id] = control.value;
        s.set(held(), true);
      });
      control.addEventListener('change', () => {
        values[f.id] = control.value;
        s.set(held());
      });
      return h(
        'label',
        { class: 'muda-field' },
        h('span', { class: 'kit-label' }, f.label),
        control,
      );
    }),
  );
}

/** The controls for a card's answers. */
export function answerControls(a: Answers, s: AnswerSink): HTMLElement {
  switch (a.kind) {
    case 'buttons':
      return buttonsKind(a, s);
    case 'rows':
      return rowsKind(a, s);
    case 'order':
      return orderKind(a, s);
    default:
      return fieldsKind(a, s);
  }
}
