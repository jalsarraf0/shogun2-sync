export function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>"']/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[character]));
}

export function errorMessage(error) {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === 'string' && error.trim()) return error;
  return 'An unexpected error occurred.';
}

export function extractFolderId(input) {
  const value = String(input ?? '').trim();
  const match = value.match(/[-\w]{25,}/);
  return match ? match[0] : value;
}

export function joinDisplayPath(root, child) {
  const base = String(root ?? '').replace(/[\\/]+$/, '');
  const leaf = String(child ?? '').replace(/^[\\/]+/, '');
  if (!base) return leaf;
  if (!leaf) return base;
  return `${base}${base.includes('\\') ? '\\' : '/'}${leaf}`;
}

export function syncReadiness(status, mirror, requireMirror = false) {
  if (!status?.linkedOk) {
    return { ready: false, reason: 'The save folder is not linked to the configured sync folder.' };
  }
  if (!mirror?.applicable) {
    return requireMirror
      ? { ready: false, reason: 'Google Drive background sync is not available.' }
      : { ready: true, reason: '' };
  }
  if (mirror.lastError) {
    return { ready: false, reason: 'Google Drive reported an error during its last background sync.' };
  }
  if (!mirror.enabled) {
    return { ready: false, reason: 'Google Drive background sync is not enabled.' };
  }
  if (!mirror.active) {
    return { ready: false, reason: 'Google Drive background sync is not running.' };
  }
  return { ready: true, reason: '' };
}
