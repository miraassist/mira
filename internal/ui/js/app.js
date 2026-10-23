// Mira's page, the parts built on Lit (docs/spec/uebersicht.md): the start
// page with its cards, the navigation inside a group, the skills and the
// agents at a glance. The older views are still the page's script; they move
// over one by one. The components render into the page itself (no shadow
// DOM), so the page's styles apply, and they use the page's helpers: T (the
// texts), TABS, GROUPS, go, api, isAdmin, tab.
import { LitElement, html, nothing } from "./lit-core.min.js";

class Light extends LitElement {
  createRenderRoot() { return this; }
}

// groupOf is the group a tab belongs to.
const groupOf = id => GROUPS.find(g => g.tabs.includes(id));
// tabsOf are the tabs of a group this visitor may see.
const tabsOf = g => g.tabs.filter(id => TABS.includes(id));

// ---- Navigation -----------------------------------------------------------
// On the start page nothing; in a group the way back and the group's tabs.
class MiraNav extends Light {
  static properties = { tab: {} };
  render() {
    if (!this.tab || this.tab === "home") return nothing;
    const group = groupOf(this.tab);
    const tabs = group ? tabsOf(group) : [];
    return html`
      <button class="secondary back" @click=${() => go("home")}>← ${T.tab_home}</button>
      ${group ? html`<span class="group-title">${T["group_" + group.id]}</span>` : nothing}
      ${tabs.length > 1 ? tabs.map(id => html`
        <button class=${id === this.tab ? "active" : ""} @click=${() => go(id)}>${T["tab_" + id]}</button>`) : nothing}`;
  }
}
customElements.define("mira-nav", MiraNav);

// ---- Start page -----------------------------------------------------------
// A card per group with a short state; a click opens the group's first tab.
class MiraHome extends Light {
  static properties = { facts: { state: true } };
  constructor() { super(); this.facts = {}; }
  connectedCallback() { super.connectedCallback(); this.load(); }
  async load() {
    const facts = {};
    const tries = [
      ["plans", async () => (await api("GET", "/v1/ui/plans")).plans || []],
    ];
    if (isAdmin) {
      tries.push(
        ["messages", async () => (await api("GET", "/v1/ui/messages?open=1")).items || []],
        ["tasks", async () => (await api("GET", "/v1/ui/tasks")).tasks || []],
        ["catalog", async () => await api("GET", "/v1/ui/catalog")]);
    }
    // The guide's chapters, a few of them as a teaser; the guide is for everyone.
    tries.push(["topics", async () => {
      const md = await (await fetch(url("/ui/guide/" + lang))).text();
      return md.split("\n").filter(l => l.startsWith("## ")).map(l => l.slice(3).trim());
    }]);
    await Promise.all(tries.map(async ([k, f]) => { try { facts[k] = await f(); } catch (e) { /* the card stays without */ } }));
    this.facts = facts;
  }
  lines(group) {
    const f = this.facts, out = [];
    switch (group) {
      case "daily":
        if (f.messages) out.push(f.messages.length ? T.home_messages(f.messages.length) : T.home_no_messages);
        if (f.plans) out.push(f.plans.length ? T.home_plans(f.plans.length) : T.home_no_plans);
        if (f.tasks) out.push(T.home_tasks(f.tasks.length));
        break;
      case "setup": {
        const v = name => (config[name] || {}).value;
        const count = x => (x && typeof x === "object" ? Object.keys(x).length : 0);
        if (v("satellites")) out.push(T.home_satellites(count(v("satellites"))));
        if (v("persons")) out.push(T.home_persons(count(v("persons"))));
        const q = (v("notifier") || {}).quiet_hours;
        if (q && q.from && q.to) out.push(T.home_quiet(q.from, q.to));
        break;
      }
      case "abilities":
        if (f.catalog) {
          const by = s => f.catalog.skills.filter(x => x.state === s).length;
          out.push(T.home_skills(by("active"), f.catalog.agents.length));
          const failed = (f.catalog.models || []).filter(m => m.last_failed && (!m.last_ok || m.last_failed > m.last_ok)).length;
          out.push(failed ? html`<span class="warn">${T.home_models(f.catalog.models.length, failed)}</span>` : T.home_models((f.catalog.models || []).length, 0));
          if (by("problem")) out.push(html`<span class="warn">${T.home_problems(by("problem"))}</span>`);
          if (by("available")) out.push(T.home_available(by("available")));
        }
        break;
    }
    return out;
  }
  render() {
    return html`<div class="home-grid">${GROUPS.filter(g => tabsOf(g).length).map(g => html`
      <section class="card home-card" role="button" tabindex="0"
        @click=${() => go(tabsOf(g)[0])} @keydown=${e => { if (e.key === "Enter") go(tabsOf(g)[0]); }}>
        <h2>${T["group_" + g.id]}</h2>
        ${g.id === "help" ? this.chapters() : html`
          <p class="home-pages">${tabsOf(g).map(id => this.link(T["tab_" + id], () => go(id)))}</p>
          <p class="home-teaser">${T["teaser_" + g.id]}</p>`}
        ${g.id === "help" ? html`<div class="home-facts"><p class="home-line">${T.teaser_help}</p></div>`
          : this.lines(g.id).length ? html`<div class="home-facts">${this.lines(g.id).map(l => html`<p class="home-line">${l}</p>`)}</div>` : nothing}
      </section>`)}</div>`;
  }
  // link opens a page of the card, not the card's first one.
  link(text, open) {
    return html`<a href="#" @click=${e => { e.preventDefault(); e.stopPropagation(); open(); }}
      @keydown=${e => e.stopPropagation()}>${text}</a>`;
  }
  // chapters are three of the guide's chapters by a short name, each a link
  // into the guide, where the other cards name their pages.
  chapters() {
    const topics = this.facts.topics || [];
    return html`<p class="home-pages">${T.home_chapters.map(([name, begins]) => {
      const n = topics.findIndex(t => t.startsWith(begins));
      return n < 0 ? nothing : this.link(name, () => openChapter(n));
    })}</p>`;
  }
}
customElements.define("mira-home", MiraHome);

