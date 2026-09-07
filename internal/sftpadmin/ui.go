package sftpadmin

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// This file owns all admin HTML. Pages are plain server-rendered forms with
// labelled inputs, table headers, and error regions so the UI stays usable
// with keyboard and screen readers and without JavaScript.

// baseTemplate is the shared shell: styles plus an optional top nav, with
// each page supplying a "content" block. Cloned per page, never executed
// directly.
var baseTemplate = template.Must(template.New("base").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title>
<style>
:root{--bg:#f4f5f7;--card:#fff;--ink:#1c2430;--muted:#5b6572;--line:#dfe3e8;--accent:#1f6feb;--accent-ink:#fff;--danger:#b42318;--ok:#067647;--nav:#24292f;--nav-muted:#57606a;--alert-bg:#fef3f2;--alert-line:#fecdca;--notice-bg:#ecfdf3;--notice-line:#a6f4c5;--row-alt:#f9fafb;--scrim:rgba(28,36,48,.55);--overlay-top:10vh;--radius:8px;--pad:16px}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
.topnav{background:var(--nav);color:var(--accent-ink);padding:10px var(--pad);display:flex;flex-wrap:wrap;gap:8px 16px;align-items:center}
.topnav .brand{font-weight:700;margin-right:auto}
.topnav a{color:var(--accent-ink);text-decoration:none;padding:4px 8px;border-radius:4px}
.topnav a.active{background:var(--nav-muted)}
.topnav button{background:transparent;color:var(--accent-ink);border:1px solid var(--nav-muted);border-radius:4px;padding:4px 10px;cursor:pointer}
main{max-width:1200px;margin:0 auto;padding:24px var(--pad) 48px}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:var(--pad);margin:0 0 20px}
h1{font-size:1.5rem;margin:0 0 16px}h2{font-size:1.15rem;margin:0 0 12px}
.alert{background:var(--alert-bg);border:1px solid var(--alert-line);color:var(--danger);border-radius:var(--radius);padding:10px 12px;margin:0 0 12px}
.notice{background:var(--notice-bg);border:1px solid var(--notice-line);color:var(--ok);border-radius:var(--radius);padding:10px 12px;margin:0 0 12px}
label{display:block;margin:10px 0 4px;font-weight:600}
.hint{color:var(--muted);font-size:.85rem;font-weight:400}
input[type=text],input[type=password],textarea,select{width:100%;max-width:560px;padding:8px 10px;border:1px solid var(--line);border-radius:4px;font:inherit}
textarea{resize:vertical}
button,.btn{display:inline-block;background:var(--accent);color:var(--accent-ink);border:0;border-radius:4px;padding:8px 16px;font:inherit;cursor:pointer;text-decoration:none}
button.secondary,.btn.secondary{background:var(--nav-muted)}
button.danger,.btn.danger{background:var(--danger)}
table{width:100%;border-collapse:collapse;margin-top:8px}
th,td{text-align:left;padding:8px 10px;border-bottom:1px solid var(--line);vertical-align:top}
tbody tr:nth-child(even){background:var(--row-alt)}
code{word-break:break-all;font-size:.85em}
.key-type{color:var(--muted);font-size:.85rem}
.key-invalid{color:var(--danger);font-weight:600}
.table-wrap{overflow-x:auto}
.users-table{table-layout:fixed}
.users-table .col-username{width:24%}
.users-table .col-status{width:18%}
.users-table .col-keys{width:22%}
.users-table .col-manage{width:36%}
.key-summary{white-space:nowrap}
.user-cell{padding:0}
.user-details{margin:0}
.user-details summary{cursor:pointer;font-weight:600}
.user-summary{display:grid;grid-template-columns:24% 18% 22% 36%;align-items:start;gap:0}
.user-summary>span{padding:8px 10px;min-width:0;overflow-wrap:anywhere}
.user-summary .summary-manage{font-weight:600;color:var(--accent);text-decoration:underline;text-underline-offset:2px}
.user-summary .summary-manage::before{content:"▸";display:inline-block;margin-right:6px;text-decoration:none}
.user-details[open] .summary-manage::before{content:"▾"}
.user-details[open] .user-maintenance{border-top:1px solid var(--line);padding:8px 10px 4px}
.user-maintenance{margin-top:8px}
.user-maintenance ul{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:12px;margin:8px 0;padding:0;list-style:none}
.user-maintenance li{min-width:0;padding:8px;border:1px solid var(--line);border-radius:4px}
.user-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px}
.user-actions .inline-form{margin:0}
@media (max-width:600px){.users-table{min-width:0;table-layout:auto}.users-table thead{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}.user-summary{grid-template-columns:repeat(2,minmax(0,1fr))}.user-summary>span{padding:8px 6px}.user-maintenance ul{grid-template-columns:1fr}.user-maintenance .inline-form{display:block;margin:0 0 8px}.user-maintenance .inline-form input[type=text]{width:100%;max-width:100%}.user-maintenance button{width:100%}}
a:focus-visible,button:focus-visible,input:focus-visible,textarea:focus-visible,select:focus-visible,summary:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.inline-form{display:inline;margin-right:8px}
.inline-form input[type=text]{width:auto;max-width:none}
.form-actions{margin-top:12px}
.status{display:inline-flex;align-items:center;gap:6px;white-space:nowrap}
.status-dot{flex:none}
.key-note{font-size:.85rem}
.note-form{margin-top:4px}
.problems{margin:8px 0}
.overlay{position:fixed;inset:0;background:var(--scrim);display:flex;align-items:flex-start;justify-content:center;padding:clamp(24px,var(--overlay-top),96px) 16px 48px;z-index:50}
.overlay-card{position:static;width:100%;max-width:560px;margin:0}
</style>
</head>
<body>
{{if .ShowNav}}<header class="topnav"><span class="brand">SFTP admin</span><a href="/"{{if eq .Active "users"}} class="active"{{end}}>Users</a><a href="/settings"{{if eq .Active "settings"}} class="active"{{end}}>Settings</a><form class="inline-form" method="post" action="/logout"><input type="hidden" name="` + csrfField + `" value="{{.CSRF}}"><button type="submit">Sign out</button></form></header>{{end}}
<main>{{template "content" .}}</main>
</body>
</html>`))

// pageData carries the fields every page shell needs. Content structs
// embed it so templates see a uniform shape.
type pageData struct {
	Title   string
	ShowNav bool
	Active  string
	CSRF    string
}

var loginTemplate = template.Must(template.Must(baseTemplate.Clone()).Parse(`{{define "content"}}
<section class="card">
<h1>SFTP admin</h1>
{{if .Error}}<p class="alert" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/login">
<label for="password">Admin password</label>
<input type="password" id="password" name="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form>
</section>
{{end}}`))

var usersTemplate = template.Must(template.Must(baseTemplate.Clone()).Parse(`{{define "content"}}
<h1>SFTP users</h1>
{{if .Generated}}<div class="overlay"><dialog class="card overlay-card" open aria-labelledby="generated-title">
<h2 id="generated-title">Key generated for {{.Generated.Username}}</h2>
<p>Public key fingerprint: <code>{{.Generated.Fingerprint}}</code></p>
<p class="alert" role="alert">The private key below can be downloaded exactly once. It is never stored. Save it now.</p>
<p><a class="btn" autofocus href="/keys/download?token={{.Generated.Token}}">Download private key (one-time)</a></p>
<p><a class="btn secondary" href="/">Close</a></p>
</dialog></div>{{else if .Error}}<div class="overlay"><dialog class="card overlay-card" open aria-labelledby="feedback-title">
<h2 id="feedback-title">{{.ErrorTitle}}</h2>
<p class="alert" role="alert">{{.Error}}</p>
<p><a class="btn secondary" autofocus href="/">Close</a></p>
</dialog></div>{{end}}
{{if .Notice}}<p class="notice">{{.Notice}}</p>{{end}}
<section class="card" id="existing-users">
<h2>Existing users</h2>
{{if .Users}}
<div class="table-wrap">
<table class="users-table">
<colgroup><col class="col-username"><col class="col-status"><col class="col-keys"><col class="col-manage"></colgroup>
<thead><tr><th scope="col">Username</th><th scope="col">Status</th><th scope="col">Keys</th><th scope="col">Manage</th></tr></thead>
<tbody>
{{range .Users}}{{$u := .}}<tr>
<td colspan="4" class="user-cell"><details class="user-details"><summary class="user-summary"><span>{{$u.Username}}</span><span>{{if $u.Enabled}}<span class="status" title="Enabled: new logins accepted" aria-label="Status: enabled, new logins accepted"><svg class="status-dot" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true" focusable="false"><circle cx="5" cy="5" r="4" fill="var(--ok)"/></svg>enabled</span>{{else}}<span class="status" title="Disabled: no new logins" aria-label="Status: disabled, no new logins"><svg class="status-dot" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true" focusable="false"><circle cx="5" cy="5" r="4" fill="var(--nav-muted)"/></svg>disabled</span>{{end}}</span>
<span><span class="key-summary">{{if $u.InvalidKeys}}{{if $u.ValidKeys}}{{$u.ValidKeys}} usable {{if eq $u.ValidKeys 1}}key{{else}}keys{{end}}, {{end}}<span class="key-invalid">{{$u.InvalidKeys}} invalid</span>{{else}}{{if $u.ValidKeys}}{{$u.ValidKeys}} usable {{if eq $u.ValidKeys 1}}key{{else}}keys{{end}}{{else}}No keys{{end}}{{end}}</span></span>
<span class="summary-manage">Manage {{$u.Username}}</span></summary><div class="user-maintenance">
{{if $u.Keys}}<ul>{{range $u.Keys}}{{if .Valid}}<li><code title="{{.Fingerprint}}">{{.Short}}</code><br><span class="key-type">{{.Type}}</span>{{if .Note}}<br><span class="key-note">Note: {{.Note}}</span>{{end}}<br><details><summary>Show full key</summary><code>{{.Key}}</code></details><form class="inline-form note-form" method="post" action="/users/key-note"><input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}"><input type="hidden" name="username" value="{{$u.Username}}"><input type="hidden" name="fingerprint" value="{{.Fingerprint}}"><label>Note <input type="text" name="note" value="{{.Note}}" maxlength="120" size="20"></label><button class="secondary" type="submit">Save note</button></form><form class="inline-form" method="post" action="/users/delete-key" data-confirm="Remove key {{.Fingerprint}}? Removing the last key disables the user." onsubmit="return confirm(this.getAttribute('data-confirm'))"><input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}"><input type="hidden" name="username" value="{{$u.Username}}"><input type="hidden" name="fingerprint" value="{{.Fingerprint}}"><button class="secondary" type="submit">Remove</button></form></li>{{else}}<li><span class="key-invalid">Invalid key <code>(invalid)</code></span><br><span class="hint">This entry fails validation and has no fingerprint.</span></li>{{end}}{{end}}</ul>{{else}}<p class="hint">No keys.</p>{{end}}
<div class="user-actions"><form class="inline-form" method="post" action="/users/generate-key">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{$u.Username}}">
<button type="submit">Generate new key</button>
</form>
<form class="inline-form" method="post" action="/users/status">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{$u.Username}}">
{{if $u.Enabled}}<input type="hidden" name="action" value="disable"><button class="secondary" type="submit">Disable</button>
{{else}}<input type="hidden" name="action" value="enable"><button type="submit">Enable</button>{{end}}
</form>
{{if not $u.Enabled}}<form class="inline-form" method="post" action="/users/delete" data-confirm="Delete user {{$u.Username}} permanently? {{$u.ValidKeys}} key(s) will be removed. This cannot be undone." onsubmit="return confirm(this.getAttribute('data-confirm'))">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{$u.Username}}">
<button class="danger" type="submit">Delete</button>
</form>{{end}}</div>
</div></details></td>
</tr>{{end}}
</tbody>
</table>
</div>
{{else}}<p>No users yet.</p>{{end}}
</section>
<section class="card" id="create-user">
<h2>Create user</h2>
<p><span class="hint">Advanced compatibility: paste an externally generated public key, or leave empty to generate an ed25519 keypair.</span></p>
<form method="post" action="/users/create">
<input type="hidden" name="` + csrfField + `" value="{{.CSRF}}">
<label for="username">Username <span class="hint">pattern ^[a-z_][a-z0-9_-]{0,31}$, not root/admin/sshd/sftpusers</span></label>
<input type="text" id="username" name="username" required pattern="[a-z_][a-z0-9_\-]{0,31}" maxlength="32">
<label for="public_key">Public key <span class="hint">leave empty to generate an ed25519 keypair</span></label>
<textarea id="public_key" name="public_key" rows="3" cols="70"></textarea>
<div class="form-actions"><button type="submit">Create</button></div>
</form>
</section>
<section class="card" id="key-repair">
<h2>Key repair</h2>
{{if .HasInvalid}}<p class="alert" role="alert">Some stored entries fail validation and block ordinary writes. Repair removes every invalid entry in the manifest at once.</p>{{end}}
{{if .Problems}}<ul class="problems">{{range .Problems}}<li>{{.}}</li>{{end}}</ul>{{end}}
<p><span class="hint">Removes every invalid entry in the manifest, preserves all valid keys, and disables users left with zero valid keys.</span></p>
<form method="post" action="/users/remove-invalid-keys">
<input type="hidden" name="` + csrfField + `" value="{{.CSRF}}">
<button class="secondary" type="submit">Remove all invalid keys (manifest-wide)</button>
</form>
</section>
{{end}}`))

type loginView struct {
	pageData
	Error string
}

type keyView struct {
	Key         string
	Fingerprint string
	Short       string
	Type        string
	Valid       bool
	Note        string
}

type userRow struct {
	Username string
	Enabled  bool
	Keys     []keyView
	// ValidKeys counts the row's valid keys for the delete confirm text.
	ValidKeys int
	// InvalidKeys counts the row's entries failing validation, surfaced in
	// the compact key summary (e.g. "1 usable key, 1 invalid").
	InvalidKeys int
}

// generatedResult carries the one-time result shared by both generate
// paths (create-user with an empty key, existing-user row generation).
// It renders only when present: the plain GET / dashboard leaves it nil
// so the card never appears.
type generatedResult struct {
	Username    string
	Token       string
	Fingerprint string
}

type usersView struct {
	pageData
	Users      []userRow
	HasInvalid bool
	Problems   []string
	Notice     string
	Generated  *generatedResult
	ErrorTitle string
	Error      string
}

var settingsTemplate = template.Must(template.Must(baseTemplate.Clone()).Parse(`{{define "content"}}
<h1>Settings</h1>
{{if .Notice}}<p class="notice">{{.Notice}}</p>{{end}}
{{if .Errors}}<div class="alert" role="alert"><ul>{{range $k, $v := .Errors}}<li><strong>{{$k}}</strong>: {{$v}}</li>{{end}}</ul></div>{{end}}
<form method="post" action="/settings">
<input type="hidden" name="` + csrfField + `" value="{{.CSRF}}">
{{range .Groups}}<section class="card">
<h2>{{.Service}}</h2>
{{range .Items}}
<label for="setting-{{.Key}}">{{.Key}} <span class="hint">{{.Description}} ({{.AllowedValues}})</span></label>
{{if .Options}}<select id="setting-{{.Key}}" name="{{.Key}}">{{ $v := .Value }}{{range .Options}}<option value="{{.}}"{{if eq . $v}} selected{{end}}>{{.}}</option>{{end}}</select>
{{else if .IsSecret}}<input type="password" id="setting-{{.Key}}" name="{{.Key}}" placeholder="unchanged" autocomplete="new-password">
{{else}}<input type="text" id="setting-{{.Key}}" name="{{.Key}}" value="{{.Value}}">{{end}}
{{end}}
</section>{{end}}
<button type="submit">Save settings</button>
</form>
{{end}}`))

type settingsGroup struct {
	Service string
	Items   []SettingValue
}

type settingsView struct {
	pageData
	Groups []settingsGroup
	Notice string
	Errors map[string]string
}

// shortFingerprint condenses a canonical fingerprint for table display:
// SHA256:<first8>…<last8> of the hash body. The full fingerprint stays in
// the title tooltip, the per-key details, and every form value.
func shortFingerprint(fp string) string {
	const prefix = "SHA256:"
	body := strings.TrimPrefix(fp, prefix)
	if len(body) <= 20 {
		return fp
	}
	return prefix + body[:8] + "…" + body[len(body)-8:]
}

// invalidProblems lists every stored entry failing canonical validation as
// username + 1-based entry position + short reason for the repair card.
func invalidProblems(m Manifest) []string {
	var out []string
	for _, u := range m.Users {
		for i, k := range u.AuthorizedKeys {
			if _, _, err := ValidatePublicKey(k); err != nil {
				out = append(out, u.Username+" key #"+strconv.Itoa(i+1)+": not a valid SSH public key")
			}
		}
	}
	return out
}

// userDashboard builds the list view. Stored keys were validated on write;
// a corrupt entry surfaces as an explicit invalid state (still carrying the
// "(invalid)" marker) rather than breaking the page, and flags the
// manifest-wide repair action.
func userDashboard(m Manifest, csrf string) usersView {
	v := usersView{Users: []userRow{}}
	v.Title = "SFTP users"
	v.ShowNav = true
	v.Active = "users"
	v.CSRF = csrf
	for _, u := range m.Users {
		row := userRow{Username: u.Username, Enabled: u.Enabled}
		for _, k := range u.AuthorizedKeys {
			canon, fp, err := ValidatePublicKey(k)
			if err != nil {
				row.Keys = append(row.Keys, keyView{Key: "(invalid)", Fingerprint: "(invalid)"})
				row.InvalidKeys++
				v.HasInvalid = true
				continue
			}
			typ := canon
			if i := strings.Index(canon, " "); i >= 0 {
				typ = canon[:i]
			}
			row.Keys = append(row.Keys, keyView{Key: canon, Fingerprint: fp, Short: shortFingerprint(fp), Type: typ, Valid: true})
			row.ValidKeys++
		}
		v.Users = append(v.Users, row)
	}
	return v
}

func renderLogin(w http.ResponseWriter, errMsg string) {
	render(w, func(out io.Writer) error {
		return loginTemplate.Execute(out, loginView{pageData: pageData{Title: "SFTP admin login"}, Error: errMsg})
	})
}

func renderUsers(w http.ResponseWriter, v usersView) {
	renderStatus(w, http.StatusOK, func(out io.Writer) error {
		return usersTemplate.Execute(out, v)
	})
}

func renderUsersStatus(w http.ResponseWriter, v usersView, status int) {
	renderStatus(w, status, func(out io.Writer) error {
		return usersTemplate.Execute(out, v)
	})
}

func renderSettings(w http.ResponseWriter, v settingsView) {
	v.Title = "Settings"
	v.ShowNav = true
	v.Active = "settings"
	render(w, func(out io.Writer) error {
		return settingsTemplate.Execute(out, v)
	})
}

// render buffers template output so a render failure becomes a clean 500
// instead of a half-written page.
func render(w http.ResponseWriter, execute func(io.Writer) error) {
	renderStatus(w, http.StatusOK, execute)
}

func renderStatus(w http.ResponseWriter, status int, execute func(io.Writer) error) {
	var buf bytes.Buffer
	if err := execute(&buf); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
