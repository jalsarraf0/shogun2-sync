import './style.css';
import './app.css';

import {
  ConfigExists,
  GetConfig,
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
  PromoteConflict,
  OpenInFileManager,
  GetGoogleDriveMirrorStatus,
  RunUndo,
  GetLogTail,
  OpenExternal,
  Platform,
} from '../wailsjs/go/main/App';
import {
  errorMessage,
  escapeHtml,
  extractFolderId,
  joinDisplayPath,
  syncReadiness,
} from './ux.js';

const app = document.querySelector('#app');

let draft = emptyDraft();
let hasSavedConfig = false;
let savedConfig = null;
let transientNotice = '';

function emptyDraft() {
  return {
    provider: '',
    cloudRoot: '',
    syncSubfolder: 'Shogun2SaveSync',
    savePath: '',
    gClientId: '',
    gClientSecret: '',
    gdriveRole: undefined,
    folderLink: '',
  };
}

function seedDraft(cfg) {
  draft = {
    ...emptyDraft(),
    provider: cfg?.cloud_provider || '',
    cloudRoot: cfg?.cloud_root || '',
    syncSubfolder: cfg?.sync_subfolder || 'Shogun2SaveSync',
    savePath: cfg?.save_path || '',
    gClientId: cfg?.gdrive_client_id || '',
    gClientSecret: cfg?.gdrive_client_secret || '',
  };
}

function shell(bodyHtml) {
  app.innerHTML = `
    <header class="titlebar">
      <span class="crest" aria-hidden="true">⚔️</span>
      <h1>Shogun 2 Save Sync</h1>
    </header>
    <main class="view" id="view">${bodyHtml}</main>
    <nav class="footer-nav" id="footerNav" aria-label="Main navigation" hidden>
      <button type="button" data-nav="status">Status</button>
      <button type="button" data-nav="recover">Recover</button>
      <button type="button" data-nav="setup">Setup</button>
    </nav>
  `;
  const heading = app.querySelector('main h2');
  if (heading) {
    heading.tabIndex = -1;
    requestAnimationFrame(() => heading.focus({ preventScroll: true }));
  }
}

function loadingScreen(message) {
  shell(`<div class="status-line" role="status" aria-live="polite">
    <span class="dot busy" aria-hidden="true"></span>${escapeHtml(message)}
  </div>`);
}

function setFooter(active) {
  const nav = document.getElementById('footerNav');
  if (!nav) return;
  nav.hidden = false;
  nav.querySelectorAll('button').forEach((button) => {
    const isActive = button.dataset.nav === active;
    button.classList.toggle('active', isActive);
    if (isActive) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
    button.onclick = () => {
      if (button.dataset.nav === 'status') renderStatus();
      if (button.dataset.nav === 'recover') renderRecover();
      if (button.dataset.nav === 'setup') renderProvider();
    };
  });
}

function renderScreenFailure(title, error, retry, active = '') {
  shell(`
    <h2>${escapeHtml(title)}</h2>
    <div class="banner error" role="alert">${escapeHtml(errorMessage(error))}</div>
    <div class="btn-row"><button type="button" class="btn" id="screenRetry">Try again</button></div>
  `);
  document.getElementById('screenRetry').onclick = retry;
  if (active) setFooter(active);
}

async function guardedScreen(message, title, retry, render, active = '') {
  loadingScreen(message);
  try {
    await render();
  } catch (error) {
    renderScreenFailure(title, error, retry, active);
  }
}

function setInlineFailure(container, error, retry) {
  container.innerHTML = `
    <div class="banner error compact" role="alert">${escapeHtml(errorMessage(error))}</div>
    ${retry ? '<button type="button" class="btn secondary inlineRetry">Try again</button>' : ''}
  `;
  if (retry) container.querySelector('.inlineRetry').onclick = retry;
}

async function withBusyButton(button, busyText, action) {
  const originalText = button.textContent;
  button.disabled = true;
  button.textContent = busyText;
  try {
    return await action();
  } finally {
    if (button.isConnected) {
      button.disabled = false;
      button.textContent = originalText;
    }
  }
}

function setupCancelHtml() {
  return hasSavedConfig ? '<button type="button" class="btn secondary" id="cancelSetup">Cancel setup</button>' : '';
}

function bindSetupCancel() {
  const button = document.getElementById('cancelSetup');
  if (!button) return;
  button.onclick = () => {
    seedDraft(savedConfig);
    renderStatus();
  };
}

