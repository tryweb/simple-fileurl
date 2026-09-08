# DESIGN.md — SFTP Admin UI Design System

Extracted from the existing server-rendered Admin UI (`internal/sftpadmin/ui.go`).
This is the visual contract for all Admin UI work. No new JS framework, no
separate CSS build, no frontend dependency. All styling is embedded in the
`baseTemplate` `<style>` block; all behavior is plain server-rendered forms.

## 0. Research Log

- Read `internal/sftpadmin/ui.go` (baseTemplate CSS, login/users/created/settings
  templates, `userDashboard` view models) as the single source of truth.
- Read `openspec/changes/manage-sftp-admin-keys/{proposal,design,specs}` for the
  key-lifecycle contract (generate-primary, per-key remove, manifest-wide repair).
- Decision: codify what exists (light neutral palette, dark nav, cards, inline
  forms, system font, no-JS) rather than invent a new visual language.

## 1. Atmosphere

Quiet operations console. Light neutral workspace on a dark top nav; content is
a single centered column of white cards. No marketing surface, no hero, no
illustration. Density is form-first: labels, hints, inputs, and inline row
actions. The UI must read as trustworthy infrastructure tooling: explicit
fingerprints, explicit danger states, explicit one-time-download warnings.

## 2. Color tokens

All color decisions MUST reference these tokens (CSS custom properties in
`baseTemplate`). No hardcoded hex/rgb outside the `:root` block.

| Token | Value | Use |
|---|---|---|
| `--bg` | `#f4f5f7` | Page background |
| `--card` | `#fff` | Card surface |
| `--ink` | `#1c2430` | Primary text |
| `--muted` | `#5b6572` | Hints, secondary text (`.hint`) |
| `--line` | `#dfe3e8` | Borders, input borders, row dividers |
| `--accent` | `#1f6feb` | Primary buttons/links (`.btn`, `button`) |
| `--accent-ink` | `#fff` | Text on accent |
| `--danger` | `#b42318` | Error text, alert regions (`.alert`) |
| `--ok` | `#067647` | Success text, notice regions (`.notice`) |
| `--nav` | `#24292f` | Authenticated top navigation surface |
| `--nav-muted` | `#57606a` | Active navigation and secondary controls |
| `--alert-bg` | `#fef3f2` | Error and warning surface |
| `--alert-line` | `#fecdca` | Error and warning border |
| `--notice-bg` | `#ecfdf3` | Success surface |
| `--notice-line` | `#a6f4c5` | Success border |
| `--row-alt` | `#f9fafb` | Alternating table rows |
| `--scrim` | `rgba(28,36,48,.55)` | Translucent overlay scrim behind the focused feedback dialog (`.overlay` only) |
| `--overlay-top` | `10vh` | Upper-viewport offset for the horizontally centered feedback dialog |
| `--radius` | `8px` | Card/alert/notice corner radius |
| `--pad` | `16px` | Card padding, nav padding unit |

All colors are tokenized in `:root`; component rules must reference the tokens
above rather than repeating raw color values.

## 3. Typography

- Font stack (single rule, no webfont): `16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif`.
- Scale: `h1 1.5rem` (page title + created-result title), `h2 1.15rem` (card
  titles), body `1rem`, `.hint .85rem`, `code .85em`.
- Labels are `font-weight:600` block elements; `.hint` inside labels is
  `font-weight:400` muted.
- Key material and fingerprints always render inside `<code>` with
  `word-break:break-all` so long fingerprints/wire text never overflow.
- Key type (e.g. `ssh-ed25519`) renders as small muted text (`.key-type`,
  `.85rem`, `var(--muted)`), never as a heading.
- No custom fonts, no icon fonts, no emojis. Text labels only.
- Operational metadata may pair inline SVG icons with visible values and
  `title`/`aria-label` tips; icons are supplementary and never the only
  accessible label.

## 4. Spacing / layout

- Shell: `main{max-width:1200px;margin:0 auto;padding:24px var(--pad) 48px}`.
  The wider shell gives the key-dense users table room to breathe; cards and
  forms keep their own `max-width` caps so nothing else stretches.
- Cards: `.card{padding:var(--pad);margin:0 0 20px}` separated by 20px vertical rhythm.
- Nav: `padding:10px var(--pad)`, flex with `gap:8px 16px`, wraps on narrow screens.
- Forms: `label{margin:10px 0 4px}`; inputs `padding:8px 10px`, `width:100%`,
  `max-width:560px`; textarea `resize:vertical`.
