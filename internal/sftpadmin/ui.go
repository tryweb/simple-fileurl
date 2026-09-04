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

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>SFTP admin login</title></head>
<body>
<main>
<h1>SFTP admin</h1>
{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/login">
<label for="password">Admin password</label>
<input type="password" id="password" name="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form>
</main>
</body>
</html>`))

var usersTemplate = template.Must(template.New("users").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>SFTP users</title></head>
<body>
<main>
<h1>SFTP users</h1>
<form method="post" action="/logout"><input type="hidden" name="` + csrfField + `" value="{{.CSRF}}"><button type="submit">Sign out</button></form>
<h2>Create user</h2>
<form method="post" action="/users/create">
<input type="hidden" name="` + csrfField + `" value="{{.CSRF}}">
<label for="username">Username (pattern ^[a-z_][a-z0-9_-]{0,31}$, not root/admin/sshd/sftpusers)</label>
<input type="text" id="username" name="username" required pattern="[a-z_][a-z0-9_\-]{0,31}" maxlength="32">
<label for="public_key">Public key (leave empty to generate an ed25519 keypair)</label>
<textarea id="public_key" name="public_key" rows="3" cols="70"></textarea>
<button type="submit">Create</button>
</form>
<h2>Existing users</h2>
{{if .Users}}
<table>
<thead><tr><th scope="col">Username</th><th scope="col">Status</th><th scope="col">Keys</th><th scope="col">Actions</th></tr></thead>
<tbody>
{{range .Users}}<tr>
<td>{{.Username}}</td><td>{{if .Enabled}}enabled{{else}}disabled{{end}}</td>
<td>{{if .Keys}}<ul>{{range .Keys}}<li><code>{{.Fingerprint}}</code><br><code>{{.Key}}</code></li>{{end}}</ul>{{else}}none{{end}}</td>
<td>
<form method="post" action="/users/add-key">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{.Username}}">
<label>Add key <input type="text" name="public_key" required size="40"></label>
<button type="submit">Add</button>
</form>
<form method="post" action="/users/status">
<input type="hidden" name="` + csrfField + `" value="{{$.CSRF}}">
<input type="hidden" name="username" value="{{.Username}}">
{{if .Enabled}}<input type="hidden" name="action" value="disable"><button type="submit">Disable</button>
{{else}}<input type="hidden" name="action" value="enable"><button type="submit">Enable</button>{{end}}
</form>
</td>
</tr>{{end}}
</tbody>
</table>
{{else}}<p>No users yet.</p>{{end}}
</main>
</body>
</html>`))

var createdTemplate = template.Must(template.New("created").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>User created</title></head>
<body>
<main>
<h1>User {{.Username}} created</h1>
<p>Public key fingerprint: <code>{{.Fingerprint}}</code></p>
<p role="alert">The private key below can be downloaded exactly once. It is never stored. Save it now.</p>
<p><a href="/keys/download?token={{.Token}}">Download private key (one-time)</a></p>
<p><a href="/">Back to users</a></p>
</main>
</body>
</html>`))

type loginView struct {
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
	CSRF  string
	Users []userRow
}

type createdView struct {
	Username    string
	Token       string
	Fingerprint string
}

// userDashboard builds the list view. Stored keys were validated on write;
// a corrupt entry surfaces as "(invalid)" rather than breaking the page.
func userDashboard(m Manifest, csrf string) usersView {
	v := usersView{CSRF: csrf}
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
		return loginTemplate.Execute(out, loginView{Error: errMsg})
	})
}

func renderUsers(w http.ResponseWriter, v usersView) {
	render(w, func(out io.Writer) error {
		return usersTemplate.Execute(out, v)
	})
}

func renderCreated(w http.ResponseWriter, v createdView) {
	render(w, func(out io.Writer) error {
		return createdTemplate.Execute(out, v)
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