function providerLabel(provider) {
  return { dropbox: 'Dropbox', onedrive: 'OneDrive', googledrive: 'Google Drive' }[provider] || 'cloud service';
}

function usesGoogleOAuth(os) {
  return os === 'linux';
}

function formatTime(unixSeconds) {
  if (!unixSeconds) return 'Unknown time';
  return new Date(unixSeconds * 1000).toLocaleString();
}

async function copyText(text, button, feedback) {
  const original = button.textContent;
  feedback.classList.remove('error-text');
  feedback.textContent = '';
  try {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        legacyCopy(text);
      }
    } else {
      legacyCopy(text);
    }
    button.textContent = 'Copied!';
    feedback.textContent = 'Copied to clipboard.';
    setTimeout(() => {
      if (button.isConnected) button.textContent = original;
    }, 1800);
  } catch (error) {
    feedback.textContent = errorMessage(error);
    feedback.classList.add('error-text');
  }
}

function legacyCopy(text) {
  const input = document.createElement('textarea');
  input.value = text;
  input.setAttribute('readonly', '');
  input.className = 'clipboard-fallback';
  document.body.append(input);
  input.select();
  let copied = false;
  try {
    copied = document.execCommand('copy');
  } finally {
    input.remove();
  }
  if (!copied) throw new Error('Clipboard access was denied. Select the text and copy it manually.');
}

// ---------- Boot ----------

function boot() {
  return guardedScreen('Loading…', 'Couldn’t start the app', boot, async () => {
    hasSavedConfig = await ConfigExists();
    if (hasSavedConfig) {
      savedConfig = await GetConfig();
      seedDraft(savedConfig);
      await renderStatus();
    } else {
      renderWelcome();
    }
  });
}

// ---------- Welcome and provider ----------

function renderWelcome() {
  shell(`
    <h2>Welcome</h2>
    <p class="sub">
      Shogun 2’s multiplayer campaign has no real fix for the “failed game”
      desync that eventually kills long campaigns. This tool keeps both
      players’ save folders mirrored through a cloud folder you already use,
      and helps you find and resolve duplicate files after a desync.
    </p>
    <div class="btn-row"><button type="button" class="btn" id="start">Get started</button></div>
  `);
  document.getElementById('start').onclick = renderProvider;
}

function renderProvider() {
  const providers = [
    { id: 'dropbox', icon: '📦', label: 'Dropbox', desc: 'Recommended — reliable on Windows and Linux.' },
    { id: 'onedrive', icon: '☁️', label: 'OneDrive', desc: 'Turn off Files On-Demand for the shared folder.' },
    { id: 'googledrive', icon: '🟢', label: 'Google Drive', desc: 'Uses Drive Desktop on Windows and secure browser authorization on Linux.' },
  ];
  shell(`
    <h2>Choose your shared cloud folder</h2>
    <p class="sub">Choose the service that you and your friend use for the same shared folder.</p>
    <div class="choice-list">
      ${providers.map((provider) => `
        <button type="button" class="choice ${draft.provider === provider.id ? 'selected' : ''}" data-id="${provider.id}" aria-pressed="${draft.provider === provider.id}">
          <span class="icon" aria-hidden="true">${provider.icon}</span>
          <span><span class="label">${provider.label}</span><span class="desc">${provider.desc}</span></span>
        </button>
      `).join('')}
    </div>
    <div class="btn-row">${setupCancelHtml()}</div>
  `);
  bindSetupCancel();
  document.querySelectorAll('.choice[data-id]').forEach((choice) => {
    choice.onclick = async () => {
      const nextProvider = choice.dataset.id;
      if (nextProvider !== draft.provider) {
        draft.provider = nextProvider;
        draft.cloudRoot = '';
        draft.folderLink = '';
        draft.gdriveRole = undefined;
      }
      if (draft.provider === 'googledrive' && usesGoogleOAuth(await Platform())) {
        renderGoogleDriveSetup();
      } else {
        renderCloudFolder();
      }
    };
  });
}

// ---------- Local cloud folder ----------