- Buttons: `padding:8px 16px`; inline row forms `.inline-form{display:inline;margin-right:8px}`.
- Block actions sit on their own row: `.form-actions{margin-top:12px}` wraps the
  Create button below the paste-or-generate textarea, left-aligned.
- Overflow and wrapping contract: every value the operator must read must be
  visible without hovering. Never rely on `text-overflow: ellipsis` for primary
  data. Fixed-layout tables keep a readable minimum width and scroll inside
  `.table-wrap{overflow-x:auto}` when the viewport is too narrow; the page
  itself never scrolls sideways. Long identifiers (fingerprints, hashes, IDs)
  may break only inside their own cell; compact metadata values stay on one
  line by default and only wrap when the viewport forces the table to scroll.
- Tables: `th,td{padding:8px 10px}`, full width, `border-collapse:collapse`,
  bottom borders only. The users table (`.users-table`,
  `table-layout:fixed`) pins scan-first column proportions via classed
  `<colgroup>` — Username 24%, Status 18%, Keys 22%, Manage 36% — and the
  header row keeps those four `<th scope="col">` columns. Each user body row
  is a single `<td colspan="4" class="user-cell">` containing one native
  `<details class="user-details">`; the `<summary class="user-summary">` is a
  CSS grid mirroring the header proportions so the collapsed scan line aligns
  with the header, while the disclosed `.user-maintenance` block below it
  spans the full table content width. Its valid-key list uses an adaptive grid
  and row-level actions use a wrapping `.user-actions` bar, so expanded content
  uses available width instead of stacking everything in one narrow column.
  No inline `style` widths: every proportion lives in CSS classes. Percentage
  widths scale down intact, which is what keeps the 375px path safe.
- Card order on the Users page is Existing users (`#existing-users`) first,
  then Create user (`#create-user`), then Key repair (`#key-repair`); stable
  `id` hooks exist for all three cards and `class="users-table"` for the
  table.
- The Share Links page uses `#existing-links` before `#create-link` and
  satisfies these layout constraints:
  - The ID column displays the full link ID on a single line at desktop width.
  - Scope, Password, Created, and Expires use compact icon+value metadata with
    visible text plus `title`/`aria-label` tips; values stay on one line where
    the viewport permits and never truncate with ellipsis.
  - Long URLs wrap only inside the URL column.
  - Optional descriptions appear below the URL, not inside the Actions column.
  - The table has a readable minimum width; on narrow viewports it scrolls
    horizontally inside `.table-wrap`, while the page itself never scrolls
    sideways and the Create link form remains a single usable column.
- The Create link `datetime-local` field represents the admin browser's local
  time. A hidden browser offset is submitted with the form, the server stores
  the resulting instant in UTC, and Created/Expires display an explicit `UTC`
  suffix plus accessible icon tips. Public `/l/<id>`, password, and files
  routes enforce expiry at request time; direct hash download URLs remain
  permanent bearer URLs by design.
- Responsive: single column below ~600px. Tables scroll horizontally inside
  `.table-wrap{overflow-x:auto}` when their minimum width exceeds the viewport;
  this is the ONLY element allowed to introduce horizontal scrolling. At 375px
  every maintenance control inside `.user-maintenance` stacks full-width and no
  horizontal page overflow is allowed. Below 600px the table header is visually
  compacted, the summary grid reflows to two columns, and the key tile grid
  becomes one column, so Status, Keys, Manage, notes, and buttons remain
  directly usable.

## 5. Reusable primitives and states

Primitives (extend these, never one-off div soup):

- `.topnav` — brand + Users/Settings links + sign-out form. Active link gets
  `.active`. Present on every authenticated page (`ShowNav`).
- `.card` — titled section (`h2` + forms/tables). Users page cards in order:
  Existing users (`#existing-users`), Create user (`#create-user`), Key repair
  (`#key-repair`). Existing users is first and dominant: it is the scanning
  surface, while creation and repair are secondary cards below it.
- `.alert[role=alert]` — blocking errors, invalid-key state, one-time-download
  warning. `.notice` — save confirmations.
- `.hint` — inline muted help inside labels (username pattern, generate-vs-paste
  guidance, repair scope).
- `.inline-form` — row-level actions that sit side by side: Generate new key,
  per-key Save note, per-key Remove, Disable/Enable, and Delete
  (disabled rows only, danger-styled). All of these live inside the per-row
  `.user-maintenance` disclosure (see `.user-details`), never in the visible
  scan row.
- `.btn` — link styled as button (one-time download, generated-card Close via
  `.btn.secondary`).
