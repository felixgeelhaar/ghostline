const crypto = require('node:crypto');

const ISSUE_LABEL = 'nox-finding';

function field(value, ...names) {
  for (const name of names) {
    if (value?.[name] !== undefined && value[name] !== null) return value[name];
  }
  return undefined;
}

function text(value, fallback = 'Unspecified') {
  return typeof value === 'string' && value.trim() ? value.trim() : fallback;
}

function plainIssueText(value) {
  return String(value ?? '').replace(/[\u0000-\u001f\u007f]+/g, ' ').replace(/@/g, '＠').trim() || 'Unspecified';
}

function escapeIssueText(value) {
  return plainIssueText(value).replace(/[\\`*_{}\[\]<>()#+!|~:]/g, '\\$&');
}

function normalizedFingerprint(value) {
  const fingerprint = text(value, '');
  if (/^[a-f\d]{32,128}$/i.test(fingerprint)) return fingerprint.toLowerCase();
  return crypto.createHash('sha256').update(fingerprint).digest('hex');
}

function locationOf(finding) {
  const location = field(finding, 'Location', 'location') || {};
  const file = text(field(location, 'FilePath', 'filePath', 'file_path'), 'Unknown file');
  const line = field(field(location, 'StartLine', 'startLine', 'start_line') || {}, 'Line', 'line')
    ?? field(location, 'StartLine', 'startLine', 'start_line');
  return { file, ...(Number.isInteger(Number(line)) && Number(line) > 0 ? { line: Number(line) } : {}) };
}

function fingerprintOf(finding) {
  const fingerprint = field(finding, 'Fingerprint', 'fingerprint');
  if (typeof fingerprint === 'string' && fingerprint.trim()) return normalizedFingerprint(fingerprint);
  const stableInput = [
    field(finding, 'RuleID', 'rule_id', 'ruleId'),
    locationOf(finding).file,
    field(finding, 'Message', 'message'),
  ].map((part) => String(part ?? '')).join('\0');
  return crypto.createHash('sha256').update(stableInput).digest('hex');
}

function findingsToIssues(report) {
  const findings = Array.isArray(report) ? report : field(report, 'findings', 'Findings');
  if (!Array.isArray(findings)) throw new Error('Nox findings report must contain a findings array');

  const grouped = new Map();

  for (const finding of findings) {
    const fingerprint = fingerprintOf(finding);
    const ruleID = text(field(finding, 'RuleID', 'rule_id', 'ruleId'), 'NOX');
    const severity = text(field(finding, 'Severity', 'severity'), 'unknown').toLowerCase();
    const message = text(field(finding, 'Message', 'message'), 'No finding details provided');
    const existing = grouped.get(fingerprint);
    if (existing) {
      const location = locationOf(finding);
      if (!existing.locations.some((item) => item.file === location.file && item.line === location.line)) existing.locations.push(location);
    } else {
      grouped.set(fingerprint, { fingerprint, ruleID, severity, message, locations: [locationOf(finding)] });
    }
  }
  return [...grouped.values()];
}

function linkedBaselineIssues(baselineEntries, repository) {
  const linkedIssues = new Map();
  for (const entry of baselineEntries) {
    const match = text(field(entry, 'reason', 'Reason'), '')
      .match(/https:\/\/github\.com\/([\w.-]+)\/([\w.-]+)\/issues\/(\d+)/i);
    const fingerprint = field(entry, 'fingerprint', 'Fingerprint');
    if (match && fingerprint
      && match[1].toLowerCase() === repository.owner.toLowerCase()
      && match[2].toLowerCase() === repository.repo.toLowerCase()) {
      linkedIssues.set(normalizedFingerprint(fingerprint), Number(match[3]));
    }
  }
  return linkedIssues;
}

function issueBody(finding, evidence) {
  const locations = finding.locations
    .map(({ file, line }) => `- ${escapeIssueText(file)}${line ? `:${line}` : ''}`)
    .join('\n');
  return [
    `<!-- nox-fingerprint:${finding.fingerprint} -->`,
    '## Nox security finding',
    '',
    `**Rule:** ${escapeIssueText(finding.ruleID)}  `,
    `**Severity:** ${escapeIssueText(finding.severity)}  `,
    `**Finding:** ${escapeIssueText(finding.message)}`,
    '',
    '**Locations:**',
    locations,
    '',
    `**Source revision:** [\`${evidence.sha}\`](${evidence.repositoryURL}/commit/${evidence.sha})  `,
    `**Nox scan:** [workflow run](${evidence.runURL})`,
    '',
    'This report records what Nox observed at the linked revision. It is not independent proof of exploitability or a claim that the issue persists in later revisions.',
  ].join('\n');
}

function issueTitle(finding) {
  const title = `Nox ${plainIssueText(finding.ruleID)} ${plainIssueText(finding.severity)}: ${plainIssueText(finding.message)}`;
  return title.length <= 240 ? title : `${title.slice(0, 237)}...`;
}

async function reportNoxIssues({ github, context, report, baseline }) {
  const repository = context.repo;
  const baselineIssues = linkedBaselineIssues(baseline?.entries || [], repository);
  const findings = findingsToIssues(report);
  if (findings.length === 0) return { created: 0, updated: 0, reopened: 0, existing: 0 };

  const labels = await github.paginate(github.rest.issues.listLabelsForRepo, {
    ...repository,
    per_page: 100,
  });
  if (!labels.some((label) => label.name === ISSUE_LABEL)) {
    await github.rest.issues.createLabel({ ...repository, name: ISSUE_LABEL, color: 'd73a4a', description: 'Findings reported by Nox security scans' });
  }

  const issues = await github.paginate(github.rest.issues.listForRepo, {
    ...repository,
    state: 'all',
    per_page: 100,
  });
  const byFingerprint = new Map();
  const byIssueNumber = new Map();
  for (const issue of issues) {
    if (issue.pull_request) continue;
    byIssueNumber.set(issue.number, issue);
    const marker = issue.body?.match(/<!-- nox-fingerprint:([a-f\d]{32,128}) -->/i)?.[1];
    if (marker && !byFingerprint.has(marker.toLowerCase())) byFingerprint.set(marker.toLowerCase(), issue);
  }

  const evidence = {
    sha: context.sha,
    repositoryURL: `https://github.com/${repository.owner}/${repository.repo}`,
    runURL: `${context.serverUrl}/${repository.owner}/${repository.repo}/actions/runs/${context.runId}`,
  };
  const counts = { created: 0, updated: 0, reopened: 0, existing: 0 };
  for (const finding of findings) {
    const existing = byFingerprint.get(finding.fingerprint)
      || byIssueNumber.get(baselineIssues.get(finding.fingerprint));
    if (existing) {
      const body = issueBody(finding, evidence);
      const title = issueTitle(finding);
      if (existing.state === 'closed') {
        await github.rest.issues.update({ ...repository, issue_number: existing.number, state: 'open', title, body });
        counts.reopened += 1;
      } else if (existing.body !== body || existing.title !== title) {
        // Keep the issue's displayed evidence bound to the latest observed revision.
        // GitHub retains the issue edit history for earlier scan snapshots.
        await github.rest.issues.update({ ...repository, issue_number: existing.number, title, body });
        counts.updated += 1;
      } else {
        counts.existing += 1;
      }
      continue;
    }
    const created = await github.rest.issues.create({
      ...repository,
      title: issueTitle(finding),
      body: issueBody(finding, evidence),
      labels: [ISSUE_LABEL],
    });
    byFingerprint.set(finding.fingerprint, created.data);
    counts.created += 1;
  }
  return counts;
}

module.exports = { findingsToIssues, escapeIssueText, issueBody, issueTitle, linkedBaselineIssues, reportNoxIssues };