function renderCloudFolder() {
  return guardedScreen('Finding your cloud folder…', 'Couldn’t check the cloud folder', renderCloudFolder, async () => {
    const defaultRoot = await DefaultCloudRoot(draft.provider);
    draft.cloudRoot ||= defaultRoot;
    const label = providerLabel(draft.provider);
    shell(`
      <h2>Confirm the synced folder</h2>
      <p class="sub">Make sure ${escapeHtml(label)} has finished its first sync before continuing.</p>
      <div class="field">
        <label for="cloudRoot">Local ${escapeHtml(label)} folder</label>
        <input type="text" id="cloudRoot" value="${escapeHtml(draft.cloudRoot)}" aria-describedby="existsHint" autocomplete="off">
        <div class="hint" id="existsHint" aria-live="polite"></div>
      </div>
      <div class="btn-row tight"><button type="button" class="btn secondary" id="browse">Browse…</button></div>
      <div class="field field-spaced">
        <label for="subfolder">Sync folder name inside it</label>
        <input type="text" id="subfolder" value="${escapeHtml(draft.syncSubfolder)}" autocomplete="off">
      </div>
      <div id="folderError"></div>
      <div class="btn-row">
        <button type="button" class="btn secondary" id="back">Back</button>
        ${setupCancelHtml()}
        <button type="button" class="btn" id="next">Continue</button>
      </div>
    `);
    bindSetupCancel();

    const rootInput = document.getElementById('cloudRoot');
    const hint = document.getElementById('existsHint');
    const nextButton = document.getElementById('next');
    let checkSequence = 0;
    const checkExists = async () => {
      const sequence = ++checkSequence;
      const path = rootInput.value.trim();
      if (!path) {
        hint.textContent = 'Choose a local folder.';
        hint.className = 'hint bad-text';
        nextButton.disabled = true;
        return false;
      }
      try {
        const exists = await PathExists(path);
        if (sequence !== checkSequence) return false;
        hint.textContent = exists ? '✓ Folder found' : '⚠ Folder not found — check that the cloud app is installed and synced.';
        hint.className = `hint ${exists ? 'good-text' : 'bad-text'}`;
        nextButton.disabled = !exists;
        return exists;
      } catch (error) {
        if (sequence !== checkSequence) return false;
        hint.textContent = errorMessage(error);
        hint.className = 'hint bad-text';
        nextButton.disabled = true;
        return false;
      }
    };
    rootInput.oninput = checkExists;
    await checkExists();

    document.getElementById('browse').onclick = async (event) => {
      const button = event.currentTarget;
      try {
        await withBusyButton(button, 'Opening…', async () => {
          const directory = await BrowseForFolder(`Choose your ${label} folder`);
          if (directory) {
            rootInput.value = directory;
            await checkExists();
          }
        });
      } catch (error) {
        setInlineFailure(document.getElementById('folderError'), error, () => button.click());
      }
    };
    document.getElementById('back').onclick = renderProvider;
    nextButton.onclick = async () => {
      if (!(await checkExists())) return;
      draft.cloudRoot = rootInput.value.trim();
      draft.syncSubfolder = document.getElementById('subfolder').value.trim() || 'Shogun2SaveSync';
      renderSavePath();
    };
  });
}

// ---------- Google Drive OAuth (Linux) ----------

