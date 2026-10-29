// Mira's page, the parts built on Lit (docs/spec/uebersicht.md): the start
// page with its cards, the navigation inside a group, the skills and the
// agents at a glance. The older views are still the page's script; they move
// over one by one. The components render into the page itself (no shadow
// DOM), so the page's styles apply, and they use the page's helpers: T (the
// texts), TABS, GROUPS, go, api, isAdmin, tab.
import { LitElement, html, svg, nothing } from "./lit-core.min.js";

class Light extends LitElement {
  createRenderRoot() { return this; }
}

// groupOf is the group a tab belongs to.
const groupOf = id => GROUPS.find(g => g.tabs.includes(id));
// tabsOf are the tabs of a group this visitor may see.
const tabsOf = g => g.tabs.filter(id => TABS.includes(id));

// ---- Navigation -----------------------------------------------------------
// On the start page nothing; in a group the way back, the group's head and
// its tabs.
// mePartsOf are the parts of the card "Ich", as its links and its tabs.
const mePartsOf = () => [["me-settings", T.card_settings], ["voice", T.voice_title], ["me-messages", T.me_messages], ["me-memory", T.me_memory]];

class MiraNav extends Light {
  static properties = { tab: {} };
  render() {
    // The start page has its own head (MiraHome); the bar is hidden there.
    if (!this.tab || this.tab === "home") return html`<span class="group-title">${T.tab_home}</span>`;
    // The card "Ich" is a group of its own: the person's name and its parts,
    // which jump to their place on the card.
    if (this.tab === "me") return this.head("person", (me && (me.name || me.person)) || T.me_title, T.subtitle_me,
      mePartsOf().map(([id, label]) => html`
        <button @click=${() => { const el = document.getElementById(id); if (el) el.scrollIntoView({ block: "start" }); }}>${label}</button>`));
    // A person's card belongs to the people.
    const group = groupOf(this.tab === "person" ? "persons" : this.tab);
    if (!group) return this.head("person", T.tab_home, "", []);
    const tabs = tabsOf(group);
    return this.head(group.id, T["group_" + group.id], T["subtitle_" + group.id], tabs.length > 1 ? tabs.map(id => html`
      <button class=${id === this.tab || (id === "persons" && this.tab === "person") ? "active" : ""} @click=${() => go(id)}>${T["tab_" + id]}</button>`) : []);
  }
  // head is the way back, the group's icon, name and subtitle, and its pages
  // as a switch.
  head(icon, title, subtitle, buttons) {
    return html`
      <button class="back" @click=${() => go("home")}>← ${T.tab_home}</button>
      <div class="page-head"><span class="category-icon">${homeIcon(icon)}</span>
        <div><h1>${title}</h1>${subtitle ? html`<p>${subtitle}</p>` : nothing}</div></div>
      ${buttons.length ? html`<div class="tabs">${buttons}</div>` : nothing}`;
  }
}
customElements.define("mira-nav", MiraNav);

