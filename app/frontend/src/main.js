import './style.css';
import './app.css';

import {
  ConfigExists,
  GetConfig,
  SaveConfigCmd,
  DetectSavePath,
  ExpectedSavePath,
  DefaultCloudRoot,
  PathExists,
  BrowseForFolder,
  AuthorizeGoogleDrive,
  RunSetup,
  GetStatus,
  RunRecover,
  ResolveConflict,
  OpenInFileManager,
  GetGoogleDriveMirrorStatus,
} from '../wailsjs/go/main/App';

const app = document.querySelector('#app');

/** @type {{provider: string, cloudRoot: string, syncSubfolder: string, savePath?: string}} */
let draft = { provider: '', cloudRoot: '', syncSubfolder: 'Shogun2SaveSync', savePath: '' };

function shell(bodyHtml) {
  app.innerHTML = `
    <div class="titlebar">
      <span class="crest">⚔️</span>
      <h1>Shogun 2 Save Sync</h1>
    </div>
    <div class="view" id="view">${bodyHtml}</div>
    <div class="footer-nav" id="footerNav" style="display:none">
      <button data-nav="status">Status</button>
      <button data-nav="recover">Recover</button>
      <button data-nav="setup">Setup</button>
    </div>
  `;
}

function setFooter(active) {
  const nav = document.getElementById('footerNav');
  nav.style.display = 'flex';
  nav.querySelectorAll('button').forEach((b) => {
    b.classList.toggle('active', b.dataset.nav === active);
    b.onclick = () => {
      if (b.dataset.nav === 'status') renderStatus();
      if (b.dataset.nav === 'recover') renderRecover();
      if (b.dataset.nav === 'setup') renderProvider();
    };
  });
}