// ---- Catalog --------------------------------------------------------------
// The skills and agents as Mira loaded them, read once per view.
let catalogLoad = null;
const catalog = (fresh = false) => {
  if (fresh || !catalogLoad) catalogLoad = api("GET", "/v1/ui/catalog");
  return catalogLoad;
};
const badge = (text, ui = false) => html`<span class=${ui ? "badge ui" : "badge"}>${text}</span>`;
const row = (label, value) => value === undefined || value === null || value === "" || (Array.isArray(value) && !value.length)
  ? nothing : html`<tr><th>${label}</th><td>${value}</td></tr>`;
const code = list => list.map((x, i) => html`${i ? ", " : ""}<code>${x}</code>`);

// cronWords says a daily cron expression in words ("täglich 07:00, 12:00");
// anything else stays as it is.
function cronWords(cron) {
  const m = /^(\d+) ([\d,]+) \* \* \*$/.exec(cron.trim());
  if (!m) return html`<code>${cron}</code>`;
  const times = m[2].split(",").map(h => `${h.padStart(2, "0")}:${m[1].padStart(2, "0")}`);
  return `${T.cat_daily} ${times.join(", ")}`;
}

// ---- Editing --------------------------------------------------------------
// Models, agents and skills can be changed (docs/spec/bearbeiten.md): the
// page writes mira.yaml and the user's own SKILL.md files; what it saves
// holds after a restart. One dialog serves all three.
let editLoad = null;
const edits = (fresh = false) => {
  if (fresh || !editLoad) editLoad = api("GET", "/v1/ui/edit").catch(() => null);
  return editLoad;
};
// edited tells the pages to read again after a change.
const edited = () => { catalog(true); edits(true); window.dispatchEvent(new Event("mira-edited")); };

// EditPage is a page that offers editing: it knows the edit state and shows
// the restart bar.
class EditPage extends Light {
  static properties = { data: { state: true }, error: { state: true }, ed: { state: true } };
  connectedCallback() {
    super.connectedCallback();
    this.reload = () => this.load(true);
    window.addEventListener("mira-edited", this.reload);
    this.load(true);
  }
  disconnectedCallback() { super.disconnectedCallback(); window.removeEventListener("mira-edited", this.reload); }
  load(fresh) {
    catalog(fresh).then(d => { this.data = d; }, e => { this.error = e.message; });
    edits(fresh).then(s => { this.ed = s; });
  }
  // canEdit: editing is there and the file can be written.
  get canEdit() { return !!(this.ed && !this.ed.read_only); }
  bar() {
    const s = this.ed;
    if (!s) return nothing;
    return html`
      ${s.read_only ? html`<p class="msg warn-box">${T.ed_read_only(s.read_only)}</p>` : nothing}
      ${s.restart_needed ? html`<div class="restart-bar"><span>${T.ed_restart_needed}</span>
        <button class="primary" @click=${() => restartMira()}>${T.ed_restart}</button></div>` : nothing}`;
  }
  editButton(kind, name, label = T.ed_edit) {
    return this.canEdit ? html`<button class="secondary" @click=${() => openEditor(kind, name)}>${label}</button>` : nothing;
  }
  // pending are the entries saved since the start, which Mira loads only
  // at the restart: shown so they can be opened again.
  pending(kind, loaded) {
    if (!this.ed) return nothing;
    const names = Object.keys(this.ed[kind] || {}).filter(n => !loaded.includes(n) && !(this.ed[kind][n] || {}).own);
    return names.map(n => html`<div class="item pending"><span class="lead"><h3>${n}</h3></span>
      ${badge(T.ed_pending, true)} ${this.editButton(kind === "models" ? "model" : "agent", n)}</div>`);
  }
}