function renderGoogleDriveSetup() {
  if (draft.gdriveRole === undefined) draft.gdriveRole = 'receiver';
  shell(`
    <h2>Connect Google Drive</h2>
    <p class="sub">We’ll open Google in your browser. Sign-in stays with Google; this app receives only the access it needs to sync the shared folder.</p>
    <div class="choice-list role-choices">
      <button type="button" class="choice ${draft.gdriveRole === 'receiver' ? 'selected' : ''}" data-role="receiver" aria-pressed="${draft.gdriveRole === 'receiver'}">
        <span class="icon" aria-hidden="true">📥</span>
        <span><span class="label">A friend shared a folder with me</span><span class="desc">You have the link they sent.</span></span>
      </button>
      <button type="button" class="choice ${draft.gdriveRole === 'host' ? 'selected' : ''}" data-role="host" aria-pressed="${draft.gdriveRole === 'host'}">
        <span class="icon" aria-hidden="true">📤</span>
        <span><span class="label">I’m sharing my own Drive</span><span class="desc">We’ll create a folder and give you a link to send.</span></span>
      </button>
    </div>
    <div class="field" id="folderLinkField" ${draft.gdriveRole === 'host' ? 'hidden' : ''}>
      <label for="folderLink">Shared folder link or ID</label>
      <input type="text" id="folderLink" value="${escapeHtml(draft.folderLink)}" placeholder="https://drive.google.com/drive/folders/…" autocomplete="off">
    </div>
    <div class="field">
      <label for="subfolder">${draft.gdriveRole === 'host' ? 'Sync folder name created in your Drive' : 'Local name for the shared sync folder'}</label>
      <input type="text" id="subfolder" value="${escapeHtml(draft.syncSubfolder)}" autocomplete="off">
      ${draft.gdriveRole === 'receiver' ? '<div class="hint">The shared link already identifies the remote sync folder; this only names its local mirror.</div>' : ''}
    </div>
    <details class="advanced">
      <summary>Use my own Google credentials</summary>
      <p class="sub compact-copy">Only needed if the built-in sign-in stops working. Follow
        <button type="button" class="link-button" id="credsHelp">rclone’s client ID guide</button>.
      </p>
      <div class="field">
        <label for="gClientId">Client ID</label>
        <input type="text" id="gClientId" value="${escapeHtml(draft.gClientId)}" autocomplete="off" spellcheck="false">
      </div>
      <div class="field">
        <label for="gClientSecret">Client secret</label>
        <input type="password" id="gClientSecret" value="${escapeHtml(draft.gClientSecret)}" autocomplete="off" spellcheck="false">
      </div>
    </details>
    <div class="btn-row" id="authArea">
      <button type="button" class="btn secondary" id="back">Back</button>
      ${setupCancelHtml()}
      <button type="button" class="btn" id="authBtn">Authorize Google Drive</button>
    </div>
    <div id="authStatus" class="action-output" aria-live="polite"></div>
  `);
  bindSetupCancel();

  document.getElementById('folderLink')?.addEventListener('input', (event) => { draft.folderLink = event.target.value; });
  document.getElementById('credsHelp').onclick = async () => {
    try {
      await OpenExternal('https://rclone.org/drive/#making-your-own-client-id');
    } catch (error) {
      setInlineFailure(document.getElementById('authStatus'), error, () => document.getElementById('credsHelp').click());
    }
  };
  document.querySelectorAll('.choice[data-role]').forEach((choice) => {
    choice.onclick = () => {
      draft.folderLink = document.getElementById('folderLink')?.value || draft.folderLink;
      draft.syncSubfolder = document.getElementById('subfolder').value.trim() || 'Shogun2SaveSync';
      draft.gClientId = document.getElementById('gClientId').value.trim();
      draft.gClientSecret = document.getElementById('gClientSecret').value.trim();
      draft.gdriveRole = choice.dataset.role;
      renderGoogleDriveSetup();
    };
  });
  document.getElementById('back').onclick = renderProvider;
  document.getElementById('authBtn').onclick = authorizeGoogleDrive;
}

async function authorizeGoogleDrive(event) {
  const button = event.currentTarget;
  const output = document.getElementById('authStatus');
  const isHost = draft.gdriveRole === 'host';
  draft.folderLink = isHost ? '' : document.getElementById('folderLink').value.trim();
  const folderId = isHost ? '' : extractFolderId(draft.folderLink);
  draft.syncSubfolder = document.getElementById('subfolder').value.trim() || 'Shogun2SaveSync';
  draft.gClientId = document.getElementById('gClientId').value.trim();
  draft.gClientSecret = document.getElementById('gClientSecret').value.trim();

  if (!isHost && !folderId) {
    setInlineFailure(output, 'Paste the shared Google Drive folder link first.');
    document.getElementById('folderLink').focus();
    return;
  }
  if (Boolean(draft.gClientId) !== Boolean(draft.gClientSecret)) {
    setInlineFailure(output, 'Enter both the client ID and client secret, or leave both blank.');
    return;
  }

  output.innerHTML = `<div class="status-line" role="status"><span class="dot busy" aria-hidden="true"></span>
    Waiting for browser authorization…</div>`;
  try {
    await withBusyButton(button, 'Waiting for Google…', async () => {
      // The credentials are passed directly and are not persisted until setup succeeds.
      const result = await AuthorizeGoogleDrive(folderId, draft.syncSubfolder, draft.gClientId, draft.gClientSecret);
      if (!result.ok) throw new Error(result.error || 'Google Drive authorization failed.');
      draft.cloudRoot = await DefaultCloudRoot('googledrive');
      if (result.shareLink) {
        document.getElementById('authArea').hidden = true;
        output.innerHTML = `
          <div class="banner success" role="status">Connected. Send this folder link to your friend:</div>
          <div class="card selectable"><code id="shareLinkText">${escapeHtml(result.shareLink)}</code></div>
          <div class="btn-row">
            <button type="button" class="btn secondary" id="copyLink">Copy link</button>
            <button type="button" class="btn" id="continueBtn">Continue</button>
          </div>
          <div class="sr-feedback" id="copyFeedback" role="status" aria-live="polite"></div>
        `;
        document.getElementById('copyLink').onclick = (copyEvent) =>
          copyText(result.shareLink, copyEvent.currentTarget, document.getElementById('copyFeedback'));
        document.getElementById('continueBtn').onclick = renderSavePath;
      } else {
        output.innerHTML = '<div class="banner success" role="status">Google Drive connected.</div>';
        await renderSavePath();
      }
    });
  } catch (error) {
    setInlineFailure(output, error, () => button.click());
  }
}

