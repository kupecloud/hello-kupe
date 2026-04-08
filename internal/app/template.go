package app

import "html/template"

const homePage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ .ServiceName }}</title>
    <style>
      :root {
        color-scheme: light;
        --bg: #f5f7f4;
        --card: #ffffff;
        --text: #0f172a;
        --muted: #475569;
        --accent: #0f766e;
        --border: #dbe4dc;
      }
      body {
        margin: 0;
        font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        background: radial-gradient(circle at top, #e2f7e9 0%, var(--bg) 45%, #edf2f7 100%);
        color: var(--text);
      }
      main {
        max-width: 760px;
        margin: 48px auto;
        padding: 0 20px;
      }
      .card {
        background: var(--card);
        border: 1px solid var(--border);
        border-radius: 20px;
        padding: 28px;
        box-shadow: 0 20px 50px rgba(15, 23, 42, 0.08);
      }
      .eyebrow {
        display: inline-block;
        margin-bottom: 12px;
        color: var(--accent);
        font-weight: 700;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        font-size: 12px;
      }
      h1 {
        margin: 0 0 12px;
        font-size: clamp(2rem, 4vw, 3rem);
        line-height: 1.05;
      }
      p {
        color: var(--muted);
        line-height: 1.6;
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 10px 16px;
        margin: 24px 0;
      }
      dt {
        font-weight: 700;
      }
      dd {
        margin: 0;
        color: var(--muted);
      }
      code {
        font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
        background: #f1f5f9;
        border-radius: 8px;
        padding: 2px 6px;
      }
      ul {
        padding-left: 18px;
      }
      a {
        color: var(--accent);
      }
    </style>
  </head>
  <body>
    <main>
      <section class="card">
        <span class="eyebrow">Kupe Cloud Quickstart</span>
        <h1>Hello from {{ .ServiceName }}</h1>
        <p>
          This app is the official Kupe Cloud quickstart workload. It gives you a working
          HTTP service, structured logs, and a scrapeable metrics endpoint in a single deploy.
        </p>
        <dl>
          <dt>Tenant</dt>
          <dd>{{ .Tenant }}</dd>
          <dt>Pod</dt>
          <dd>{{ .PodName }}</dd>
          <dt>Namespace</dt>
          <dd>{{ .PodNamespace }}</dd>
          <dt>Public URL</dt>
          <dd><a href="{{ .PublicURL }}">{{ .PublicURL }}</a></dd>
        </dl>
        <p>Useful endpoints:</p>
        <ul>
          <li><code>/</code> this page</li>
          <li><code>/api/hello</code> JSON response</li>
          <li><code>/healthz</code> liveness endpoint</li>
          <li><code>/readyz</code> readiness endpoint</li>
          <li><code>/metrics</code> Prometheus-format metrics</li>
        </ul>
      </section>
    </main>
  </body>
</html>
`

func newHomeTemplate() (*template.Template, error) {
	return template.New("home").Parse(homePage)
}