// restartMira asks Mira to start anew and waits until she answers again.
async function restartMira() {
  const view = document.querySelector("#view");
  try { await api("POST", "/v1/ui/restart"); } catch (e) { /* she may be gone already */ }
  if (view) view.insertAdjacentHTML("afterbegin", `<section class="card"><div class="check-wait"><span class="spinner"></span><span>${T.ed_restarting}</span></div></section>`);
  await new Promise(r => setTimeout(r, 2500));
  for (let i = 0; i < 40; i++) {
    try { await api("GET", "/v1/ui/edit"); break; } catch (e) { await new Promise(r => setTimeout(r, 1500)); }
  }
  location.reload();
}

class MiraSkills extends EditPage {
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    const of = s => this.data.skills.filter(x => x.state === s);
    return html`${this.bar()}
      <section class="card"><h2>${T.cat_active}</h2><p class="hint">${T.cat_skills_hint}</p>
        ${of("active").map(s => this.skill(s))}</section>
      ${of("problem").length ? html`<section class="card"><h2>${T.cat_problem}</h2>
        ${of("problem").map(s => html`<div class="item"><h3>${s.name}</h3><p class="msg err">${s.problem}</p>
          <div class="actions">${this.editButton("skill", s.name)}</div></div>`)}</section>` : nothing}
      ${of("available").length ? html`<section class="card"><h2>${T.cat_available}</h2><p class="hint">${T.cat_available_hint}</p>
        <table>${of("available").map(s => html`<tr><th><code>${s.name}</code></th><td>${s.description}</td>
          <td class="nowrap">${this.editButton("skill", s.name, T.ed_switch_on)}</td></tr>`)}</table></section>` : nothing}`;
  }
  skill(s) {
    const t = s.target, m = s.mcp;
    return html`<details class="item">
      <summary>
        <span class="lead"><h3>${s.name}</h3></span>
        <span class="short clip">${s.description}</span>
        <span>${(s.kinds || []).map(k => badge(T["kind_" + k] || k))}${m && !m.connected ? badge(T.cat_mcp_down, true) : nothing}</span>
      </summary>
      <table class="facts">
        ${row(T.cat_description, s.description)}
        ${(s.settings || []).length ? row(T.cat_settings, html`${s.settings.map(x => html`<div>
          <code>${x.name}</code>: ${x.secret ? (x.set ? T.cat_secret_set : T.cat_secret_unset) : (x.set ? x.value : html`<span class="hint">${T.cat_unset}</span>`)}
          ${x.description ? html`<div class="hint">${x.description}</div>` : nothing}</div>`)}`) : nothing}
        ${(s.schedules || []).length ? row(T.cat_schedules, html`${s.schedules.map(x => html`<div>
          ${cronWords(x.cron)}${x.per_person ? html` · ${T.cat_per_person}` : nothing}${x.next ? html` · ${T.cat_next} ${x.next}` : nothing}</div>`)}`) : nothing}
        ${t ? row(T.cat_target, html`
          ${t.plan ? html`<div>${T.cat_plan}${t.agent ? html` · ${T.cat_written_by} <code>${t.agent}</code>` : nothing}</div>` : nothing}
          ${(t.fields || []).map(f => html`<div><code>${f.name}</code>${f.optional ? html` <span class="hint">(${T.cat_optional})</span>` : nothing}:
            ${f.description}${f.check ? html` · ${T.cat_check} <code>${f.check}</code>` : nothing}${f.agent ? html` · ${T.cat_filled_by} <code>${f.agent}</code>` : nothing}</div>`)}
          ${t.finish ? html`<div>${T.cat_finish}: <code>${t.finish}</code>${t.confirm ? html` · ${T.cat_confirm}` : nothing}</div>` : nothing}
          <div class="hint">${T.cat_timeout} ${t.timeout}</div>`) : nothing}
        ${m ? row(T.cat_mcp, html`
          <div>${m.connected ? badge(T.cat_connected) : badge(T.cat_mcp_down, true)} ${m.transport || "http"} <code>${m.address}</code></div>
          ${m.error ? html`<div class="msg err">${m.error}</div>` : nothing}
          ${(m.tools || []).length ? html`<div>${T.cat_tools}: ${code(m.tools)}</div>` : html`<div>${T.cat_all_tools}</div>`}
          ${(m.missing || []).length ? html`<div class="warn">${T.cat_missing}: ${code(m.missing)}</div>` : nothing}
          <div class="hint">${m.conversation ? T.cat_mcp_conversation : T.cat_mcp_agents_only}${m.web_content ? html` · ${T.cat_web_content}` : nothing}</div>`) : nothing}
        ${(s.agents || []).length ? row(T.cat_agents, html`${s.agents.map((a, i) => html`${i ? ", " : ""}<a href="#agents" @click=${e => { e.preventDefault(); go("agents"); }}><code>${a}</code></a>`)}`) : nothing}
      </table>
      ${s.guide ? html`<details class="profile"><summary>${T.cat_guide}</summary><pre>${s.guide}</pre></details>` : nothing}
      <div class="actions">${this.editButton("skill", s.name)}</div>
    </details>`;
  }
}
customElements.define("mira-skills", MiraSkills);

