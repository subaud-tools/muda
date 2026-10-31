// shell.ts: what every page gets from the router, and small shared parts.

import { h, type Api, type BarHandle } from '/_kit/kit.js';
import type { Route } from './model.ts';

export interface Shell {
  root: HTMLElement;
  bar: BarHandle;
  api: Api;
  go(r: Route): void;
}

export interface Page {
  stop(): void;
  /** A project changed; id 0: read the open one again. */
  changed(id: number): void;
  /** The address moved inside this page. */
  go?(r: Route): void;
}

/** A short line under the top bar: the page's name and what follows it. */
export function band(...children: (HTMLElement | null)[]): HTMLElement {
  return h('header', { class: 'muda-band' }, children);
}

export const phone = () => matchMedia('(width <= 640px)').matches;