// ---------- Save folder and confirmation ----------

function renderSavePath() {
  return guardedScreen('Looking for Shogun 2 saves…', 'Couldn’t check the save folder', renderSavePath, async () => {
    const detected = await DetectSavePath();
    const expected = detected || await ExpectedSavePath();
    const remembered = draft.savePath && await PathExists(draft.savePath) ? draft.savePath : '';
    const selected = remembered || detected;
    draft.savePath = selected;
    shell(`
      <h2>Shogun 2 save folder</h2>
      ${selected ? `
        <div class="banner success" role="status">${remembered ? 'Using your configured save folder.' : 'Found the save folder automatically.'}</div>
        <div class="card selectable"><code>${escapeHtml(selected)}</code></div>
      ` : `
        <div class="banner info">We couldn’t find the folder automatically. Run Shogun 2 once to create it, then detect again, or choose it below.</div>
        <div class="card selectable"><span class="hint">Expected location</span><code>${escapeHtml(expected || '(unknown)')}</code></div>
        <div class="field">
          <label for="manualPath">Save folder path</label>
          <input type="text" id="manualPath" value="${escapeHtml(remembered)}" placeholder="${escapeHtml(expected || '')}" aria-describedby="manualPathHint" autocomplete="off">
          <div class="hint" id="manualPathHint" aria-live="polite"></div>
        </div>
        <div class="btn-row tight"><button type="button" class="btn secondary" id="browseSave">Browse…</button></div>
      `}
      <div id="savePathError"></div>
      <div class="btn-row">
        <button type="button" class="btn secondary" id="back">Back</button>
        ${setupCancelHtml()}
        <button type="button" class="btn secondary" id="redetect">Detect again</button>
        <button type="button" class="btn" id="next" ${draft.savePath ? '' : 'disabled'}>Review setup</button>
      </div>
    `);
    bindSetupCancel();
    document.getElementById('back').onclick = async () => {
      if (draft.provider === 'googledrive' && usesGoogleOAuth(await Platform())) renderGoogleDriveSetup();
      else renderCloudFolder();
    };
    document.getElementById('redetect').onclick = renderSavePath;
    document.getElementById('next').onclick = renderConfirmation;

    if (!selected) {
      const input = document.getElementById('manualPath');
      const hint = document.getElementById('manualPathHint');
      const next = document.getElementById('next');
      let sequence = 0;
      const checkManual = async () => {
        const current = ++sequence;
        const value = input.value.trim();
        if (!value) {
          hint.textContent = '';
          next.disabled = true;
          draft.savePath = '';
          return;
        }
        try {
          const exists = await PathExists(value);
          if (current !== sequence) return;
          hint.textContent = exists ? '✓ Folder found' : '⚠ That folder does not exist.';
          hint.className = `hint ${exists ? 'good-text' : 'bad-text'}`;
          next.disabled = !exists;
          draft.savePath = exists ? value : '';
        } catch (error) {
          if (current !== sequence) return;
          hint.textContent = errorMessage(error);
          hint.className = 'hint bad-text';
          next.disabled = true;
        }
      };
      input.oninput = checkManual;
      if (input.value) await checkManual();
      document.getElementById('browseSave').onclick = async (event) => {
        const button = event.currentTarget;
        try {
          await withBusyButton(button, 'Opening…', async () => {
            const directory = await BrowseForFolder('Choose the save_games_multiplayer folder');
            if (directory) {
              input.value = directory;
              await checkManual();
            }
          });
        } catch (error) {
          setInlineFailure(document.getElementById('savePathError'), error, () => button.click());
        }
      };
    }
  });
}

