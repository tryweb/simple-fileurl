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

## 4. Spacing / layout

- Shell: `main{max-width:960px;margin:0 auto;padding:24px var(--pad) 48px}`.
- Cards: `.card{padding:var(--pad);margin:0 0 20px}` separated by 20px vertical rhythm.
- Nav: `padding:10px var(--pad)`, flex with `gap:8px 16px`, wraps on narrow screens.
- Forms: `label{margin:10px 0 4px}`; inputs `padding:8px 10px`, `width:100%`,
  `max-width:560px`; textarea `resize:vertical`.
- Buttons: `padding:8px 16px`; inline row forms `.inline-form{display:inline;margin-right:8px}`.
- Tables: `th,td{padding:8px 10px}`, full width, `border-collapse:collapse`,
  bottom borders only.
- Responsive: single column below ~600px. Tables scroll horizontally if needed
  (wrap the users table in `.table-wrap{overflow-x:auto}`); at 375px every form
  control stacks full-width and no horizontal page overflow is allowed.

## 5. Reusable primitives and states

Primitives (extend these, never one-off div soup):

- `.topnav` — brand + Users/Settings links + sign-out form. Active link gets
  `.active`. Present on every authenticated page (`ShowNav`).
- `.card` — titled section (`h2` + forms/tables). Users page cards: Create user,
  Existing users, Key repair.
- `.alert[role=alert]` — blocking errors, invalid-key state, one-time-download
  warning. `.notice` — save confirmations.
- `.hint` — inline muted help inside labels (username pattern, generate-vs-paste
  guidance, repair scope).
- `.inline-form` — row-level actions that sit side by side: Generate new key,
  Advanced add-key (inside `<details>`), per-key Remove, Disable/Enable.
- `.btn` — link styled as button (one-time download, Back to users).
- `.key-type` — muted key-algorithm label next to each fingerprint.
- `.key-invalid` — explicit invalid-entry state: danger-colored strong label
  plus the literal `(invalid)` marker (kept for compatibility) and the sentence
  "This entry fails validation and has no fingerprint." Never renders a
  fingerprint or a per-key remove button for invalid entries.
- `details.advanced` — collapsed Advanced migration flow (external
  `POST /users/add-key` paste form). Closed by default; primary flow stays
  visible without opening it.

States:

- User status: plain text `enabled` / `disabled` in the Status column.
- Empty keys: plain text `none`.
- Valid key: fingerprint `<code>SHA256:…</code>` + type + canonical key text +
  per-key Remove form (`POST /users/delete-key` with username/fingerprint/CSRF).
- Invalid key: `.key-invalid` state, no per-key form.
- Repair card: `POST /users/remove-invalid-keys` with CSRF only; copy states
  manifest-wide scope ("removes every invalid entry in the manifest, preserves
  valid keys, disables users left with zero valid keys").
- Disable vs Enable are separate buttons in one `POST /users/status` form with
  hidden `action=disable|enable`; labels stay exactly `Disable` / `Enable`.
- Focus: every interactive element MUST show a visible focus ring
  (`:focus-visible{outline:2px solid var(--accent);outline-offset:2px}`).

## 6. Motion / interaction

Server-rendered, no JavaScript. There are no transitions, keyframes, hover
animations, or client-side state. "Interaction" means full-page form POSTs with
CSRf tokens and `303 → /` redirects (or `200` + one-time download page for
generation). Do not add animation, hover transforms, or JS-driven disclosure
beyond native `<details>` for the Advanced flow. The generate control MUST NOT
be disabled client-side (backend explicitly rejects UI-disabling assumptions;
duplicate generation is recoverable via per-key Remove).

## 7. Depth / surface

Flat. Cards use a `1px solid var(--line)` border and `8px` radius; no box
shadows, no gradients, no layered transparencies, no background images. Depth
comes from zebra table rows and border separation only. Nav is a flat dark bar.

## 8. Accessibility constraints

- Semantic HTML: `header/main/section/h1/h2/table(caption-equivalent via h2)/
  thead+th[scope=col]/ul/li/form/label`.
- Every input has an associated `<label for>`; row inputs use wrapped labels
  (`<label>Add key <input …></label>`).
- Error/invalid regions use `role="alert"`; tables use `th scope="col"`.
- Keyboard: all actions are native submit buttons reachable by Tab; Advanced
  flow uses native `<details><summary>` (keyboard-togglable, no JS).
- Visible `:focus-visible` outline on links, buttons, inputs, summary.
- No action depends on color alone: invalid state pairs danger color with the
  words "Invalid key"; status pairs text `enabled/disabled` (not dots).
- Contrast: `--ink` on `--card`, `--danger` on `#fef3f2`, `--ok` on `#ecfdf3`
  all target WCAG AA at body sizes.

## 9. Accepted debt

- Users table has no pagination or search; acceptable at operator scale.
- Repair action is manifest-wide (not per-user) because invalid entries have no
  reliable fingerprint and strict writes block while any invalid entry exists;
  per-invalid-key buttons are intentionally absent.
- `POST /users/add-key` remains only as an Advanced migration path; its paste
  input keeps `type="text"` to preserve existing copy/behavior (not upgraded
  to textarea in row context).
- Login and Settings pages are out of scope for redesign; this system only
  governs their shared shell tokens.
- No client-side duplicate-submission guard on Generate; recovery is via
  visible fingerprints + per-key Remove.