// homeIcon is the line icon of a group card, or of the card "Ich".
const homeIcon = kind => svg`<svg class="mira-icon" viewBox="0 0 24 24" aria-hidden="true">${
  kind === "daily" ? svg`<path d="m3 10 9-7 9 7v10H3Z"/><path d="M9 20v-7h6v7"/>` :
  kind === "setup" ? svg`<path d="M3 6h18M3 12h18M3 18h18"/><circle cx="8" cy="6" r="2"/><circle cx="16" cy="12" r="2"/><circle cx="9" cy="18" r="2"/>` :
  kind === "abilities" ? svg`<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z"/>` :
  kind === "person" ? svg`<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>` :
  svg`<path d="M12 5c-3-2-6-2-9-1v15c3-1 6-1 9 1 3-2 6-2 9-1V4c-3-1-6-1-9 1ZM12 5v15"/>`
}</svg>`;

// avatar is the picture of whoever is logged in, as Home Assistant's sidebar
// shows it: their person's entity_picture (Home Assistant serves it itself),
// else their initials.
function avatar(name) {
  const h = hass(), id = h && h.user && h.user.id;
  const person = id ? Object.values(h.states || {}).find(s => s.entity_id.startsWith("person.") && s.attributes.user_id === id) : null;
  const picture = person && person.attributes.entity_picture;
  if (picture) return html`<img src=${picture} alt="">`;
  const initials = String(name || "").trim().split(/\s+/).slice(0, 2).map(w => w[0] || "").join("").toUpperCase();
  return html`<span class="initials">${initials}</span>`;
}

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
      // Who looks: their own card, with their voice, their settings and how
      // much Mira keeps for them.
      ["me", async () => {
        const m = await api("GET", "/v1/ui/me");
        if (!m.person) return m;
        const tries = [];
        if (m.voice) tries.push(api("GET", `/v1/ui/voice/${encodeURIComponent(m.person)}`).then(v => { m.samples = v.samples || 0; }));
        if (embedded) tries.push(haApi("GET", `${HA_BASE}/settings/me`).then(d => { m.settings = d.settings || {}; }));
        tries.push(api("GET", "/v1/ui/memory").then(d => {
          const mine = (d.memories || []).find(x => x.owner === m.person);
          m.memories = mine ? (mine.facts || []).length : 0;
        }));
        tries.push(api("GET", "/v1/ui/messages?open=1&mine=1").then(d => { m.messages = (d.items || []).length; }));
        await Promise.allSettled(tries);
        return m;
      }],
    ];
    // The house's messages for everyone, all of them for admins.
    tries.push(["messages", async () => {
      const r = await api("GET", "/v1/ui/messages?open=1");
      facts.phone = r.phone; // what waits on the answering machine, if anything
      return r.items || [];
    }]);
    if (isAdmin) {
      tries.push(
        ["tasks", async () => { const r = await api("GET", "/v1/ui/tasks"); return r.items || r.tasks || []; }],
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
        if (f.phone) out.push(f.phone.url
          ? html`<a href=${f.phone.url} target="_blank" rel="noopener">${T.phone_line(f.phone.messages, f.phone.missed)}</a>`
          : T.phone_line(f.phone.messages, f.phone.missed));
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
          const broken = (f.catalog.hooks || []).filter(h => h.problem).length;
          if (broken) out.push(html`<span class="warn">${T.home_hook_problems(broken)}</span>`);
        }
        break;
    }
    return out;
  }
  // me is the card of whoever looks, when Home Assistant links a person to
  // their login, as a bar above the groups: a click opens it.
  me() {
    const m = this.facts.me;
    if (!m || !m.person) return nothing;
    const parts = mePartsOf();
    return html`<section class="card home-person" role="button" tabindex="0"
      @click=${() => go("me")} @keydown=${e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go("me"); } }}>
      <span class="personal-icon">${avatar(m.name || m.person)}</span>
      <div><h2>${m.name || m.person}</h2><p class="home-pages">${parts.map(([id, label]) => this.link(label, () => goMe(id)))}</p></div>
      <div class="home-facts">
        ${m.memories !== undefined ? html`<p class="home-line">${T.home_memories(m.memories)}</p>` : nothing}
        ${m.messages !== undefined ? html`<p class="home-line">${T.home_mine_messages(m.messages)}</p>` : nothing}
        ${m.voice && m.samples !== undefined ? html`<p class="home-line">${m.samples ? T.home_voice_on(m.samples) : T.home_voice_off}</p>` : nothing}
      </div>
    </section>`;
  }
  // render is the page's head (logo, greeting), the card "Ich" as a bar, the
  // group cards with an icon each and a foot.
  render() {
    const m = this.facts.me;
    const name = m && m.person ? (m.name || m.person) : "";
    const open = g => go(tabsOf(g)[0]);
    return html`
      <div class="mira-brand">
        <div class="mira-brand-name"><img src=${url("/ui/icon.png")} alt="Mira">
          <div><div class="mira-eyebrow">${T.home_eyebrow}</div><h1>Mira Assist<span>.</span></h1></div></div>
      </div>
      <div class="mira-welcome"><h2>${this.greeting(name)}</h2><p>${this.today()}</p></div>
      ${this.me()}
      <div class="mira-label"><span>${T.home_label}</span><span>${T.home_label_side}</span></div>
      <div class="home-grid">${GROUPS.filter(g => tabsOf(g).length).map(g => html`
        <section class="card home-card" data-group=${g.id} role="button" tabindex="0"
          @click=${() => open(g)} @keydown=${e => { if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); open(g); } }}>
          <div class="home-card-top"><span class="category-icon">${homeIcon(g.id)}</span>
            <a href=${"#" + tabsOf(g)[0]} class="home-arrow" aria-label=${T["group_" + g.id]}
              @click=${e => { e.preventDefault(); e.stopPropagation(); open(g); }} @keydown=${e => e.stopPropagation()}>↗</a></div>
          <h2>${T["group_" + g.id]}</h2><p class="home-subtitle">${T["subtitle_" + g.id]}</p>
          ${g.id === "help" ? this.chapters() : html`<p class="home-pages">${tabsOf(g).map(id => this.link(T["tab_" + id], () => go(id)))}</p>`}
          <div class="home-facts">${g.id === "help" ? html`<p class="home-line">${T.home_help_line}</p>` : this.lines(g.id).map(l => html`<p class="home-line">${l}</p>`)}</div>
        </section>`)}</div>
      <footer class="mira-footer"><span>${T.home_footer}</span><span>${this.versions()}<a href="https://mira-assist.com" target="_blank" rel="noopener">Mira Assist</a></span></footer>`;
  }
  // versions are the service's and, inside Home Assistant, the integration's.
  versions() {
    const v = document.querySelector('meta[name="mira-versions"]').dataset;
    const parts = [`${T.footer_service} ${v.service}`];
    if (v.integration) parts.push(`${T.footer_integration} ${v.integration}`);
    return parts.join(" · ") + " · ";
  }
  // greeting greets by the time of day.
  greeting(name) {
    const h = new Date().getHours();
    const part = h >= 5 && h < 11 ? "morning" : h >= 11 && h < 17 ? "day" : h >= 17 && h < 22 ? "evening" : "night";
    return T["greet_" + part](name);
  }
  // today says in a line what is there for whoever looks: messages to them,
  // running plans, or that all is quiet; nothing until the facts are read.
  today() {
    const f = this.facts;
    if (!Object.keys(f).length) return "";
    const out = [], plans = (f.plans || []).filter(p => !p.finished);
    if (f.me && f.me.messages) out.push(T.greet_messages(f.me.messages));
    if (plans.length === 1) {
      const r = (plans[0].request || "").trim();
      out.push(T.greet_plan(r.length > 50 ? r.slice(0, 49) + "…" : r));
    } else if (plans.length) out.push(T.greet_plans(plans.length));
    return out.length ? out.join(" ") : T.greet_quiet;
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
        ${s.phonebook ? row(T.cat_phonebook, html`
          <div>${(s.phonebook.books || []).length ? s.phonebook.books.join(", ") : T.cat_phonebook_unread}${s.phonebook.read_at ? html` · ${T.cat_phonebook_read} ${s.phonebook.read_at}` : nothing}</div>
          ${s.phonebook.error ? html`<div class="warn">${s.phonebook.error}</div>` : nothing}
          ${s.phonebook.link ? html`<div><a href=${s.phonebook.link} target="_blank" rel="noopener">${T.cat_phonebook_open}</a> <span class="hint">${T.cat_phonebook_hint}</span></div>` : nothing}`) : nothing}
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
    return html`<details class="item agent">
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

