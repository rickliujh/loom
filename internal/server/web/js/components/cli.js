// "Copy as CLI": every action in the UI can be repeated in a terminal or CI.

import { h, icon, downloadText, replace } from '../dom.js';
import * as api from '../api.js';
import { openModal } from './modal.js';
import { copyButton } from './copy.js';
import { toastError } from './toast.js';

/** The file name the command refers to (--params-file X or --items X). */
function referencedFile(command) {
  const m = /--(?:params-file|items)[ =](\S+)/.exec(command || '');
  return m ? m[1].replace(/^['"]|['"]$/g, '') : 'params.yaml';
}

/**
 * showCli({command, paramsFile}, {title, choices, current, onChoose})
 * choices: optional [{id, label}] rendered as a segmented control; onChoose(id)
 * must resolve to a new {command, paramsFile}.
 */
export function showCli(result, { title = 'Copy as CLI', choices, current, onChoose } = {}) {
  const body = h('div', { class: 'stack' });
  let active = current;
  const seg = choices ? h('div', { class: 'seg', role: 'radiogroup', 'aria-label': 'Action' }) : null;

  const paintSeg = () => {
    if (!seg) return;
    replace(seg, ...choices.map((c) => h('button', {
      type: 'button', role: 'radio', 'aria-checked': String(c.id === active),
      onClick: async () => {
        if (c.id === active) return;
        active = c.id;
        paintSeg();
        try { paint(await onChoose(c.id)); } catch (err) { toastError(err, 'Could not build the command'); }
      },
    }, c.label)));
  };

  const paint = (r) => {
    const file = referencedFile(r.command);
    replace(body, 
      seg,
      h('div', { class: 'field' },
        h('div', { class: 'field-label' }, 'Command', h('span', { class: 'meta' }, copyButton(() => r.command, { label: 'Copy command', iconOnly: false, cls: 'btn btn-sm', what: 'Command copied' }))),
        h('pre', { class: 'code code-wrap' }, r.command)),
      r.paramsFile ? h('div', { class: 'field' },
        h('div', { class: 'field-label' }, h('span', { class: 'mono' }, file),
          h('span', { class: 'meta row' },
            copyButton(() => r.paramsFile, { label: 'Copy file', iconOnly: false, cls: 'btn btn-sm', what: 'File copied' }),
            h('button', { class: 'btn btn-sm', type: 'button', onClick: () => downloadText(file, r.paramsFile, 'application/yaml') }, icon('download', 'icon-sm'), 'Download'))),
        h('pre', { class: 'code' }, r.paramsFile),
        h('div', { class: 'field-help' }, `Structured values travel in a file. Save it as ${file} next to where you run the command.`)) : null,
    );
  };
  paintSeg();
  paint(result);
  openModal({ title, body, wide: true });
}

/** Asks the server for the command line of a job request and shows it. */
export async function copyAsCli(request, opts) {
  try {
    const r = await api.cli(request);
    showCli(r, opts);
  } catch (err) {
    toastError(err, 'Could not build the command');
  }
}