function renderConfirmation() {
  const syncTarget = joinDisplayPath(draft.cloudRoot, draft.syncSubfolder);
  shell(`
    <h2>Review setup</h2>
    <p class="sub">Confirm these folders before Shogun 2 Save Sync changes your save folder.</p>
    <div class="transfer-summary" aria-label="Setup folder summary">
      <div class="transfer-point"><span class="hint">Game save folder</span><code>${escapeHtml(draft.savePath)}</code></div>
      <span class="transfer-arrow" aria-hidden="true">→</span>
      <div class="transfer-point"><span class="hint">Shared sync folder</span><code>${escapeHtml(syncTarget)}</code></div>
    </div>
    <div class="banner info"><strong>Close Shogun 2 before continuing.</strong> Setup moves existing saves into the shared folder and links the game’s folder to it.</div>
    <div class="confirm-check">
      <input type="checkbox" id="gameClosed">
      <label for="gameClosed">Shogun 2 is closed, and these folders are correct.</label>
    </div>
    <div id="setupError"></div>
    <div class="btn-row">
      <button type="button" class="btn secondary" id="back">Back</button>
      ${setupCancelHtml()}
      <button type="button" class="btn" id="confirmSetup" disabled>Start syncing</button>
    </div>
  `);
  bindSetupCancel();
  const checkbox = document.getElementById('gameClosed');
  const confirm = document.getElementById('confirmSetup');
  checkbox.onchange = () => { confirm.disabled = !checkbox.checked; };
  document.getElementById('back').onclick = renderSavePath;
  confirm.onclick = runSetupAndFinish;
}

async function runSetupAndFinish(event) {
  const button = event.currentTarget;
  const output = document.getElementById('setupError');
  const configDraft = {
    cloud_provider: draft.provider,
    cloud_root: draft.cloudRoot,
    sync_subfolder: draft.syncSubfolder,
    // Keep this blank so RunSetup records the discovered/overridden path only after success.
    save_path: '',
    gdrive_client_id: draft.provider === 'googledrive' ? draft.gClientId : '',
    gdrive_client_secret: draft.provider === 'googledrive' ? draft.gClientSecret : '',
  };
  try {
    await withBusyButton(button, 'Setting up…', async () => {
      const result = await RunSetup(configDraft, draft.savePath);
      if (!result.ok) throw new Error(result.error || 'Setup did not finish.');

      const finalConfig = { ...configDraft, save_path: result.savePath };
      hasSavedConfig = true;
      savedConfig = finalConfig;
      seedDraft(finalConfig);
      shell(`
        <h2>${result.alreadySet ? 'You’re all set' : 'Setup complete'}</h2>
        <div class="banner success" role="status">${result.alreadySet ? 'This folder was already linked correctly.' : 'Your saves will now sync automatically.'}</div>
        <div class="card selectable">
          <span class="hint">Game save folder</span><code>${escapeHtml(result.savePath)}</code>
          <span class="hint section-hint">Syncs to</span><code>${escapeHtml(result.syncTarget)}</code>
        </div>
        <p class="sub">Your friend should set up the same shared folder on their computer before you play.</p>
        <div class="btn-row"><button type="button" class="btn" id="toStatus">Go to status</button></div>
      `);
      document.getElementById('toStatus').onclick = renderStatus;
    });
  } catch (error) {
    setInlineFailure(output, error, () => button.click());
  }
}

// ---------- Status ----------