// ---- Hooks ----------------------------------------------------------------
// The hooks that decide on every tool call (docs/spec/hooks.md): the
// household's own, the default ones, the system's; read only.
class MiraHooks extends EditPage {
  render() {
    if (this.error) return html`<p class="msg err">${this.error}</p>`;
    if (!this.data) return html`<section class="card"><p class="hint">${T.loading}</p></section>`;
    const of = level => (this.data.hooks || []).filter(h => h.level === level);
    const own = of("household");
    return html`${this.bar()}
      <section class="card"><h2>${T.hooks_household}</h2><p class="hint">${T.hooks_intro}</p><p class="hint">${T.hooks_household_hint}</p>
        ${own.length ? own.map(h => this.hook(h)) : html`<p class="hint">${T.hooks_none}</p>`}</section>
      <section class="card"><h2>${T.hooks_default}</h2><p class="hint">${T.hooks_default_hint}</p>
        ${of("default").map(h => this.hook(h))}</section>
      <section class="card"><h2>${T.hooks_system}</h2><p class="hint">${T.hooks_system_hint}</p>
        ${of("system").map(h => this.hook(h))}</section>`;
  }
  hook(h) {
    return html`<details class="item hook">
      <summary>
        <span class="lead"><h3>${h.name}</h3></span>
        <span class="short clip">${h.problem ? "" : h.description}</span>
        <span>${h.problem ? badge(T.hooks_not_loaded, true) : nothing}${h.off ? badge(T.hooks_off, true) : nothing}${h.replaced ? badge(T.hooks_replaced, true) : nothing}</span>
      </summary>
      ${h.problem ? html`<p class="msg err">${h.problem}</p>` : nothing}
      <pre class="code">${h.source}</pre>
    </details>`;
  }
}
customElements.define("mira-hooks", MiraHooks);

