const crypto = require('node:crypto');

const ISSUE_LABEL = 'nox-finding';
const REPORT_START = '<!-- nox-report:start -->';
const REPORT_END = '<!-- nox-report:end -->';
const REPORT_DISCLAIMER = 'These reports record what Nox observed at each linked revision. They are not independent proof of exploitability or claims that findings persist in later revisions.';

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

function managedFindingBody(finding, evidence) {
  const locations = finding.locations
    .map(({ file, line }) => `- ${escapeIssueText(file)}${line ? `:${line}` : ''}`)
    .join('\n');
  return [
    `<!-- nox-fingerprint:${finding.fingerprint} -->`,
    `### ${escapeIssueText(finding.ruleID)} · ${escapeIssueText(finding.severity)}`,
    '',
    `**Finding:** ${escapeIssueText(finding.message)}`,
    '',
    '**Locations:**',
    locations,
    '',
    `**Source revision:** [\`${evidence.sha}\`](${evidence.repositoryURL}/commit/${evidence.sha})  `,
    `**Nox scan:** [workflow run](${evidence.runURL})`,
  ].join('\n');
}

function existingFindingBlocks(body) {
  const blocks = new Map();
  const start = body?.indexOf(REPORT_START) ?? -1;
  const end = body?.indexOf(REPORT_END) ?? -1;
  if (start < 0 || end < start) return blocks;
  const fullSection = body.slice(start + REPORT_START.length, end);
  const disclaimerAt = fullSection.indexOf(REPORT_DISCLAIMER);
  const section = disclaimerAt >= 0 ? fullSection.slice(0, disclaimerAt) : fullSection;
  const markers = [...section.matchAll(/<!-- nox-fingerprint:([a-f\d]{32,128}) -->/gi)];
  for (let index = 0; index < markers.length; index += 1) {
    const marker = markers[index];
    const blockEnd = markers[index + 1]?.index ?? section.length;
    blocks.set(marker[1].toLowerCase(), section.slice(marker.index, blockEnd).trim());
  }
  return blocks;
}

function reportSection(blocks) {
  return [REPORT_START, '## Nox security findings', '', ...blocks, '',
    REPORT_DISCLAIMER, REPORT_END].join('\n');
}

function replaceReportSection(body, section) {
  const original = typeof body === 'string' ? body : '';
  const start = original.indexOf(REPORT_START);
  const end = original.indexOf(REPORT_END);
  if (start >= 0 && end >= start) return `${original.slice(0, start).trimEnd()}\n\n${section}${original.slice(end + REPORT_END.length)}`.trim();
  return `${original.trim()}${original.trim() ? '\n\n' : ''}${section}`;
}

function evidenceSha(body, fingerprint) {
  const escapedFingerprint = fingerprint.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const marker = new RegExp(`<!-- nox-fingerprint:${escapedFingerprint} -->([\\s\\S]*?)(?=<!-- nox-fingerprint:|${REPORT_END}|$)`, 'i');
  const match = (body || '').match(marker);
  return match?.[1]?.match(/\*\*Source revision:\*\*\s*\[[^\]]*\]\([^\s)]*\/commit\/([a-f\d]{7,64})\)/i)?.[1];
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
  const reporterIssues = new Set();
  for (const issue of issues) {
    if (issue.pull_request) continue;
    byIssueNumber.set(issue.number, issue);
    const hasReporterLabel = (issue.labels || []).some((label) => (typeof label === 'string' ? label : label.name) === ISSUE_LABEL);
    if (hasReporterLabel) reporterIssues.add(issue.number);
    if (hasReporterLabel) {
      for (const match of issue.body?.matchAll(/<!-- nox-fingerprint:([a-f\d]{32,128}) -->/gi) || []) {
        const fingerprint = match[1].toLowerCase();
        if (!byFingerprint.has(fingerprint)) byFingerprint.set(fingerprint, issue);
      }
    }
  }

  const evidence = {
    sha: context.sha,
    repositoryURL: `https://github.com/${repository.owner}/${repository.repo}`,
    runURL: `${context.serverUrl}/${repository.owner}/${repository.repo}/actions/runs/${context.runId}`,
  };
  const counts = { created: 0, updated: 0, reopened: 0, existing: 0 };
  const groupedByIssue = new Map();
  const unlinked = [];
  for (const finding of findings) {
    const existing = byFingerprint.get(finding.fingerprint)
      || byIssueNumber.get(baselineIssues.get(finding.fingerprint));
    if (existing) groupedByIssue.set(existing.number, { issue: existing, findings: [...(groupedByIssue.get(existing.number)?.findings || []), finding] });
    else unlinked.push(finding);
  }

  for (const { issue, findings: linkedFindings } of groupedByIssue.values()) {
    const activeFindings = existingFindingBlocks(issue.body);
    let staleScan = false;
    for (const finding of linkedFindings) {
      const previousSha = evidenceSha(issue.body, finding.fingerprint);
      if (previousSha && previousSha !== evidence.sha) {
        const comparison = await github.rest.repos.compareCommitsWithBasehead({ ...repository, basehead: `${previousSha}...${evidence.sha}` });
        if (comparison.data.status !== 'ahead') {
          staleScan = true;
          break;
        }
      }
      activeFindings.set(finding.fingerprint, managedFindingBody(finding, evidence));
    }
    // An issue can aggregate multiple fingerprints. Skip it as a unit so evidence
    // for a stale fingerprint is never relabeled with another finding's newer SHA.
    if (staleScan) {
      counts.existing += linkedFindings.length;
      continue;
    }
    const nextBody = replaceReportSection(issue.body, reportSection([...activeFindings.values()]));
    const title = linkedFindings.length === 1 ? issueTitle(linkedFindings[0]) : issue.title;
    const nextLabels = reporterIssues.has(issue.number) ? undefined : [...new Set([...(issue.labels || []).map((label) => typeof label === 'string' ? label : label.name), ISSUE_LABEL])];
    const changes = { title, body: nextBody, ...(nextLabels ? { labels: nextLabels } : {}) };
    if (issue.state === 'closed') changes.state = 'open';
    const wasClosed = issue.state === 'closed';
    const changed = issue.title !== title || issue.body !== nextBody || wasClosed || Boolean(nextLabels);
    if (changed) {
      await github.rest.issues.update({ ...repository, issue_number: issue.number, ...changes });
      if (wasClosed) counts.reopened += 1;
      else counts.updated += 1;
      reporterIssues.add(issue.number);
      for (const finding of linkedFindings) byFingerprint.set(finding.fingerprint, issue);
    } else counts.existing += linkedFindings.length;
  }

  for (const finding of unlinked) {
    const created = await github.rest.issues.create({
      ...repository,
      title: issueTitle(finding),
      body: replaceReportSection('', reportSection([managedFindingBody(finding, evidence)])),
      labels: [ISSUE_LABEL],
    });
    byFingerprint.set(finding.fingerprint, created.data);
    counts.created += 1;
  }
  return counts;
}

module.exports = { findingsToIssues, escapeIssueText, issueBody, issueTitle, linkedBaselineIssues, reportNoxIssues };