// ---- Agents ---------------------------------------------------------------
// The agents' descriptions, above the runs in the tab "Agenten".
class MiraAgentDefs extends EditPage {
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    const of = o => this.data.agents.filter(a => a.origin === o);
    return html`${this.bar()}<section class="card"><h2>${T.cat_agents_title}
        ${this.canEdit ? html`<button class="secondary" @click=${() => openEditor("agent", "")}>${T.ed_new_agent}</button>` : nothing}</h2>
      <p class="hint">${T.cat_agents_hint}</p>
      ${["own", "config", "skill"].filter(o => of(o).length).map(o => html`
        <h3 class="subhead">${T["origin_" + o]}</h3>${of(o).map(a => this.agent(a))}`)}
      ${this.pending("agents", this.data.agents.map(a => a.name))}</section>`;
  }
  agent(a) {
    const own = a.origin === "own";
    const desc = own ? (T.own_agents[a.name] || "") : a.description;
    const called = [
      ...(own ? [T.own_called[a.name] || ""] : []),
      a.conversation ? (a.skill ? T.cat_called_conversation_skill(a.skill) : T.cat_called_conversation) : "",
      ...(a.threads || []).map(t => T.cat_called_thread(t)),
      ...(a.called_by || []).map(n => T.cat_called_agent(n)),
      own ? "" : T.cat_called_ha,
    ].filter(Boolean);
    return html`<details class="item">
      <summary>
        <span class="lead"><h3>${a.name}</h3></span>
        <span class="short clip">${desc}</span>
        <span>${a.model ? badge(a.model) : nothing}${a.model_privacy ? badge(T["privacy_" + a.model_privacy] || a.model_privacy) : nothing}${a.problem ? badge(T.cat_not_started, true) : nothing}</span>
      </summary>
      ${a.problem ? html`<p class="msg err">${a.problem}</p>` : nothing}
      <table class="facts">
        ${row(T.cat_description, desc)}
        ${row(T.cat_skill, a.skill)}
        ${row(T.cat_model, a.model ? html`<a href="#models" @click=${e => { e.preventDefault(); go("models"); }}><code>${a.model}</code></a>${a.model_privacy ? ` (${T["privacy_" + a.model_privacy] || a.model_privacy})` : ""}${a.fallback ? html` · ${T.cat_fallback} <code>${a.fallback}</code>` : nothing}${a.privacy === "local" ? html` · ${T.cat_must_be_local}` : nothing}` : "")}
        ${row(T.cat_context, a.context ? (T["context_" + a.context] || a.context) : "")}
        ${row(T.cat_tools, a.tools ? (a.tools === "list" ? code(a.tool_names) : (T["tools_" + a.tools] || a.tools)) : "")}
        ${row(T.cat_rights, (a.rights || []).filter(r => r !== "none").map(r => T["right_" + r] || r).join(", "))}
        ${row(T.cat_rounds, a.max_rounds ? String(a.max_rounds) : "")}
        ${row(T.cat_called, called.length ? html`${called.map(c => html`<div>${c}</div>`)}` : "")}
      </table>
      <div class="actions">${a.origin === "skill" ? html`<span class="hint">${T.ed_skill_agent}</span>` : this.editButton("agent", a.name)}</div>
    </details>`;
  }
}
customElements.define("mira-agent-defs", MiraAgentDefs);

// ---- Models ---------------------------------------------------------------
// The language models: how they are set, who takes them, how their calls
// went; "check now" asks one.
// checkWait is how long the page waits for a check; the agent gives up
// after 30 seconds and says so.
const checkWait = 35;