// providerOf names the provider whose address an endpoint is; "" for a
// server of one's own.
const providerOf = (providers, endpoint) =>
  ((providers || []).find(p => p.base_url === String(endpoint || "").replace(/\/+$/, "")) || {}).key || "";
const opts = (list, current) => list.map(([v, label]) => html`<option value=${v} ?selected=${v === (current ?? "")}>${label}</option>`);
const RIGHTS = ["read_house", "act_on_house", "remember", "messages", "web"];

class MiraEditor extends Light {
  static properties = { e: { state: true } };
  open(kind, name) {
    edits().then(s => {
      if (!s) return;
      const isNew = !name;
      let form;
      if (kind === "model") {
        const first = (s.providers || [])[0];
        form = { name: name || "", ...(name ? s.models[name] : first ? { endpoint: first.base_url, privacy: "cloud" } : { privacy: "local" }) };
        form.provider = providerOf(s.providers, form.endpoint);
      }
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
      if (kind === "model" && name && !form.from_ha) this.loadModels();
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
  // setProvider takes a provider's address and privacy; a server of one's
  // own gets the address field and counts as local.
  setProvider(key) {
    const p = (this.e.state.providers || []).find(p => p.key === key);
    const f = this.e.form;
    const endpoint = p ? p.base_url : providerOf(this.e.state.providers, f.endpoint) ? "" : f.endpoint;
    this.e = { ...this.e, models: null, check: null, form: { ...f, provider: key, endpoint, privacy: p ? "cloud" : "local" } };
    this.loadModels();
  }
  // loadModels asks the model's server which models it offers; without an
  // answer the model stays a text field.
  async loadModels() {
    const f = this.e.form;
    if (!f.endpoint) { this.e = { ...this.e, models: null }; return; }
    this.e = { ...this.e, models: { wait: true } };
    try {
      const r = await api("POST", "/v1/ui/edit/model-list", { name: this.e.name || "", endpoint: f.endpoint, token: f.token || "" });
      this.e = { ...this.e, models: r };
    } catch (err) { this.e = { ...this.e, models: { error: err.message } }; }
  }
  pickModel(value) {
    this.set("model", value);
    this.checkModel();
  }
  // checkModel tries the model as the form has it; when it answers, the
  // context field gets what its server says (or the size that holds).
  async checkModel() {
    const f = this.e.form;
    if (!f.endpoint || !f.model) { this.e = { ...this.e, check: null }; return; }
    this.e = { ...this.e, check: { wait: true } };
    try {
      const r = await api("POST", "/v1/ui/edit/model-check", { name: this.e.name || "", endpoint: f.endpoint, model: f.model, token: f.token || "", privacy: f.privacy || "" });
      this.e = { ...this.e, check: r, form: r.ok ? { ...this.e.form, context_tokens: r.context_tokens } : this.e.form };
    } catch (err) { this.e = { ...this.e, check: { ok: false, error: err.message } }; }
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
        <label>${T.mod_provider}</label><select ?disabled=${locked} @change=${ev => this.setProvider(ev.target.value)}>
          ${opts([...(s.providers || []).map(p => [p.key, p.name]), ["", T.ed_provider_custom]], f.provider)}</select>
        ${f.provider ? nothing : html`<label>${T.mod_endpoint}</label>
          <input .value=${f.endpoint ?? ""} placeholder="http://…/v1" ?disabled=${locked}
            @input=${ev => this.set("endpoint", ev.target.value)} @change=${() => { this.loadModels(); this.checkModel(); }}>`}
        <label>${T.mod_token}</label><input type="password" autocomplete="new-password" ?disabled=${locked} .value=${f.token || ""}
          placeholder=${f.token_set ? T.ed_token_keep : ""} @input=${ev => this.set("token", ev.target.value)}
          @change=${() => { this.loadModels(); this.checkModel(); }}>
        ${f.token_set && !locked ? html`<span></span><label class="checkline"><input type="checkbox" .checked=${!!f.token_clear}
          @change=${ev => this.set("token_clear", ev.target.checked)}> ${T.ed_token_clear}</label>` : nothing}
        <label>${T.mod_model}</label>${this.modelPicker(locked)}
        <label>${T.mod_privacy}</label><select ?disabled=${locked} @change=${ev => this.set("privacy", ev.target.value)}>
          ${opts([["local", T.privacy_local], ["cloud", T.privacy_cloud]], f.privacy)}</select>
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
  // modelPicker is a list of what the server offers; while there is no
  // list, a text field.
  modelPicker(locked) {
    const f = this.e.form, m = this.e.models;
    const list = m && m.models && m.models.length ? m.models : null;
    if (!list || locked) {
      return html`<div><input .value=${f.model ?? ""} ?disabled=${locked} @input=${ev => this.set("model", ev.target.value)}
          @change=${() => this.checkModel()}>
        ${m && m.wait ? html`<div class="hint">${T.loading}</div>` : m && !list && !locked ? html`<div class="hint warn">${T.ed_models_none}</div>` : nothing}
        ${this.checkHint()}</div>`;
    }
    const all = f.model && !list.includes(f.model) ? [f.model, ...list] : list;
    return html`<div><select @change=${ev => this.pickModel(ev.target.value)}>
      ${opts([["", "–"], ...all.map(n => [n, n])], f.model)}</select>${this.checkHint()}</div>`;
  }
  // checkHint says under the model why it did not answer; nothing when it did.
  checkHint() {
    const c = this.e.check;
    if (!c || c.ok) return nothing;
    if (c.wait) return html`<div class="hint">${T.ed_checking}</div>`;
    return html`<div class="hint warn">${T["ed_check_" + c.reason] || c.error}</div>`;
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
      ${cat.mcp || f.mcp || cat.phonebook || name === "telefon" ? html`<div class="actions left"><button class="secondary" @click=${e => { e.preventDefault(); this.checkMCP(); }}>${T.ed_mcp_check}</button>
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
    if (m.ok && m.books) return html`<span class="ok">${T.ed_book_ok(m.contacts, m.books, m.ms)}</span>`;
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