function renderStatus() {
  return guardedScreen('Checking sync status…', 'Couldn’t check sync status', renderStatus, async () => {
    const [status, cfg] = await Promise.all([GetStatus(), GetConfig()]);
    let mirror = null;
    let requireMirror = false;
    if (cfg.cloud_provider === 'googledrive') {
      [mirror, requireMirror] = await Promise.all([
        GetGoogleDriveMirrorStatus(),
        Platform().then(usesGoogleOAuth),
      ]);
    }
    const readiness = syncReadiness(status, mirror, requireMirror);
    const readyMessage = requireMirror
      ? 'Ready to play. If a desync happens, open Recover.'
      : `Folder link is ready. Before playing, confirm ${providerLabel(cfg.cloud_provider)} shows the shared folder is fully up to date.`;
    const notice = transientNotice;
    transientNotice = '';
    const mirrorHtml = mirror?.applicable ? `
      <div class="status-line">
        <span class="dot ${mirror.enabled && mirror.active && !mirror.lastError ? 'good' : 'bad'}" aria-hidden="true"></span>
        <span>Google Drive background sync: ${mirror.enabled && mirror.active ? 'running' : mirror.enabled ? 'not running' : 'not enabled'}</span>
      </div>
      ${mirror.lastSync ? `<div class="hint status-detail">Last synced ${escapeHtml(mirror.lastSync)}</div>` : ''}
      ${mirror.lastError ? `<div class="banner error compact" role="alert">${escapeHtml(mirror.lastError)}</div>` : ''}
    ` : requireMirror ? `
      <div class="status-line">
        <span class="dot bad" aria-hidden="true"></span>
        <span>Google Drive background sync: unavailable</span>
      </div>
    ` : '';

    shell(`
      <h2>Status</h2>
      ${notice ? `<div class="banner success" role="status">${escapeHtml(notice)}</div>` : ''}
      ${status.error ? `<div class="banner error" role="alert">${escapeHtml(status.error)}</div>` : ''}
      <div class="card">
        <div class="status-line">
          <span class="dot ${status.linkedOk ? 'good' : 'bad'}" aria-hidden="true"></span>
          <span>Save folder ${status.linkedOk ? 'is linked to the sync folder' : 'is not linked correctly'}</span>
        </div>
        ${mirrorHtml}
        <span class="hint section-hint">Game save folder</span><code>${escapeHtml(status.savePath || '(not found)')}</code>
        <span class="hint section-hint">Shared sync folder</span><code>${escapeHtml(status.syncTarget || '(not configured)')}</code>
      </div>
      <div class="banner ${readiness.ready ? 'success' : 'error'}" ${readiness.ready ? 'role="status"' : 'role="alert"'}>
        ${readiness.ready ? escapeHtml(readyMessage) : `${escapeHtml(readiness.reason)} Refresh after fixing it, or run Setup again.`}
      </div>
      <div class="btn-row"><button type="button" class="btn secondary" id="refreshStatus">Refresh status</button></div>
      <details class="advanced">
        <summary>Troubleshooting</summary>
        <p class="sub compact-copy"><strong>Stop syncing</strong> restores a normal local save folder. The shared copy is left untouched.</p>
        <div class="btn-row">
          <button type="button" class="btn secondary" id="undoBtn">Stop syncing</button>
          <button type="button" class="btn secondary" id="logBtn">Show log</button>
        </div>
        <div id="advancedOut" class="action-output" aria-live="polite"></div>
      </details>
    `);
    setFooter('status');
    document.getElementById('refreshStatus').onclick = renderStatus;
    document.getElementById('undoBtn').onclick = confirmUndo;
    document.getElementById('logBtn').onclick = loadLog;
  }, 'status');
}

async function loadLog(event) {
  const button = event.currentTarget;
  const output = document.getElementById('advancedOut');
  try {
    await withBusyButton(button, 'Loading…', async () => {
      const log = await GetLogTail();
      output.innerHTML = `
        <pre class="log" id="logText" tabindex="0"></pre>
        <div class="btn-row"><button type="button" class="btn secondary" id="copyLog">Copy log</button></div>
        <div class="sr-feedback" id="copyLogFeedback" role="status" aria-live="polite"></div>
      `;
      document.getElementById('logText').textContent = log;
      document.getElementById('copyLog').onclick = (copyEvent) =>
        copyText(log, copyEvent.currentTarget, document.getElementById('copyLogFeedback'));
    });
  } catch (error) {
    setInlineFailure(output, error, () => button.click());
  }
}

function confirmUndo() {
  const output = document.getElementById('advancedOut');
  output.innerHTML = `
    <div class="banner info">This replaces the link with a normal folder containing a copy of your saves. The shared folder is untouched.</div>
    <div class="btn-row">
      <button type="button" class="btn secondary" id="undoCancel">Cancel</button>
      <button type="button" class="btn danger" id="undoConfirm">Yes, stop syncing</button>
    </div>
  `;
  document.getElementById('undoCancel').onclick = () => { output.innerHTML = ''; };
  document.getElementById('undoConfirm').onclick = async (event) => {
    const button = event.currentTarget;
    try {
      await withBusyButton(button, 'Restoring…', async () => {
        const result = await RunUndo();
        if (!result.ok) throw new Error(result.error || 'Could not stop syncing.');
        transientNotice = `Syncing stopped. Restored ${result.restored} file${result.restored === 1 ? '' : 's'} to ${result.savePath}.`;
        await renderStatus();
      });
    } catch (error) {
      setInlineFailure(output, error, confirmUndo);
    }
  };
}

