export interface ClipboardWriter { writeText(text: string): Promise<void> }
export interface CopyResult { ok: boolean; message: string }

const deniedErrors = new Set(['NotAllowedError', 'SecurityError'])

export async function copyRevisionSha(sha: string, clipboard: ClipboardWriter | undefined): Promise<CopyResult> {
  const shortSha = sha.slice(0, 7)
  if (!clipboard) return { ok: false, message: `Clipboard access is unavailable in this browser. Full SHA for revision ${shortSha}: ${sha}` }
  try {
    await clipboard.writeText(sha)
    return { ok: true, message: `Copied full SHA for revision ${shortSha}.` }
  } catch (e) {
    if (e instanceof DOMException && deniedErrors.has(e.name)) return { ok: false, message: `Clipboard access was denied. Full SHA for revision ${shortSha}: ${sha}` }
    return { ok: false, message: `Could not copy the SHA for revision ${shortSha}. Full SHA: ${sha}` }
  }
}