function escapeHtml(s) {
  return (s || '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function fmtTime(unixSeconds) {
  if (!unixSeconds) return '';
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleString();
}

// ---------- Boot ----------

async function boot() {
  shell('<p class="sub">Loading…</p>');
  const exists = await ConfigExists();
  if (exists) {
    renderStatus();
  } else {
    renderWelcome();
  }
}

// ---------- Welcome ----------

function renderWelcome() {
  shell(`
    <h2>Welcome</h2>
    <p class="sub">
      Shogun 2's multiplayer campaign has no real fix for the "failed game"
      desync that eventually kills every long campaign. This tool doesn't
      fix that — nothing outside the game can. What it does is turn
      recovering from one into a five-minute job instead of a reason to
      give up on the campaign: it keeps both players' save folders mirrored
      automatically through a cloud folder you already use, and gives you a
      one-click way to spot and resolve the duplicate files a desync
      leaves behind.
    </p>
    <div class="btn-row">
      <button class="btn" id="start">Get Started</button>
    </div>
  `);
  document.getElementById('start').onclick = renderProvider;
}

// ---------- Provider selection ----------

function renderProvider() {
  const providers = [
    { id: 'dropbox', icon: '📦', label: 'Dropbox', desc: 'Recommended — most reliable of the three on both OSes.' },
    { id: 'onedrive', icon: '☁️', label: 'OneDrive', desc: 'Works well. Turn off "Files On-Demand" for this folder.' },
    { id: 'googledrive', icon: '🟢', label: 'Google Drive', desc: 'Needs an extra one-time login step — handled below.' },
  ];
  shell(`
    <h2>Which cloud service do you and your friend already share a folder on?</h2>
    <p class="sub">This should be the same account/folder your friend already invited you to.</p>
    <div class="choice-list">
      ${providers.map((p) => `
        <button class="choice" data-id="${p.id}">
          <span class="icon">${p.icon}</span>
          <span>
            <div class="label">${p.label}</div>
            <div class="desc">${p.desc}</div>
          </span>
        </button>
      `).join('')}
    </div>
  `);
  document.querySelectorAll('.choice').forEach((el) => {
    el.onclick = () => {
      draft.provider = el.dataset.id;
      if (draft.provider === 'googledrive') {
        renderGoogleDriveSetup();
      } else {
        renderCloudFolder();
      }
    };
  });
}

// ---------- Dropbox / OneDrive folder confirmation ----------

async function renderCloudFolder() {
  const defaultRoot = await DefaultCloudRoot(draft.provider);
  draft.cloudRoot = draft.cloudRoot || defaultRoot;

  shell(`
    <h2>Confirm the synced folder</h2>
    <p class="sub">
      This should already be syncing to your computer — make sure ${draft.provider === 'dropbox' ? 'Dropbox' : 'OneDrive'}
      has finished its first sync before continuing.
    </p>
    <div class="field">
      <label>Local folder</label>
      <input type="text" id="cloudRoot" value="${escapeHtml(draft.cloudRoot)}" />
      <div class="hint" id="existsHint"></div>
    </div>
    <div class="btn-row">
      <button class="btn secondary" id="browse">Browse…</button>
    </div>
    <div class="field" style="margin-top:20px">
      <label>Sync folder name (inside it)</label>
      <input type="text" id="subfolder" value="${escapeHtml(draft.syncSubfolder)}" />
    </div>
    <div class="btn-row">
      <button class="btn secondary" id="back">Back</button>
      <button class="btn" id="next">Continue</button>
    </div>
  `);

  const rootInput = document.getElementById('cloudRoot');
  const hint = document.getElementById('existsHint');

  async function checkExists() {
    const ok = await PathExists(rootInput.value);
    hint.textContent = ok ? '✓ Found' : '⚠ Not found yet — make sure the cloud app is installed and synced.';
    hint.style.color = ok ? 'var(--good)' : 'var(--bad)';
  }
  checkExists();
  rootInput.oninput = checkExists;

  document.getElementById('browse').onclick = async () => {
    const dir = await BrowseForFolder('Choose your ' + draft.provider + ' folder');
    if (dir) { rootInput.value = dir; checkExists(); }
  };
  document.getElementById('back').onclick = renderProvider;
  document.getElementById('next').onclick = () => {
    draft.cloudRoot = rootInput.value;
    draft.syncSubfolder = document.getElementById('subfolder').value || 'Shogun2SaveSync';
    renderSavePath();
  };
}

// ---------- Google Drive OAuth ----------

function extractFolderId(input) {
  const m = input.match(/[-\w]{25,}/);
  return m ? m[0] : input.trim();
}

function renderGoogleDriveSetup() {
  // Default to "I have a link" the first time; remember the choice on draft.
  if (draft.gdriveRole === undefined) draft.gdriveRole = 'receiver';

  shell(`
    <h2>Connect Google Drive</h2>
    <p class="sub">
      We'll open your browser to log in — that's the standard, safest way
      to do this (the same pattern used by tools like the GitHub CLI and
      Google's own gcloud).
    </p>
    <div class="choice-list" style="margin-bottom:20px">
      <button class="choice ${draft.gdriveRole === 'receiver' ? 'selected' : ''}" data-role="receiver">
        <span class="icon">📥</span>
        <span><div class="label">A friend shared a folder with me</div>
        <div class="desc">You have a link they sent you.</div></span>
      </button>
      <button class="choice ${draft.gdriveRole === 'host' ? 'selected' : ''}" data-role="host">
        <span class="icon">📤</span>
        <span><div class="label">I'm sharing my own Drive</div>
        <div class="desc">No link needed — we'll create the folder and give you a link to send.</div></span>
      </button>
    </div>
    <div class="field" id="folderLinkField" style="${draft.gdriveRole === 'host' ? 'display:none' : ''}">
      <label>Shared folder link or ID</label>
      <input type="text" id="folderLink" placeholder="https://drive.google.com/drive/folders/..." />
    </div>
    <div class="field">
      <label>Sync folder name${draft.gdriveRole === 'host' ? ' (created in your Drive)' : ' (created inside it)'}</label>
      <input type="text" id="subfolder" value="${escapeHtml(draft.syncSubfolder)}" />
    </div>
    <div id="authArea">
      <div class="btn-row">
        <button class="btn secondary" id="back">Back</button>
        <button class="btn" id="authBtn">Authorize Google Drive</button>
      </div>
    </div>
    <div id="authStatus"></div>
  `);

  document.querySelectorAll('.choice[data-role]').forEach((el) => {
    el.onclick = () => {
      draft.gdriveRole = el.dataset.role;
      renderGoogleDriveSetup();
    };
  });

  document.getElementById('back').onclick = renderProvider;
  document.getElementById('authBtn').onclick = async () => {
    const isHost = draft.gdriveRole === 'host';
    const link = isHost ? '' : document.getElementById('folderLink').value;
    const folderId = isHost ? '' : extractFolderId(link);
    const subfolder = document.getElementById('subfolder').value || 'Shogun2SaveSync';
    if (!isHost && !folderId) {
      document.getElementById('authStatus').innerHTML = '<div class="banner error">Paste the folder link first.</div>';
      return;
    }

    const statusEl = document.getElementById('authStatus');
    statusEl.innerHTML = `
      <div class="status-line"><span class="dot busy"></span>
      Waiting for you to finish logging in and approving access in your browser…</div>
    `;
    document.getElementById('authBtn').disabled = true;

    try {
      const res = await AuthorizeGoogleDrive(folderId, subfolder);
      if (res.ok) {
        draft.syncSubfolder = subfolder;
        draft.cloudRoot = await DefaultCloudRoot('googledrive');
        if (res.shareLink) {
          statusEl.innerHTML = `
            <div class="banner success">Connected! Send this link to your friend so they can set up too:</div>
            <div class="card"><code id="shareLinkText">${escapeHtml(res.shareLink)}</code></div>
            <div class="btn-row">
              <button class="btn secondary" id="copyLink">Copy link</button>
              <button class="btn" id="continueBtn">Continue</button>
            </div>
          `;
          document.getElementById('copyLink').onclick = () => {
            navigator.clipboard?.writeText(res.shareLink);
            document.getElementById('copyLink').textContent = 'Copied!';
          };
          document.getElementById('continueBtn').onclick = renderSavePath;
        } else {
          statusEl.innerHTML = '<div class="banner success">Connected. Continuing…</div>';
          setTimeout(renderSavePath, 600);
        }
      } else {
        statusEl.innerHTML = `<div class="banner error">${escapeHtml(res.error || 'Something went wrong.')}</div>`;
        document.getElementById('authBtn').disabled = false;
      }
    } catch (err) {
      statusEl.innerHTML = `<div class="banner error">${escapeHtml(String(err))}</div>`;
      document.getElementById('authBtn').disabled = false;
    }
  };
}

// ---------- Save path ----------

async function renderSavePath() {
  const detected = await DetectSavePath();
  const expected = detected || (await ExpectedSavePath());
  draft.savePath = detected || '';

  shell(`
    <h2>Shogun 2 save folder</h2>
    ${detected ? `
      <div class="banner success">Found it automatically:</div>
      <div class="card"><code>${escapeHtml(detected)}</code></div>
    ` : `
      <div class="banner info">
        Couldn't find it automatically — that's normal if you haven't run
        Shogun 2 on this computer before. Run the game once (any
        campaign, just to create the save folder), then click Detect
        again. If you know the path yourself (e.g. a non-default Steam
        library location), you can type it in directly below instead.
        Expected location:
      </div>
      <div class="card"><code>${escapeHtml(expected || '(unknown)')}</code></div>
      <div class="field">
        <label>Save folder path (optional manual override)</label>
        <input type="text" id="manualPath" placeholder="${escapeHtml(expected || '')}" />
        <div class="hint" id="manualPathHint"></div>
      </div>
      <div class="btn-row">
        <button class="btn secondary" id="browseSave">Browse…</button>
      </div>
    `}
    <div class="btn-row">
      <button class="btn secondary" id="back">Back</button>
      <button class="btn secondary" id="redetect">Detect again</button>
      <button class="btn" id="next" ${detected ? '' : 'disabled'}>Continue</button>
    </div>
  `);

  document.getElementById('back').onclick = () => (draft.provider === 'googledrive' ? renderGoogleDriveSetup() : renderCloudFolder());
  document.getElementById('redetect').onclick = renderSavePath;
  document.getElementById('next').onclick = runSetupAndFinish;

  if (!detected) {
    const manualInput = document.getElementById('manualPath');
    const hint = document.getElementById('manualPathHint');
    const nextBtn = document.getElementById('next');

    const checkManual = async () => {
      const val = manualInput.value.trim();
      if (!val) {
        hint.textContent = '';
        nextBtn.disabled = true;
        draft.savePath = '';
        return;
      }
      const ok = await PathExists(val);
      hint.textContent = ok ? '✓ Found' : '⚠ That folder doesn\'t exist.';
      hint.style.color = ok ? 'var(--good)' : 'var(--bad)';
      nextBtn.disabled = !ok;
      draft.savePath = ok ? val : '';
    };
    manualInput.oninput = checkManual;

    document.getElementById('browseSave').onclick = async () => {
      const dir = await BrowseForFolder('Choose your Shogun 2 save_games_multiplayer folder');
      if (dir) {
        manualInput.value = dir;
        checkManual();
      }
    };
  }
}

async function runSetupAndFinish() {
  shell('<div class="status-line"><span class="dot busy"></span> Setting up…</div>');
  const cfg = {
    cloud_provider: draft.provider,
    cloud_root: draft.cloudRoot,
    sync_subfolder: draft.syncSubfolder,
  };
  const saveErr = await SaveConfigCmd(cfg);
  if (saveErr) {
    shell(`<div class="banner error">Couldn't save settings: ${escapeHtml(saveErr)}</div>
      <button class="btn" id="retry">Try again</button>`);
    document.getElementById('retry').onclick = () => renderSavePath();
    return;
  }

  const result = await RunSetup(cfg, draft.savePath || '');
  if (!result.ok) {
    shell(`
      <h2>Setup didn't finish</h2>
      <div class="banner error">${escapeHtml(result.error)}</div>
      <div class="btn-row">
        <button class="btn secondary" id="back">Back</button>
      </div>
    `);
    document.getElementById('back').onclick = renderSavePath;
    return;
  }

  shell(`
    <h2>${result.alreadySet ? "You're all set" : 'All done!'}</h2>
    <div class="banner success">
      ${result.alreadySet ? 'This was already set up — nothing to change.' : 'Your saves will now sync automatically.'}
    </div>
    <div class="card">
      <div class="hint">Save folder</div>
      <code>${escapeHtml(result.savePath)}</code>
      <div class="hint" style="margin-top:10px">Syncs to</div>
      <code>${escapeHtml(result.syncTarget)}</code>
    </div>
    <p class="sub">Have your friend run the same setup on their computer, pointed at this same shared folder. Then you're both good to play.</p>
    <div class="btn-row">
      <button class="btn" id="toStatus">Go to Status</button>
    </div>
  `);
  document.getElementById('toStatus').onclick = renderStatus;
}

// ---------- Status ----------

async function renderStatus() {
  shell('<div class="status-line"><span class="dot busy"></span> Checking…</div>');
  const status = await GetStatus();
  const cfg = await GetConfig();

  let mirrorHtml = '';
  if (cfg.cloud_provider === 'googledrive') {
    const m = await GetGoogleDriveMirrorStatus();
    if (m.applicable) {
      mirrorHtml = `
        <div class="status-line">
          <span class="dot ${m.active ? 'good' : 'bad'}"></span>
          Google Drive background sync: ${m.active ? 'running' : 'not running'}
          ${m.lastSync ? `<span class="hint" style="margin-left:8px">last synced ${escapeHtml(m.lastSync)}</span>` : ''}
        </div>
      `;
    }
  }

  shell(`
    <h2>Status</h2>
    <div class="card">
      <div class="status-line">
        <span class="dot ${status.linkedOk ? 'good' : 'bad'}"></span>
        Save folder ${status.linkedOk ? 'is linked and syncing' : 'is not set up correctly'}
      </div>
      ${mirrorHtml}
      <div class="hint" style="margin-top:10px">Save folder</div>
      <code>${escapeHtml(status.savePath || '(not found)')}</code>
      <div class="hint" style="margin-top:10px">Synced folder</div>
      <code>${escapeHtml(status.syncTarget)}</code>
    </div>
    ${!status.linkedOk ? `
      <div class="banner error">Something's not right. Run Setup again from the bottom bar.</div>
    ` : `
      <div class="banner success">You're good to play. If a desync happens, use Recover below.</div>
    `}
  `);
  setFooter('status');
}

// ---------- Recover ----------

async function renderRecover() {
  shell('<div class="status-line"><span class="dot busy"></span> Scanning for conflicts…</div>');
  const res = await RunRecover();

  if (!res.ok) {
    shell(`<div class="banner error">${escapeHtml(res.error)}</div>`);
    setFooter('recover');
    return;
  }

  if (!res.conflicts || res.conflicts.length === 0) {
    shell(`
      <h2>Recover</h2>
      <div class="banner success">No problem files found — looks clean.</div>
      ${res.recent && res.recent.length ? `
        <p class="sub">Most recent saves in the shared folder:</p>
        <div class="file-list">
          ${res.recent.map((f) => `
            <div class="file-row">
              <div><div class="name">${escapeHtml(f.name)}</div><div class="when">${fmtTime(f.modified)}</div></div>
            </div>
          `).join('')}
        </div>
      ` : ''}
    `);
    setFooter('recover');
    return;
  }

  shell(`
    <h2>Recover</h2>
    <div class="banner info">
      Found files that look like sync conflicts (both players saved at the
      same moment). Talk to the other player, figure out together which
      save is the right one to keep, then resolve the rest below —
      resolving moves them to a hidden .shogun2sync-trash folder, so
      nothing is permanently deleted by mistake.
    </div>
    <div class="file-list" id="conflictList">
      ${res.conflicts.map((f, i) => `
        <div class="file-row" data-path="${escapeHtml(f.path)}">
          <div><div class="name">${escapeHtml(f.name)}</div><div class="when">${fmtTime(f.modified)}</div></div>
          <button class="btn danger resolveBtn" data-idx="${i}">Resolve</button>
        </div>
      `).join('')}
    </div>
    <div class="btn-row">
      <button class="btn secondary" id="openFolder">Open folder</button>
    </div>
  `);

  document.querySelectorAll('.resolveBtn').forEach((btn) => {
    btn.onclick = async () => {
      const row = btn.closest('.file-row');
      const path = row.dataset.path;
      btn.disabled = true;
      btn.textContent = 'Moving…';
      const err = await ResolveConflict(path);
      if (err) {
        btn.textContent = 'Failed';
        btn.title = err;
      } else {
        row.remove();
      }
    };
  });

  const first = res.conflicts[0];
  document.getElementById('openFolder').onclick = () => {
    OpenInFileManager(first.path.substring(0, first.path.lastIndexOf('/')));
  };

  setFooter('recover');
}

boot();