// ---------- Recover ----------

function renderRecover() {
  return guardedScreen('Scanning for conflicts…', 'Couldn’t scan the shared folder', renderRecover, async () => {
    const result = await RunRecover();
    if (!result.ok) throw new Error(result.error || 'The conflict scan failed.');

    if (!result.conflicts?.length) {
      shell(`
        <h2>Recover</h2>
        <div class="banner success" role="status">No conflict files found — the shared folder looks clean.</div>
        ${result.recent?.length ? `
          <p class="sub">Most recent saves in the shared folder:</p>
          <div class="file-list">
            ${result.recent.map((file) => `
              <div class="file-row"><div><div class="name">${escapeHtml(file.name)}</div><div class="when">${escapeHtml(formatTime(file.modified))}</div></div></div>
            `).join('')}
          </div>
        ` : ''}
        <div class="btn-row"><button type="button" class="btn secondary" id="refreshRecover">Scan again</button></div>
      `);
      document.getElementById('refreshRecover').onclick = renderRecover;
      setFooter('recover');
      return;
    }

    shell(`
      <h2>Recover</h2>
      <div class="banner info">Talk to the other player and decide which save to keep. Both choices preserve the version you replace in <code>.shogun2sync-trash</code>; nothing is permanently deleted.</div>
      <div class="file-list" id="conflictList">
        ${result.conflicts.map((file, index) => `
          <div class="file-row" data-index="${index}">
            <div class="file-details">
              <div class="name">${escapeHtml(file.name)}</div>
              <div class="when">${escapeHtml(formatTime(file.modified))}${file.reason ? ` — ${escapeHtml(file.reason)}` : ''}
                ${file.original ? `<br>Copy of <code>${escapeHtml(file.original)}</code>` : ''}
              </div>
              <div class="resolve-output" aria-live="polite"></div>
            </div>
            <div class="conflict-actions">
              ${file.original ? '<button type="button" class="btn secondary promoteBtn">Keep this copy</button>' : ''}
              <button type="button" class="btn danger resolveBtn">Keep original</button>
            </div>
          </div>
        `).join('')}
      </div>
      <div class="btn-row">
        <button type="button" class="btn secondary" id="openFolder">Open folder</button>
        <button type="button" class="btn secondary" id="refreshRecover">Scan again</button>
      </div>
      <div id="recoverActionError" class="action-output" aria-live="polite"></div>
    `);
    document.querySelectorAll('.resolveBtn').forEach((button) => {
      button.onclick = () => resolveConflict(result.conflicts, button, false);
    });
    document.querySelectorAll('.promoteBtn').forEach((button) => {
      button.onclick = () => resolveConflict(result.conflicts, button, true);
    });
    document.getElementById('refreshRecover').onclick = renderRecover;
    document.getElementById('openFolder').onclick = async (event) => {
      const button = event.currentTarget;
      try {
        await withBusyButton(button, 'Opening…', () => OpenInFileManager(result.conflicts[0].path));
      } catch (error) {
        setInlineFailure(document.getElementById('recoverActionError'), error, () => button.click());
      }
    };
    setFooter('recover');
  }, 'recover');
}

async function resolveConflict(conflicts, button, promote) {
  const row = button.closest('.file-row');
  const output = row.querySelector('.resolve-output');
  const file = conflicts[Number(row.dataset.index)];
  try {
    await withBusyButton(button, 'Moving…', async () => {
      const resolveError = promote ? await PromoteConflict(file.path) : await ResolveConflict(file.path);
      if (resolveError) throw new Error(resolveError);
      row.remove();
      if (!document.querySelector('.resolveBtn')) await renderRecover();
    });
  } catch (error) {
    setInlineFailure(output, error, () => resolveConflict(conflicts, button, promote));
  }
}

window.addEventListener('unhandledrejection', (event) => {
  event.preventDefault();
  renderScreenFailure('Something went wrong', event.reason, boot);
});

window.addEventListener('error', (event) => {
  renderScreenFailure('Something went wrong', event.error || event.message, boot);
});

boot();
