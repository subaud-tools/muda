// The linter for internal/web: the family base (build/eslint.base.mjs, copied
// from the tools-common version go.mod pins, by `npm run page-build`).

import base from './build/eslint.base.mjs';

export default base({ root: import.meta.dirname, ignores: ['kit.d.ts'] });