- Feedback dialog — both generate paths (`POST /users/create` with an empty
  key for a new user, `POST /users/generate-key` for an existing user) return
  `200` rendering the Users dashboard itself with one identical result window
  as the first element under the `h1` (above the Create-user card).
  Authenticated failures from browser-facing Users mutations use the same
  window with the operation title and an `.alert[role=alert]`; their HTTP
  status is preserved for API clients. This includes create, generate,
  enable/disable, delete-user, delete-key, save-note, and repair actions.
  Both states use existing `.card`/`.alert`/`.btn` primitives only. The
  window is a pure-CSS focused overlay: a fixed full-viewport
  `.overlay` div (translucent `var(--scrim)` background, horizontally
  centered, upper-viewport `var(--overlay-top)` offset) wrapping a semantic
  `<dialog class="card overlay-card" open aria-labelledby="generated-title">`
  (capped at `560px`, zero margin). No `showModal()`, no JS focus trap, no
  auto-download, no toasts — the existing single `confirm()` guard stays the
  only script on the page. The success dialog carries `Key generated for
  <user>` as its `h2` (`id="generated-title"`), the new fingerprint in
  `<code>`, the same one-time warning text for both flows
  (`.alert[role=alert]`), a `Download private key (one-time)` `.btn` link
  (with `autofocus`) to `/keys/download?token=[REDACTED:API key param], and a
  `Close` secondary-button anchor to `/` (`class="btn secondary"`). The
  success card renders only when result fields are present; the plain `GET /
  ` dashboard has no overlay and no token. The token travels only in the `200`
  response body, never in the address bar via redirect. The new key also
  appears in that user's row. The old separate create-user result page
  (`createdView`/`createdTemplate`) is removed.
- Feedback errors use `aria-labelledby="feedback-title"`, show the safe server
  message inside `.alert[role=alert]`, carry no download token, and use the
  same autofocus Close anchor to return to `/`. The dialog is a browser-facing
  presentation choice; non-2xx status codes remain available to clients.
  `POST /users/add-key` remains a plain API-compatible response because it has
  no visible dashboard form. Unauthenticated and CSRF-rejected requests also
  keep their existing `401`/`403` responses rather than rendering a dashboard
  that cannot establish the required session context.
- Focus-window semantics: `autofocus` on the Download link is a hint, not a
  guarantee — the attribute is honored inconsistently on anchors without JS,
  so keyboard users may need one Tab stop to reach the window; every control
  inside keeps the standard `:focus-visible` ring and full Tab reachability.
  Known limitation (no-JS tradeoff, accepted): Esc does NOT dismiss the
  dialog because only `showModal()` provides native light-dismiss and that
  requires script; the Close link to `/` is the single supported dismiss
  path and MUST remain a plain anchor (never a button, never script).
- `.status` — icon+tips status indicator: an inline-SVG dot
  (`var(--ok)` green when enabled, `var(--nav-muted)` gray when disabled) plus
  the words `enabled` / `disabled`, a native `title` tooltip stating the meaning
  (disabled = no new logins), and an `aria-label` describing state and meaning.
  Text is never dropped: the dot alone must not carry the state.
- Condensed-hash fingerprint: valid keys render `SHA256:<first8>…<last8>` in
  `<code>` with `title` = full fingerprint. The full canonical key text lives
  inside a collapsed native `<details><summary>Show full key</summary>` per key
  for copy/inspection. Form values (hidden fingerprint fields, confirm text)
  always carry the full fingerprint, never the condensed form.
- `.key-note` + `.note-form` — per-key operator-note primitive: the saved note
  (if any) renders as small `Note: …` text under the key type, followed by a
  small inline `POST /users/key-note` form (username + fingerprint + note,
  `maxlength=120`) with a `Save note` secondary button. Notes live in the
  `key-notes.json` sidecar next to `users.json` and never affect validation,
  reconciliation, or fingerprints.
- `.problems` — repair-card problem list: one `<li>` per invalid entry as
  `username + key #n + short reason` (e.g. `legacy key #2: not a valid SSH
  public key`), rendered above the repair button when invalid entries exist.
- Query-notice pattern: actions that redirect with a result (repair) encode it
  as validated query params (`?repaired=<n>&disabled=<u1,u2>`, empty params
  omitted) and the landing page renders a green `.notice` summary
  (`Removed N invalid entries; disabled: …` or `No invalid entries found.`).
  Params are parsed strictly server-side (integers only, usernames matching the
  existing pattern; malformed values yield no notice) and escaped on render.
- `.key-type` — muted key-algorithm label next to each fingerprint.
- `.key-invalid` — explicit invalid-entry state: danger-colored strong label
  plus the literal `(invalid)` marker (kept for compatibility) and the sentence
  "This entry fails validation and has no fingerprint." Never renders a
  fingerprint or a per-key remove button for invalid entries.
- `details` — two-level native disclosure, both closed by default,
  keyboard-togglable, JS-free. Level 1 is `.user-details` per row
  (`<td colspan="4" class="user-cell"><details
  class="user-details"><summary class="user-summary">` with username,
  `.status`, `.key-summary`, and `<span
  class="summary-manage">Manage <username></span>` grid cells):
  the scan line shows only username, `.status`, and `.key-summary`, and
  opening the disclosure reveals the full-width `.user-maintenance` block
  with every maintenance control for that user. Level 2 is the per-valid-key
  collapsed full-key inspection inside `.user-maintenance`
  (`<summary>Show full key</summary>`); the condensed fingerprint stays
  visible without opening it.
- `.user-details` — per-row disclosure wrapper (`margin:0`) inside the
  spanning cell; its `summary`
  is `cursor:pointer;font-weight:600`, uses an explicit `▸`/`▾` marker and
  accent underline for the Manage affordance, and keeps the standard
  `:focus-visible` ring. The `.user-summary` grid cells carry
  `padding:8px 10px;min-width:0;overflow-wrap:anywhere` so long names never
  force sideways overflow; an open row adds a separating
  `border-top:1px solid var(--line)` above `.user-maintenance`.
- `.user-maintenance` — the disclosed maintenance surface inside
  `.user-details` (`margin-top:8px`, full table content width): full per-key details (condensed
  fingerprint, type, note, `Show full key` details, Save-note form, Remove
  form) render as adaptive bordered key tiles; the Generate-new-key form, the
  Disable/Enable form, and the disabled-only Delete form sit in a wrapping
  `.user-actions` bar. On narrow screens the key grid becomes one column and
  its `.inline-form` controls stack full-width via a `600px` media query;
  buttons go `width:100%`.
- `.key-summary` — compact per-row key count (`white-space:nowrap`): `N
  usable keys` (`1 usable key` singular), `No keys` when empty, and an
  appended danger `.key-invalid` count (`N invalid`) when invalid entries
  exist. Counts come from `userRow.ValidKeys`/`userRow.InvalidKeys`
  (`InvalidKeys` is incremented in `userDashboard` alongside the existing
  valid-key count); full fingerprints never render in the summary.

States:

- User status: `.status` icon+tips indicator (dot + words + tooltip +
  `aria-label`); plain text `enabled` / `disabled` is always present.
- Empty keys: `No keys` in the `.key-summary` scan cell, plus a muted `No
  keys.` hint inside the row's `.user-maintenance` block.
- Valid key (inside `.user-maintenance`): condensed fingerprint `<code
  title=full>` + type + optional `.key-note` + collapsed full-key `<details>`
  + inline Save-note form + per-key Remove form (`POST /users/delete-key`
  with username/fingerprint/CSRF and a `confirm()` guard naming the full
  fingerprint).
- Invalid key: `.key-invalid` state with the literal `(invalid)` marker and
  the no-fingerprint sentence inside `.user-maintenance`, no per-key form;
  the scan cell appends an `N invalid` warning to `.key-summary`.
- Repair card: problem list (`.problems`) above the button when invalid entries
  exist; `POST /users/remove-invalid-keys` with CSRF only and button copy
  stating manifest-wide scope (`Remove all invalid keys (manifest-wide)` +
  hint). After repair the page shows the query-notice result.
- Create-user paste field stays (`POST /users/create` with `public_key`); the
  row-level `details.advanced` add-key primitive is removed. `POST
  /users/add-key` remains as an API-compatible endpoint with no visible form.
- Disable vs Enable are separate buttons in one `POST /users/status` form with
  hidden `action=disable|enable`; labels stay exactly `Disable` / `Enable`.
- Delete is a disabled-only danger control: a `POST /users/delete` form
  (username + CSRF) rendering only on disabled rows, next to `Enable`, with a
  `Delete` button carrying `class="danger"` (`button.danger,.btn.danger`
  referencing `var(--danger)` — no raw hex). Enabled rows show no Delete
  button. The form reuses the single accepted `confirm()` guard pattern
  (`onsubmit="return confirm(this.getAttribute('data-confirm'))"`) with a
  `data-confirm` message naming the user, the valid-key count, and
  irreversibility (`Delete user <name> permanently? <N> key(s) will be
  removed. This cannot be undone.`). The server enforces the disabled-only
  rule (deleting an enabled user is `409`); the UI is never trusted.
- Data retention: deleting a user removes the whole manifest entry (keys
  included) plus its sidecar notes, but the per-user data directory stays on
  disk — same as disable.
- Focus: every interactive element MUST show a visible focus ring
  (`:focus-visible{outline:2px solid var(--accent);outline-offset:2px}`).

## 6. Motion / interaction

Server-rendered, no JavaScript frameworks. There are no transitions, keyframes,
hover animations, or client-side state. "Interaction" means full-page form
POSTs with CSRf tokens and `303 → /` redirects (or `200` + dashboard with the
shared one-time result dialog for either generate path). Do not add animation, hover transforms, or JS-driven
disclosure beyond the two-level native `<details>` flow (row-level `Manage
<user>` plus per-key `Show full key` inside `.user-maintenance`). The
single accepted JS exception is the `confirm()` guard
(`onsubmit="return confirm(this.getAttribute('data-confirm'))"`), used by the
per-key Remove form (whose `data-confirm` message names the full fingerprint
being removed and warns that removing the last key disables the user) and by
the disabled-only Delete form (whose `data-confirm` message names the user,
the valid-key count, and irreversibility). Each message lives in a plain
attribute (not inline JS-string interpolation) so template escaping stays
predictable. No other inline
JS is allowed. The generate control MUST NOT
be disabled client-side (backend explicitly rejects UI-disabling assumptions;
duplicate generation is recoverable via per-key Remove).

## 7. Depth / surface

Flat. Cards use a `1px solid var(--line)` border and `8px` radius; no box
shadows, no gradients, no background images. The single exception is the
generate-result `.overlay` scrim (`var(--scrim)`), whose translucency is the
focus mechanism itself. Depth
comes from zebra table rows and border separation only. Nav is a flat dark bar.

## 8. Accessibility constraints

- Semantic HTML: `header/main/section/h1/h2/table(caption-equivalent via h2)/
  thead+th[scope=col]/ul/li/form/label`.
- Every input has an associated `<label for>`; row inputs use wrapped labels
  (`<label>Add key <input …></label>`).
- Error/invalid regions use `role="alert"`; tables use `th scope="col"`.
- Keyboard: all actions are native submit buttons reachable by Tab; both
  disclosure levels use native `<details><summary>` (row-level `Manage
  <user>`, per-key `Show full key`), keyboard-togglable with no JS;
  the feedback window is a semantic `<dialog open>` labelled by its
  `h2`, with `autofocus` requesting initial focus on the Download link and
  the Close-to-`/` anchor as the supported dismiss path (Esc does not
  dismiss without JS — documented limitation, not a bug).
- Visible `:focus-visible` outline on links, buttons, inputs, summary —
  including the `Manage <user>` summary; rings must remain visible at every
  viewport width.
- Scan-first reading: the collapsed row already answers identity at a
  glance (username, status words+dot, `.key-summary` counts); maintenance
  requires the explicit `Manage <user>` action, so nothing destructive is
  one Tab stop away by accident.
- Mobile: `.user-maintenance` controls stack full-width below 600px; only
  `.table-wrap` may introduce scrolling — the page itself never scrolls
  sideways and no other element may set horizontal overflow.
- No action depends on color alone: invalid state pairs danger color with the
  words "Invalid key"; status pairs the dot with the words `enabled/disabled`
  plus tooltip and `aria-label`.
- Key identity is never condensed where it matters: forms and confirms use the
  full fingerprint; only the visible table cell shows the condensed form, and
  the full value is one tooltip/expand away.
- Contrast: `--ink` on `--card`, `--danger` on `#fef3f2`, `--ok` on `#ecfdf3`
  all target WCAG AA at body sizes.

## 9. Accepted debt

- Scan rows show counts only; operators open `Manage <user>` for
  fingerprints — accepted because the summary names the invalid count and
  repair stays manifest-wide, so nothing actionable hides behind the click.
- Users table has no pagination or search; acceptable at operator scale.
- Repair action is manifest-wide (not per-user) because invalid entries have no
  reliable fingerprint and strict writes block while any invalid entry exists;
  per-invalid-key buttons are intentionally absent.
- `POST /users/add-key` remains only as an API-compatible endpoint with no
  visible form (the row-level `details.advanced` primitive was removed); its
  paste input keeps `type="text"` to preserve existing behavior (not upgraded
  to textarea in row context).
- Per-key notes are display-only sidecar data (`key-notes.json`); they are
  never logged, never validated as keys, and never read by the reconciler.
- Repair result travels as validated query params rendered into `.notice`;
  malformed params are ignored, never rendered.
- Login and Settings pages are out of scope for redesign; this system only
  governs their shared shell tokens.
- No client-side duplicate-submission guard on Generate; recovery is via
  visible fingerprints + per-key Remove.
