// Catalogue (#/): every module under the configured roots, grouped by root and
// folder, with a fuzzy filter, an "open by path or URL" box and recent jobs.

import { h, icon, replace, markIndices, debounce } from '../dom.js';
import * as api from '../api.js';
import { state, prefs, emit } from '../store.js';
import { href, navigate } from '../router.js';
import { fuzzyFilter } from '../lib/fuzzy.js';
import { dirName, relTime, plural, jobTitle, jobKindLabel, isLocalSource } from '../lib/format.js';
import { stateIcon } from '../components/chip.js';
import { openModal } from '../components/modal.js';
import { toast, toastError } from '../components/toast.js';

export function moduleHref(source, tab, query) {
  return href(`/m/${encodeURIComponent(source)}${tab ? `/${tab}` : ''}`, query);
}

export function renderCatalogue(outlet) {
  let destroyed = false;
  let modules = state.modules;
  let jobsList = null;
  let query = prefs.get('catalogue.filter', '');

  const listEl = h('div', { 'aria-live': 'polite' });
  const railJobs = h('div', null, skeletonRail());
  const countEl = h('span', null, '');

  const search = h('input', {
    class: 'input', type: 'search', placeholder: 'Filter modules — try “onb svc”', value: query,
    'aria-label': 'Filter modules', autocomplete: 'off', spellcheck: 'false',
    onInput: debounce(() => { query = search.value; prefs.set('catalogue.filter', query); paint(); }, 80),
    onKeydown: (e) => {
      if (e.key === 'Enter') {
        const first = listEl.querySelector('a.mod-card');
        if (first) first.click();
      }
    },
  });

  const openInput = h('input', {
    class: 'input', placeholder: '/path/to/module or git URL',
    'aria-label': 'Module path or git URL', autocomplete: 'off', spellcheck: 'false',
  });
  const openErr = h('div', { class: 'field-error', 'aria-live': 'polite' });
  const openForm = h('form', {
    class: 'stack-sm',
    onSubmit: (e) => {
      e.preventDefault();
      const v = openInput.value.trim();
      const msg = checkSource(v);
      if (msg) { replace(openErr, icon('alert', 'icon-sm'), msg); openInput.setAttribute('aria-invalid', 'true'); return; }
      replace(openErr);
      openInput.removeAttribute('aria-invalid');
      navigate(`/m/${encodeURIComponent(v)}`);
    },
  },
  h('div', { class: 'open-box' }, openInput, h('button', { class: 'btn', type: 'submit' }, icon('arrow-right'), 'Open')),
  openErr);

  const refreshBtn = h('button', { class: 'btn', type: 'button', onClick: () => load(true) }, icon('refresh'), 'Refresh');

  const page = h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', { class: 'title-block' },
        h('h1', null, 'Modules'),
        h('div', { class: 'subtitle' }, countEl)),
      h('div', { class: 'page-actions' },
        refreshBtn,
        h('button', { class: 'btn btn-primary', type: 'button', onClick: newModuleDialog }, icon('plus'), 'New module'))),
    h('div', { class: 'catalogue' },
      h('section', { 'aria-label': 'Modules' },
        h('div', { class: 'toolbar' }, h('div', { class: 'search' }, icon('search'), search)),
        listEl),
      h('aside', { class: 'rail', 'aria-label': 'Open and recent' },
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('h2', null, icon('link'), 'Open by path or git URL')),
          h('div', { class: 'card-body' }, openForm,
            h('p', { class: 'field-help mt-2' }, 'A local path must be absolute and inside a root. A git URL may carry //subdir.'))),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' },
            h('h2', null, icon('history'), 'Recent jobs'),
            h('a', { class: 'btn btn-ghost btn-sm', href: href('/jobs') }, 'All jobs', icon('chevron-right', 'icon-sm'))),
          railJobs),
        h('section', { class: 'card' },
          h('div', { class: 'card-head' }, h('h2', null, icon('sparkle'), 'Start something new')),
          h('div', { class: 'card-body stack-sm' },
            h('a', { class: 'btn btn-block', href: href('/generate') }, icon('wand'), 'Generate a module from a PR'),
            h('a', { class: 'btn btn-block', href: href('/bulk') }, icon('layers'), 'Scaffold a bulk run'))))));

  outlet.appendChild(page);
  paint();
  load(false);
  loadJobs();

  async function load(refresh) {
    refreshBtn.disabled = true;
    try {
      const r = await api.modules.list(refresh);
      if (destroyed) return;
      modules = (r && r.modules) || [];
      state.modules = modules;
      if (refresh) toast(`Found ${plural(modules.length, 'module')}`, { kind: 'ok', timeout: 2000 });
    } catch (err) {
      if (destroyed) return;
      if (!modules) {
        replace(listEl, h('div', { class: 'banner banner-err' }, icon('alert'),
          h('div', { class: 'banner-body' }, h('div', { class: 'banner-title' }, 'Could not list modules'), err.message)));
        return;
      }
      toastError(err, 'Refresh failed');
    } finally {
      refreshBtn.disabled = false;
    }
    paint();
  }

  async function loadJobs() {
    try {
      jobsList = api.jobList(await api.jobs.list({ limit: 50 }));
    } catch (err) {
      jobsList = [];
      if (!destroyed) replace(railJobs, h('div', { class: 'card-body muted small' }, err.message));
      return;
    }
    if (destroyed) return;
    paintRail();
    paint();
  }

  function paintRail() {
    const recent = (jobsList || []).slice(0, 8);
    if (!recent.length) {
      replace(railJobs, h('div', { class: 'card-body' }, h('p', { class: 'muted small' },
        'No jobs yet. Open a module and run a Quick preview — it executes nothing.')));
      return;
    }
    replace(railJobs, recent.map((j) => h('a', { class: 'job-mini', href: href(`/jobs/${encodeURIComponent(j.id)}`) },
      h('span', { class: `status-${stateClass(j.state)}`, title: j.state }, stateIcon(j.state)),
      h('span', { class: 'jm-title' }, jobTitle(j)),
      h('span', { class: 'jm-time' }, relTime(j.createdAt)),
      h('span', { class: 'jm-sub' }, `${jobKindLabel(j)} · ${j.state}`))));
  }

  function lastJobFor(dir) {
    if (!jobsList) return null;
    return jobsList.find((j) => (j.module && j.module.dir === dir) || (j.request && j.request.source === dir)) || null;
  }

  function paint() {
    if (!modules) {
      countEl.textContent = 'Looking under the roots…';
      replace(listEl, h('div', { class: 'cards' }, Array.from({ length: 6 }, () => h('div', { class: 'skel skel-card' }))));
      return;
    }
    const roots = (state.info && state.info.roots) || [...new Set(modules.map((m) => m.root))];
    countEl.textContent = `${plural(modules.length, 'module')} under ${plural(roots.length, 'root')}`;
    if (!modules.length) {
      replace(listEl, h('div', { class: 'empty' }, icon('module', 'icon-xl'),
        h('h3', null, 'No modules found'),
        h('p', null, `Nothing under ${roots.join(', ') || 'the roots'} holds a loom.yaml or loom.jsonnet. Create one here, generate one from a pull request, or restart loom serve with --root pointing at your modules.`),
        h('div', { class: 'actions' },
          h('button', { class: 'btn btn-primary', type: 'button', onClick: newModuleDialog }, icon('plus'), 'New module'),
          h('a', { class: 'btn', href: href('/generate') }, icon('wand'), 'Generate from a PR'))));
      return;
    }
    if (query.trim()) {
      const ranked = fuzzyFilter(modules, query.trim(), [(m) => m.name || '', (m) => m.rel || m.dir]);
      if (!ranked.length) {
        replace(listEl, h('div', { class: 'empty empty-compact' }, icon('search', 'icon-xl'),
          h('h3', null, `No module matches “${query.trim()}”`),
          h('p', null, 'The filter matches names and paths. To open a module outside the list, use “Open by path or git URL”.'),
          h('div', { class: 'actions' }, h('button', { class: 'btn', type: 'button', onClick: () => { search.value = ''; query = ''; prefs.set('catalogue.filter', ''); paint(); search.focus(); } }, 'Clear filter'))));
        return;
      }
      replace(listEl,
        h('div', { class: 'section-title' }, `${plural(ranked.length, 'match', 'matches')}`, h('span', { class: 'rule' })),
        h('div', { class: 'cards' }, ranked.map((r) => card(r.item, r.matches))));
      return;
    }
    const byRoot = new Map();
    for (const m of modules) {
      if (!byRoot.has(m.root)) byRoot.set(m.root, new Map());
      const folder = dirName(m.rel || '') || '.';
      const f = byRoot.get(m.root);
      if (!f.has(folder)) f.set(folder, []);
      f.get(folder).push(m);
    }
    replace(listEl, [...byRoot.entries()].map(([root, folders]) => h('section', { class: 'group', 'aria-label': root },
      h('div', { class: 'group-root' }, icon('folder-open'), h('span', { class: 'root-path' }, root),
        h('span', { class: 'badge' }, plural([...folders.values()].reduce((n, x) => n + x.length, 0), 'module'))),
      [...folders.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([folder, mods]) => h('div', { class: 'folder' },
        h('div', { class: 'folder-name' }, icon('folder', 'icon-sm'), folder === '.' ? '(root)' : `${folder}/`),
        h('div', { class: 'cards' }, mods.sort((a, b) => (a.name || a.rel).localeCompare(b.name || b.rel)).map((m) => card(m))))))));
  }

  function card(m, matches) {
    const name = m.name || (m.rel || m.dir).split('/').pop();
    const orchestrator = m.children > 0;
    const last = lastJobFor(m.dir);
    return h('a', { class: ['mod-card', m.loadError && 'has-error'], href: moduleHref(m.dir) },
      h('div', { class: 'mc-head' },
        h('span', { class: orchestrator ? 'status-run' : 'faint' }, icon(orchestrator ? 'root' : 'module')),
        h('span', { class: 'mc-name' }, matches && matches[0] ? markIndices(name, matches[0]) : name),
        m.format === 'jsonnet' ? h('span', { class: 'badge badge-mono' }, 'jsonnet') : null,
        h('span', { class: 'spacer' }),
        last ? h('span', { class: `status-${stateClass(last.state)}`, title: `Last job: ${last.state}, ${relTime(last.createdAt)}`, 'aria-label': `Last job ${last.state}` }, stateIcon(last.state)) : null),
      h('div', { class: 'mc-path' }, matches && matches[1] ? markIndices(m.rel || m.dir, matches[1]) : (m.rel || m.dir)),
      m.loadError
        ? h('div', { class: 'mc-err' }, m.loadError)
        : h('div', { class: 'mc-meta' },
          h('span', null, icon('sliders', 'icon-sm'), m.params && m.params.total ? `${m.params.required} required · ${m.params.total} params` : 'no params'),
          h('span', null, icon(m.hasTarget ? 'repo' : 'folder', 'icon-sm'), m.hasTarget ? 'target repo' : 'no target'),
          m.children ? h('span', null, icon('tree', 'icon-sm'), plural(m.children, 'submodule')) : null));
  }

  function newModuleDialog() {
    const roots = (state.info && state.info.roots) || [];
    const rootSel = h('select', { class: 'select', 'aria-label': 'Root' }, roots.map((r) => h('option', { value: r }, r)));
    const relIn = h('input', { class: 'input', placeholder: 'modules/new-service', 'aria-label': 'Directory inside the root', autocomplete: 'off', spellcheck: 'false' });
    const nameIn = h('input', { class: 'input', placeholder: 'new-service', id: 'nm-name', autocomplete: 'off', spellcheck: 'false' });
    const resolved = h('div', { class: 'resolved-path' });
    const err = h('div', { class: 'field-error', 'aria-live': 'polite' });
    let nameTouched = false;
    const sync = () => {
      const rel = relIn.value.trim().replace(/^\/+|\/+$/g, '');
      resolved.textContent = rel ? `${rootSel.value}/${rel}` : '';
      if (!nameTouched) nameIn.value = rel.split('/').pop() || '';
    };
    relIn.addEventListener('input', sync);
    rootSel.addEventListener('change', sync);
    nameIn.addEventListener('input', () => { nameTouched = true; });
    const submit = async () => {
      const rel = relIn.value.trim().replace(/^\/+|\/+$/g, '');
      if (!rel) { replace(err, icon('alert', 'icon-sm'), 'Name a directory for the module.'); relIn.focus(); return; }
      if (rel.split('/').includes('..')) { replace(err, icon('alert', 'icon-sm'), 'The directory may not contain “..”.'); return; }
      const dir = `${rootSel.value}/${rel}`;
      try {
        const entry = await api.modules.create(dir, nameIn.value.trim() || rel.split('/').pop());
        m.close();
        state.modules = null;
        toast(`Created ${entry && entry.dir ? entry.dir : dir}`, { kind: 'ok' });
        emit('modules-changed');
        navigate(`/m/${encodeURIComponent((entry && entry.dir) || dir)}/files`, { path: 'loom.yaml' });
      } catch (e2) {
        replace(err, icon('alert', 'icon-sm'), e2.message);
      }
    };
    const m = openModal({
      title: 'New module',
      body: h('form', { class: 'stack', onSubmit: (e) => { e.preventDefault(); submit(); } },
        h('p', { class: 'muted small' }, 'Creates a directory with a minimal loom.yaml. You can edit it in the Files tab right after.'),
        h('div', { class: 'field' },
          h('label', { class: 'field-label' }, 'Directory'),
          h('div', { class: 'dir-picker' }, rootSel, relIn),
          resolved),
        h('div', { class: 'field' },
          h('label', { class: 'field-label', for: 'nm-name' }, 'Module name', h('span', { class: 'meta' }, 'metadata.name')),
          nameIn),
        err,
        h('button', { type: 'submit', hidden: true })),
      footer: [
        h('button', { class: 'btn', type: 'button', onClick: () => m.close() }, 'Cancel'),
        h('button', { class: 'btn btn-primary', type: 'button', onClick: submit }, icon('plus'), 'Create module'),
      ],
      initialFocus: () => relIn,
    });
  }

  return () => { destroyed = true; };
}

export function checkSource(v) {
  if (!v) return 'Enter an absolute path or a git URL.';
  if (isLocalSource(v)) return '';
  if (v.startsWith('.') || v.startsWith('~')) return 'Use an absolute path. A relative one would be read as a git URL.';
  if (/^(https?:\/\/|ssh:\/\/|git@|git:\/\/)/.test(v)) return '';
  return 'That is neither an absolute path nor a git URL (https://…, ssh://…, git@…).';
}

function stateClass(s) {
  return { succeeded: 'ok', failed: 'err', running: 'run', cancelling: 'warn', interrupted: 'warn' }[s] || 'muted';
}

function skeletonRail() {
  return h('div', { class: 'card-body' }, Array.from({ length: 4 }, () => h('div', { class: 'skel skel-line skel-w-90' })));
}
