package sftpadmin

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
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
:root{--bg:#f4f5f7;--card:#fff;--ink:#1c2430;--muted:#5b6572;--line:#dfe3e8;--accent:#1f6feb;--accent-ink:#fff;--danger:#b42318;--ok:#067647;--radius:8px;--pad:16px}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
.topnav{background:#24292f;color:#fff;padding:10px var(--pad);display:flex;flex-wrap:wrap;gap:8px 16px;align-items:center}
.topnav .brand{font-weight:700;margin-right:auto}
.topnav a{color:#fff;text-decoration:none;padding:4px 8px;border-radius:4px}
.topnav a.active{background:#57606a}
.topnav button{background:transparent;color:#fff;border:1px solid #57606a;border-radius:4px;padding:4px 10px;cursor:pointer}
main{max-width:960px;margin:0 auto;padding:24px var(--pad) 48px}
.card{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:var(--pad);margin:0 0 20px}
h1{font-size:1.5rem;margin:0 0 16px}h2{font-size:1.15rem;margin:0 0 12px}
.alert{background:#fef3f2;border:1px solid #fecdca;color:var(--danger);border-radius:var(--radius);padding:10px 12px;margin:0 0 12px}
.notice{background:#ecfdf3;border:1px solid #a6f4c5;color:var(--ok);border-radius:var(--radius);padding:10px 12px;margin:0 0 12px}
label{display:block;margin:10px 0 4px;font-weight:600}
.hint{color:var(--muted);font-size:.85rem;font-weight:400}
input[type=text],input[type=password],textarea,select{width:100%;max-width:560px;padding:8px 10px;border:1px solid var(--line);border-radius:4px;font:inherit}
textarea{resize:vertical}
button,.btn{display:inline-block;background:var(--accent);color:var(--accent-ink);border:0;border-radius:4px;padding:8px 16px;font:inherit;cursor:pointer;text-decoration:none}
button.secondary{background:#57606a}
table{width:100%;border-collapse:collapse;margin-top:8px}
th,td{text-align:left;padding:8px 10px;border-bottom:1px solid var(--line);vertical-align:top}
tbody tr:nth-child(even){background:#f9fafb}
code{word-break:break-all;font-size:.85em}
.inline-form{display:inline;margin-right:8px}
.inline-form input[type=text]{width:auto;max-width:none}
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
<section class="card">
<h2>Create user</h2>
<form method="post" action="/users/create">
<input type="hidden" name="` + csrfField + `" value="{{.CSRF}}">
<label for="username">Username <span class="hint">pattern ^[a-z_][a-z0-9_-]{0,31}$, not root/admin/sshd/sftpusers</span></label>
<input type="text" id="username" name="username" required pattern="[a-z_][a-z0-9_\-]{0,31}" maxlength="32">
<label for="public_key">Public key <span class="hint">leave empty to generate an ed25519 keypair</span></label>
<textarea id="public_key" name="public_key" rows="3" cols="70"></textarea>
<button type="submit">Create</button>
</form>
</section>
<section class="card">
<h2>Existing users</h2>
{{if .Users}}
<table>
<thead><tr><th scope="col">Username</th><th scope="col">Status</th><th scope="col">Keys</th><th scope="col">Actions</th></tr></thead>
<tbody>
{{range .Users}}<tr>
<td>{{.Username}}</td><td>{{if .Enabled}}enabled{{else}}disabled{{end}}</td>
<td>{{if .Keys}}<ul>{{range .Keys}}<li><code>{{.Fingerprint}}</code><br><code>{{.Key}}</code></li>{{end}}</ul>{{else}}none{{end}}</td>
<td>
<form class="inline-form" method="post" action="/users/add-key">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{.Username}}">
<label>Add key <input type="text" name="public_key" required size="40"></label>
<button type="submit">Add</button>
</form>
<form class="inline-form" method="post" action="/users/status">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{.Username}}">
{{if .Enabled}}<input type="hidden" name="action" value="disable"><button class="secondary" type="submit">Disable</button>
{{else}}<input type="hidden" name="action" value="enable"><button type="submit">Enable</button>{{end}}
</form>
</td>
</tr>{{end}}
</tbody>
</table>
{{else}}<p>No users yet.</p>{{end}}
</section>
{{end}}`))

var createdTemplate = template.Must(template.Must(baseTemplate.Clone()).Parse(`{{define "content"}}
<section class="card">
<h1>User {{.Username}} created</h1>
<p>Public key fingerprint: <code>{{.Fingerprint}}</code></p>
<p class="alert" role="alert">The private key below can be downloaded exactly once. It is never stored. Save it now.</p>
<p><a class="btn" href="/keys/download?token={{.Token}}">Download private key (one-time)</a></p>
<p><a href="/">Back to users</a></p>
</section>
{{end}}`))

type loginView struct {
	pageData
	Error string
}

type keyView struct {
	Key         string
	Fingerprint string
}

type userRow struct {
	Username string
	Enabled  bool
	Keys     []keyView
}

type usersView struct {
	pageData
	Users []userRow
}

type createdView struct {
	pageData
	Username    string
	Token       string
	Fingerprint string
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

// userDashboard builds the list view. Stored keys were validated on write;
// a corrupt entry surfaces as "(invalid)" rather than breaking the page.
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
				continue
			}
			row.Keys = append(row.Keys, keyView{Key: canon, Fingerprint: fp})
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
	render(w, func(out io.Writer) error {
		return usersTemplate.Execute(out, v)
	})
}

func renderCreated(w http.ResponseWriter, v createdView) {
	v.Title = "User created"
	render(w, func(out io.Writer) error {
		return createdTemplate.Execute(out, v)
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
	var buf bytes.Buffer
	if err := execute(&buf); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
