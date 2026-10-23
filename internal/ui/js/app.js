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

class MiraSkills extends Light {
  static properties = { data: { state: true }, error: { state: true } };
  connectedCallback() {
    super.connectedCallback();
    catalog(true).then(d => { this.data = d; }, e => { this.error = e.message; });
  }
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    const of = s => this.data.skills.filter(x => x.state === s);
    return html`
      <section class="card"><h2>${T.cat_active}</h2><p class="hint">${T.cat_skills_hint}</p>
        ${of("active").map(s => this.skill(s))}</section>
      ${of("problem").length ? html`<section class="card"><h2>${T.cat_problem}</h2>
        ${of("problem").map(s => html`<div class="item"><h3>${s.name}</h3><p class="msg err">${s.problem}</p></div>`)}</section>` : nothing}
      ${of("available").length ? html`<section class="card"><h2>${T.cat_available}</h2><p class="hint">${T.cat_available_hint}</p>
        <table>${of("available").map(s => html`<tr><th><code>${s.name}</code></th><td>${s.description}</td></tr>`)}</table></section>` : nothing}`;
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
    </details>`;
  }
}
customElements.define("mira-skills", MiraSkills);

// ---- Agents ---------------------------------------------------------------
// The agents' descriptions, above the runs in the tab "Agenten".
class MiraAgentDefs extends Light {
  static properties = { data: { state: true }, error: { state: true } };
  connectedCallback() {
    super.connectedCallback();
    catalog().then(d => { this.data = d; }, e => { this.error = e.message; });
  }
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    const of = o => this.data.agents.filter(a => a.origin === o);
    return html`<section class="card"><h2>${T.cat_agents_title}</h2><p class="hint">${T.cat_agents_hint}</p>
      ${["own", "config", "skill"].filter(o => of(o).length).map(o => html`
        <h3 class="subhead">${T["origin_" + o]}</h3>${of(o).map(a => this.agent(a))}`)}</section>`;
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
        ${row(T.cat_model, a.model ? html`<code>${a.model}</code>${a.model_privacy ? ` (${T["privacy_" + a.model_privacy] || a.model_privacy})` : ""}${a.fallback ? html` · ${T.cat_fallback} <code>${a.fallback}</code>` : nothing}${a.privacy === "local" ? html` · ${T.cat_must_be_local}` : nothing}` : "")}
        ${row(T.cat_context, a.context ? (T["context_" + a.context] || a.context) : "")}
        ${row(T.cat_tools, a.tools ? (a.tools === "list" ? code(a.tool_names) : (T["tools_" + a.tools] || a.tools)) : "")}
        ${row(T.cat_rights, (a.rights || []).filter(r => r !== "none").map(r => T["right_" + r] || r).join(", "))}
        ${row(T.cat_rounds, a.max_rounds ? String(a.max_rounds) : "")}
        ${row(T.cat_called, called.length ? html`${called.map(c => html`<div>${c}</div>`)}` : "")}
      </table>
    </details>`;
  }
}
customElements.define("mira-agent-defs", MiraAgentDefs);
