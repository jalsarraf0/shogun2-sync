import test from 'node:test';
import assert from 'node:assert/strict';

import { escapeHtml, extractFolderId, joinDisplayPath, syncReadiness } from './ux.js';

test('escapeHtml safely renders backend and path text', () => {
  assert.equal(escapeHtml('<save name="x">&\''), '&lt;save name=&quot;x&quot;&gt;&amp;&#39;');
});

test('extractFolderId accepts a Drive URL or a raw ID', () => {
  const id = '1AbCdEfGhIjKlMnOpQrStUvWxYz';
  assert.equal(extractFolderId(`https://drive.google.com/drive/folders/${id}?usp=sharing`), id);
  assert.equal(extractFolderId(id), id);
});

test('joinDisplayPath preserves the native-looking separator', () => {
  assert.equal(joinDisplayPath('/home/me/Dropbox/', '/Saves'), '/home/me/Dropbox/Saves');
  assert.equal(joinDisplayPath('C:\\Users\\me\\Dropbox\\', 'Saves'), 'C:\\Users\\me\\Dropbox\\Saves');
});

test('Google mirror must be enabled, active, and error-free to be ready', () => {
  assert.equal(syncReadiness({ linkedOk: true }, null).ready, true);
  assert.equal(syncReadiness({ linkedOk: true }, { applicable: false }, true).ready, false);
  assert.equal(syncReadiness({ linkedOk: false }, null).ready, false);
  assert.equal(syncReadiness({ linkedOk: true }, { applicable: true, enabled: false, active: false }).ready, false);
  assert.equal(syncReadiness({ linkedOk: true }, { applicable: true, enabled: true, active: true, lastError: 'failed' }).ready, false);
  assert.equal(syncReadiness({ linkedOk: true }, { applicable: true, enabled: true, active: true }).ready, true);
});