class MiraModels extends EditPage {
  static properties = { ...EditPage.properties, checking: { state: true } };
  disconnectedCallback() { super.disconnectedCallback(); clearInterval(this.ticker); }
  // check opens the dialog, sends the model a short request and shows how
  // it went; a check that takes too long ends with a timeout.
  async check(name) {
    clearInterval(this.ticker);
    const began = Date.now();
    this.checking = { name, state: "wait", seconds: 0 };
    await this.updateComplete;
    const dlg = this.querySelector("dialog.mira-dialog");
    if (!dlg.open) dlg.showModal();
    this.ticker = setInterval(() => {
      if (this.checking && this.checking.state === "wait") this.checking = { ...this.checking, seconds: Math.round((Date.now() - began) / 1000) };
    }, 1000);
    const timeout = new Promise(resolve => setTimeout(() => resolve({ timeout: true }), checkWait * 1000));
    let r;
    try {
      r = await Promise.race([api("POST", "/v1/ui/models/" + encodeURIComponent(name) + "/check"), timeout]);
    } catch (e) { r = { ok: false, error: e.message }; }
    clearInterval(this.ticker);
    if (!this.checking || this.checking.name !== name) return;
    if (r.timeout) r = { ok: false, error: T.mod_check_timeout(checkWait) };
    this.checking = { name, state: r.ok ? "ok" : "error", result: r };
    // The model's last answer or error changed with the check.
    catalog(true).then(d => { this.data = d; }, () => {});
  }
  closeCheck() {
    clearInterval(this.ticker);
    this.checking = null;
    const dlg = this.querySelector("dialog.mira-dialog");
    if (dlg && dlg.open) dlg.close();
  }
  dialog() {
    const c = this.checking;
    const r = c && c.result;
    const short = s => (s.length > 300 ? s.slice(0, 300) + " …" : s);
    return html`<dialog class="mira-dialog" @close=${() => { this.checking = null; clearInterval(this.ticker); }}>
      <form method="dialog">
        ${c ? html`
          <h3>${T.mod_check_title(c.name)}</h3>
          ${c.state === "wait" ? html`<div class="check-wait"><span class="spinner"></span><span>${T.mod_check_wait(c.seconds)}</span></div>` : nothing}
          ${c.state === "ok" ? html`<div class="check-result">
            <div class="big ok">${T.mod_check_success}</div>
            <p class="hint">${T.mod_check_after(r.ms)}</p>
            ${r.answer ? html`<p>${T.mod_check_said}: „${short(r.answer)}“</p>` : nothing}</div>` : nothing}
          ${c.state === "error" ? html`<div class="check-result">
            <div class="big err">${T.mod_check_error}</div>
            <pre class="runpre">${short(r.error || "")}</pre>
            ${(r.error || "").length > 300 ? html`<details class="profile"><summary>${T.mod_check_full}</summary><pre class="runpre">${r.error}</pre></details>` : nothing}</div>` : nothing}
          <div class="actions"><button class="primary" value="close" @click=${e => { e.preventDefault(); this.closeCheck(); }}>${T.mod_check_close}</button></div>` : nothing}
      </form>
    </dialog>`;
  }
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    return html`${this.bar()}<section class="card"><h2>${T.models_title}
        ${this.canEdit ? html`<button class="secondary" @click=${() => openEditor("model", "")}>${T.ed_new_model}</button>` : nothing}</h2>
      <p class="hint">${T.models_hint}</p>
      ${(this.data.models || []).map(m => this.model(m))}
      ${this.pending("models", (this.data.models || []).map(m => m.name))}</section>${this.dialog()}`;
  }
  state(m) {
    if (m.last_failed && (!m.last_ok || m.last_failed > m.last_ok)) return badge(T.mod_failed, true);
    return m.last_ok ? badge(T.mod_ok) : badge(T.mod_unknown);
  }
  model(m) {
    const settings = [
      m.context_tokens ? `${T.mod_context} ${m.context_tokens} ${T.mod_tokens}` : "",
      m.reasoning_effort ? `${T.mod_reasoning} ${m.reasoning_effort}` : "",
      m.temperature != null ? `${T.mod_temperature} ${m.temperature}` : "",
      m.max_tool_rounds ? `${m.max_tool_rounds} ${T.mod_rounds}` : "",
      m.timeout ? `${T.mod_timeout} ${m.timeout}` : "",
      m.cache_prompt ? T.mod_cache : "",
      m.warmup ? T.mod_warmup : "",
    ].filter(Boolean).join(" · ");
    const agentLink = n => html`<a href="#agents" @click=${e => { e.preventDefault(); go("agents"); }}><code>${n}</code></a>`;
    return html`<details class="item">
      <summary>
        <span class="lead"><h3>${m.name}</h3></span>
        <span class="short clip">${m.model}</span>
        <span>${badge(T["privacy_" + m.privacy] || m.privacy)}${this.state(m)}</span>
      </summary>
      <table class="facts">
        ${row(T.mod_endpoint, html`<code>${m.endpoint}</code>${m.from_ha ? html` <span class="hint">(${T.mod_from_ha})</span>` : nothing}`)}
        ${row(T.mod_model, html`<code>${m.model}</code>`)}
        ${row(T.mod_token, m.token_set ? T.mod_token_set : T.mod_token_unset)}
        ${row(T.mod_privacy, T["privacy_" + m.privacy] || m.privacy)}
        ${row(T.mod_parallel, String(m.parallel || ""))}
        ${row(T.mod_fallback, m.fallback ? html`<code>${m.fallback}</code>` : "")}
        ${row(T.mod_settings, settings)}
        ${row(T.mod_used_by, (m.used_by || []).length ? html`${m.used_by.map((n, i) => html`${i ? ", " : ""}${agentLink(n)}`)}` : html`<span class="hint">${T.mod_nobody}</span>`)}
        ${row(T.mod_fallback_for, (m.fallback_for || []).length ? code(m.fallback_for) : "")}
        ${row(T.mod_state, html`
          ${m.last_ok ? html`<div>${T.mod_last_ok} ${m.last_ok}</div>` : nothing}
          ${m.last_failed ? html`<div class="warn">${T.mod_last_failed} ${m.last_failed}: ${m.problem}</div>` : nothing}
          ${!m.last_ok && !m.last_failed ? html`<div class="hint">${T.mod_unknown}</div>` : nothing}
          ${m.runs ? html`<div class="hint">${T.mod_runs(m.runs, m.tokens)}</div>` : nothing}`)}
      </table>
      <div class="actions">
        <button class="secondary" @click=${() => this.check(m.name)}>${T.mod_check}</button>
        ${this.editButton("model", m.name)}
      </div>
    </details>`;
  }
}
customElements.define("mira-models", MiraModels);

const opts = (list, current) => list.map(([v, label]) => html`<option value=${v} ?selected=${v === (current ?? "")}>${label}</option>`);
const RIGHTS = ["read_house", "act_on_house", "remember", "messages", "web"];

class MiraEditor extends Light {
  static properties = { e: { state: true } };
  open(kind, name) {
    edits().then(s => {
      if (!s) return;
      const isNew = !name;
      let form;
      if (kind === "model") form = { name: name || "", ...(name ? s.models[name] : { privacy: "cloud" }) };
      if (kind === "agent") {
        const a = name ? s.agents[name] : {};
        form = { name: name || "", ...a, tools_text: (a.tools || []).join(", "),
          tools_kind: (a.tools || []).length ? "list" : (a.tools_word || "none"), rights: a.rights || [] };
      }
      if (kind === "skill") {
        const sk = s.skills[name] || {};
        form = { ...sk, settings: { ...(sk.settings || {}) }, enabled: name && !sk.enabled ? true : sk.enabled };
        this.original = { description: sk.description || "", guide: sk.guide || "", agents: sk.agents || "", target: sk.target || "", mcp: sk.mcp || "" };
      }
      this.e = { kind, name, isNew, form, state: s, error: "", saving: false, mcp: null };
      this.updateComplete.then(() => { const d = this.querySelector("dialog"); if (d && !d.open) d.showModal(); });
    });
  }
  close() { const d = this.querySelector("dialog"); if (d && d.open) d.close(); this.e = null; }
  set(key, value) { this.e = { ...this.e, form: { ...this.e.form, [key]: value } }; }
  setSetting(key, value) { this.set("settings", { ...this.e.form.settings, [key]: value }); }
  async save() {
    const { kind, name, form } = this.e;
    let path, body;
    if (kind === "model") {
      const target = name || form.name.trim();
      path = "/v1/ui/edit/models/" + encodeURIComponent(target);
      body = { name: form.name.trim(), endpoint: form.endpoint || "", model: form.model || "", privacy: form.privacy || "",
        parallel: +form.parallel || 0, fallback: form.fallback || "", context_tokens: +form.context_tokens || 0,
        reasoning_effort: form.reasoning_effort || "", max_tool_rounds: +form.max_tool_rounds || 0, timeout: form.timeout || "",
        token: form.token || "", token_clear: !!form.token_clear };
      if (String(form.temperature ?? "").trim() !== "") body.temperature = parseFloat(String(form.temperature).replace(",", "."));
    }
    if (kind === "agent") {
      path = "/v1/ui/edit/agents/" + encodeURIComponent(name || form.name.trim());
      body = { model: form.model || "", privacy: form.privacy || "" };
      if (!form.own) Object.assign(body, { description: form.description || "", context: form.context || "",
        tools: form.tools_kind === "list" ? form.tools_text.split(",").map(x => x.trim()).filter(Boolean) : [],
        tools_word: form.tools_kind === "read_only" ? "read_only" : "", rights: form.rights || [],
        conversation: !!form.conversation, max_rounds: +form.max_rounds || 0, instructions: form.instructions || "",
        input: form.input || "", output: form.output || "" });
    }
    if (kind === "skill") {
      path = "/v1/ui/edit/skills/" + encodeURIComponent(name);
      const parts = form.own ? Object.keys(this.original).filter(k => (form[k] || "") !== this.original[k]) : [];
      body = { enabled: !!form.enabled, settings: form.settings || {}, parts,
        description: form.description || "", guide: form.guide || "", agents: form.agents || "", target: form.target || "", mcp: form.mcp || "" };
    }
    this.e = { ...this.e, saving: true, error: "" };
    try {
      await api("PUT", path, body);
      this.close();
      edited();
    } catch (err) { this.e = { ...this.e, saving: false, error: err.message }; }
  }
  async remove() {
    const { kind, name } = this.e;
    if (!confirm(T.ed_delete_confirm(name))) return;
    try {
      await api("DELETE", `/v1/ui/edit/${kind}s/` + encodeURIComponent(name));
      this.close();
      edited();
    } catch (err) { this.e = { ...this.e, error: err.message }; }
  }
  async checkMCP() {
    this.e = { ...this.e, mcp: { wait: true } };
    try {
      const r = await api("POST", `/v1/ui/edit/skills/${encodeURIComponent(this.e.name)}/mcp-check`, { settings: this.e.form.settings });
      this.e = { ...this.e, mcp: r };
    } catch (err) { this.e = { ...this.e, mcp: { ok: false, error: err.message } }; }
  }
  field(label, key, attrs = {}) {
    const f = this.e.form;
    return html`<label>${label}</label>
      <input type=${attrs.type || "text"} .value=${f[key] ?? ""} placeholder=${attrs.placeholder || ""}
        ?disabled=${!!attrs.disabled} @input=${ev => this.set(key, ev.target.value)}>`;
  }
  area(label, key, rows = 4, hint = "") {
    return html`<label class="wide-label">${label}${hint ? html` <span class="hint">${hint}</span>` : nothing}</label>
      <textarea class="wide code" rows=${rows} .value=${this.e.form[key] || ""} @input=${ev => this.set(key, ev.target.value)}></textarea>`;
  }
  modelForm() {
    const { form: f, state: s, name } = this.e;
    const locked = f.from_ha;
    const others = Object.keys(s.models).filter(n => n !== (name || f.name));
    return html`
      ${locked ? html`<p class="hint">${T.ed_main_ha}</p>` : nothing}
      <div class="grid">
        ${name === "main" ? nothing : this.field(T.ed_name, "name", { placeholder: "smart" })}
        ${this.field(T.mod_endpoint, "endpoint", { placeholder: "https://…/v1", disabled: locked })}
        ${this.field(T.mod_model, "model", { disabled: locked })}
        <label>${T.mod_privacy}</label><select ?disabled=${locked} @change=${ev => this.set("privacy", ev.target.value)}>
          ${opts([["local", T.privacy_local], ["cloud", T.privacy_cloud]], f.privacy)}</select>
        <label>${T.mod_token}</label><input type="password" autocomplete="new-password" ?disabled=${locked} .value=${f.token || ""}
          placeholder=${f.token_set ? T.ed_token_keep : ""} @input=${ev => this.set("token", ev.target.value)}>
        ${f.token_set && !locked ? html`<span></span><label class="checkline"><input type="checkbox" .checked=${!!f.token_clear}
          @change=${ev => this.set("token_clear", ev.target.checked)}> ${T.ed_token_clear}</label>` : nothing}
        ${this.field(T.mod_parallel, "parallel", { type: "number", disabled: locked })}
        <label>${T.mod_fallback}</label><select ?disabled=${locked} @change=${ev => this.set("fallback", ev.target.value)}>
          ${opts([["", "–"], ...others.map(n => [n, n])], f.fallback)}</select>
        ${this.field(T.ed_context_tokens, "context_tokens", { type: "number", disabled: locked })}
        <label>${T.mod_reasoning}</label><select ?disabled=${locked} @change=${ev => this.set("reasoning_effort", ev.target.value)}>
          ${opts([["", "–"], ["none", "none"], ["low", "low"], ["medium", "medium"], ["high", "high"]], f.reasoning_effort)}</select>
        ${this.field(T.mod_temperature, "temperature", { disabled: locked, placeholder: "0.7" })}
        ${this.field(T.ed_tool_rounds, "max_tool_rounds", { type: "number", disabled: locked })}
        ${this.field(T.mod_timeout, "timeout", { disabled: locked, placeholder: "60s" })}
      </div>
      <p class="hint">${s.encrypted ? T.ed_secrets_encrypted : T.ed_secrets_plain}</p>`;
  }
  agentForm() {
    const { form: f, state: s, isNew } = this.e;
    const models = [["", T.ed_model_default], ...Object.keys(s.models).map(n => [n, n])];
    const common = html`
      <label>${T.cat_model}</label><select @change=${ev => this.set("model", ev.target.value)}>${opts(models, f.model)}</select>
      <label>${T.ed_privacy}</label><select @change=${ev => this.set("privacy", ev.target.value)}>
        ${opts([["", T.ed_privacy_any], ["local", T.ed_privacy_local]], f.privacy)}</select>`;
    if (f.own) return html`<p class="hint">${T.ed_own_agent}</p><div class="grid">${common}</div>`;
    return html`<div class="grid">
        ${isNew ? this.field(T.ed_name, "name", { placeholder: "recherche" }) : nothing}
        ${this.field(T.cat_description, "description")}
        ${common}
        <label>${T.cat_context}</label><select @change=${ev => this.set("context", ev.target.value)}>
          ${opts([["", T.context_isolated || "isolated"], ["background", T.context_background || "background"]], f.context)}</select>
        <label>${T.cat_tools}</label><select @change=${ev => this.set("tools_kind", ev.target.value)}>
          ${opts([["none", T.tools_none || "none"], ["read_only", T.tools_read_only || "read_only"], ["list", T.ed_tools_list]], f.tools_kind)}</select>
        ${f.tools_kind === "list" ? html`<span></span><input .value=${f.tools_text || ""} placeholder="web_page, web_search"
          @input=${ev => this.set("tools_text", ev.target.value)}>` : nothing}
        <label>${T.cat_rights}</label><div>${RIGHTS.map(r => html`<label class="checkline"><input type="checkbox"
          .checked=${(f.rights || []).includes(r)} @change=${ev => this.set("rights", ev.target.checked ? [...(f.rights || []), r] : (f.rights || []).filter(x => x !== r))}>
          ${T["right_" + r] || r}</label> `)}</div>
        <label>${T.ed_conversation}</label><input type="checkbox" .checked=${!!f.conversation} @change=${ev => this.set("conversation", ev.target.checked)}>
        ${this.field(T.cat_rounds, "max_rounds", { type: "number", placeholder: "8" })}
      </div>
      ${this.area(T.ed_instructions, "instructions", 6)}
      ${this.area(T.ed_input, "input", 3, T.ed_yaml)}
      ${this.area(T.ed_output, "output", 3, T.ed_yaml)}
      ${(f.used_by || []).length ? html`<p class="hint">${T.ed_used_by}: ${f.used_by.join(", ")}</p>` : nothing}`;
  }
  skillForm() {
    const { form: f, name } = this.e;
    const cat = ((catalogData || {}).skills || []).find(x => x.name === name) || {};
    return html`
      <label class="checkline"><input type="checkbox" .checked=${!!f.enabled} @change=${ev => this.set("enabled", ev.target.checked)}> ${T.ed_enabled}</label>
      ${!f.enabled ? html`<p class="hint">${T.ed_disable_hint}</p>` : nothing}
      ${(f.setting_defs || []).length ? html`<h4>${T.cat_settings}</h4><div class="grid">${f.setting_defs.map(d => html`
        <label title=${d.description || ""}>${d.name}${d.required ? " *" : ""}</label>
        <div>${d.secret ? html`<input type="password" autocomplete="new-password" .value=${f.settings[d.name] || ""}
            placeholder=${d.secret_set ? T.ed_token_keep : ""} @input=${ev => this.setSetting(d.name, ev.target.value)}>`
          : html`<input .value=${f.settings[d.name] || ""} @input=${ev => this.setSetting(d.name, ev.target.value)}>`}
          ${d.description ? html`<div class="hint">${d.description}</div>` : nothing}</div>`)}</div>` : nothing}
      ${cat.mcp || f.mcp ? html`<div class="actions left"><button class="secondary" @click=${e => { e.preventDefault(); this.checkMCP(); }}>${T.ed_mcp_check}</button>
        ${this.mcpResult()}</div>` : nothing}
      ${f.own ? html`
        <h4>${T.ed_skill_file}</h4>
        ${f.read_only ? html`<p class="msg warn-box">${f.read_only}</p>` : html`
          <div class="grid">${this.field(T.cat_description, "description")}</div>
          ${this.area(T.cat_guide, "guide", 10)}
          ${this.area(T.cat_agents, "agents", 5, T.ed_yaml)}
          ${this.area(T.cat_target, "target", 5, T.ed_yaml)}
          ${this.area(T.cat_mcp, "mcp", 4, T.ed_yaml)}`}`
        : html`<p class="hint">${T.ed_bundled}</p>`}
      <p class="hint">${this.e.state.encrypted ? T.ed_secrets_encrypted : T.ed_secrets_plain}</p>`;
  }
  mcpResult() {
    const m = this.e.mcp;
    if (!m) return nothing;
    if (m.wait) return html`<span class="check-wait"><span class="spinner"></span></span>`;
    return m.ok ? html`<span class="ok">${T.ed_mcp_ok(m.tools, m.ms)}${(m.missing || []).length ? html` <span class="warn">${T.cat_missing}: ${m.missing.join(", ")}</span>` : nothing}</span>`
      : html`<span class="warn">${m.error}</span>`;
  }
  render() {
    const e = this.e;
    const title = !e ? "" : e.isNew ? T["ed_new_" + e.kind] : T.ed_title(e.name);
    const canDelete = e && !e.isNew && ((e.kind === "model" && e.name !== "main" && e.form.in_file) || (e.kind === "agent" && !e.form.own));
    return html`<dialog class="mira-dialog wide" @close=${() => { this.e = null; }}>
      <form method="dialog" @submit=${ev => { ev.preventDefault(); this.save(); }}>
        ${e ? html`<h3>${title}</h3>
          ${e.kind === "model" ? this.modelForm() : e.kind === "agent" ? this.agentForm() : this.skillForm()}
          ${e.error ? html`<p class="msg err">${e.error}</p>` : nothing}
          <div class="actions">
            ${canDelete ? html`<button class="danger" @click=${ev => { ev.preventDefault(); this.remove(); }}>${T.ed_delete}</button>` : nothing}
            <span class="spacer"></span>
            <button class="secondary" @click=${ev => { ev.preventDefault(); this.close(); }}>${T.ed_cancel}</button>
            <button class="primary" type="submit" ?disabled=${e.saving || (e.kind === "model" && e.form.from_ha)}>${T.ed_save}</button>
          </div>` : nothing}
      </form>
    </dialog>`;
  }
}
customElements.define("mira-editor", MiraEditor);
// One editor for the page, opened by the pages' buttons.
let catalogData = null;
const editorEl = document.createElement("mira-editor");
document.body.appendChild(editorEl);
function openEditor(kind, name) {
  catalog().then(d => { catalogData = d; editorEl.open(kind, name); });
}
