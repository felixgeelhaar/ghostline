import { test } from 'vitest';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const assert = require('node:assert/strict');

const {
  findingsToIssues,
  escapeIssueText,
  issueBody,
  linkedBaselineIssues,
  reportNoxIssues,
} = require('../../.github/scripts/report-nox-issues.cjs');

function context(overrides = {}) {
  return { repo: { owner: 'owner', repo: 'repo' }, sha: 'abc123', serverUrl: 'https://github.com', runId: 42, ...overrides };
}

function fakeGithub(issues = [], ancestry = {}) {
  const labels = [{ name: 'nox-finding' }];
  return {
    issues,
    paginate: async (method, params) => (await method(params)).data,
    rest: {
      issues: {
        listLabelsForRepo: async () => ({ data: labels }),
        createLabel: async ({ name }) => { labels.push({ name }); },
        listForRepo: async () => ({ data: issues }),
        create: async (input) => {
          const issue = { number: Math.max(0, ...issues.map((item) => item.number)) + 1, state: 'open', labels: input.labels.map((name) => ({ name })), ...input };
          issues.push(issue);
          return { data: issue };
        },
        update: async ({ issue_number, ...changes }) => {
          const issue = issues.find((item) => item.number === issue_number);
          Object.assign(issue, changes);
          return { data: issue };
        },
      },
      repos: {
        compareCommitsWithBasehead: async ({ basehead }) => {
          const [base, head] = basehead.split('...');
          if (base === ancestry.olderSha && head === ancestry.newerSha && ancestry.comparison) {
            return { data: ancestry.comparison };
          }
          const ahead_by = base === ancestry.olderSha && head === ancestry.newerSha ? 1 : 0;
          return { data: { ahead_by, status: ahead_by ? 'ahead' : 'identical' } };
        },
      },
    },
  };
}

test('groups findings by fingerprint and retains each source location', () => {
  const result = findingsToIssues({
    findings: [
      { RuleID: 'VULN-1', Severity: 'high', Message: 'unsafe package', Fingerprint: 'same', Location: { FilePath: 'a.go', StartLine: 4 } },
      { RuleID: 'VULN-1', Severity: 'high', Message: 'unsafe package', Fingerprint: 'same', Location: { FilePath: 'b.go', StartLine: 9 } },
    ],
  }, []);

  assert.equal(result.length, 1);
  assert.match(result[0].fingerprint, /^[a-f0-9]{64}$/);
  assert.deepEqual(result[0].locations, [
    { file: 'a.go', line: 4 },
    { file: 'b.go', line: 9 },
  ]);
});

test('derives a stable fingerprint when Nox omits one', () => {
  const finding = { RuleID: 'RULE-1', Severity: 'medium', Message: 'review me', Location: { FilePath: 'src/a.go', StartLine: 2 } };
  const first = findingsToIssues({ findings: [finding] }, []);
  const second = findingsToIssues({ findings: [finding] }, []);

  assert.match(first[0].fingerprint, /^[a-f0-9]{64}$/);
  assert.equal(first[0].fingerprint, second[0].fingerprint);
});

test('keeps baseline findings linked to an issue available for synchronization', () => {
  const result = findingsToIssues(
    { findings: [{ RuleID: 'VULN-1', Severity: 'medium', Message: 'accepted', Fingerprint: 'tracked', Location: { FilePath: 'Dockerfile' } }] },
    [{ fingerprint: 'tracked', reason: 'Tracked in https://github.com/example/repo/issues/12' }],
  );

  assert.equal(result.length, 1);
  assert.equal(result[0].message, 'accepted');
});

test('keeps baseline findings without a linked issue reportable', () => {
  const result = findingsToIssues(
    { findings: [{ RuleID: 'IAC-1', Severity: 'low', Message: 'review me', Fingerprint: 'untracked', Location: { FilePath: '.github/workflows/ci.yml' } }] },
    [{ fingerprint: 'untracked', reason: 'Accepted with a temporary exception' }],
  );

  assert.equal(result.length, 1);
});

test('only links baseline issues from the scanned repository', () => {
  const result = linkedBaselineIssues([
    { fingerprint: 'a'.repeat(64), reason: 'Tracked in https://github.com/other/repo/issues/12' },
  ], { owner: 'owner', repo: 'repo' });

  assert.equal(result.size, 0);
});

test('escapes mentions and markdown supplied by an untrusted finding', () => {
  assert.equal(escapeIssueText('@everyone [click](https://bad.example)'), '＠everyone \\[click\\]\\(https\\://bad.example\\)');
});

test('issue body carries a stable marker and revision-linked evidence', () => {
  const body = issueBody({
    fingerprint: 'abc123',
    ruleID: 'RULE-7',
    severity: 'high',
    message: 'check this',
    locations: [{ file: 'src/main.go', line: 12 }],
  }, {
    repositoryURL: 'https://github.com/owner/repo',
    sha: 'def456',
    runURL: 'https://github.com/owner/repo/actions/runs/42',
  });

  assert.match(body, /<!-- nox-fingerprint:abc123 -->/);
  assert.match(body, /\/commit\/def456/);
  assert.ok(body.includes('src/main.go:12'));
  assert.match(body, /not independent proof of exploitability/i);
  assert.match(body, /\*\*Rule:\*\* RULE-7/);
});

test('preserves maintainer notes and aggregates multiple fingerprints in a linked issue', async () => {
  const issue = { number: 58, state: 'open', title: 'Tracked dependency alerts', body: 'Maintainer rationale: waiting for upstream image.' , labels: [{ name: 'nox-finding' }] };
  const github = fakeGithub([issue]);
  const baseline = { entries: [
    { fingerprint: 'a'.repeat(64), reason: 'Tracked in https://github.com/owner/repo/issues/58' },
    { fingerprint: 'b'.repeat(64), reason: 'Tracked in https://github.com/owner/repo/issues/58' },
  ] };
  const report = { findings: [
    { RuleID: 'RULE-1', Severity: 'medium', Message: 'first alert', Fingerprint: 'a'.repeat(64), Location: { FilePath: 'a.go' } },
    { RuleID: 'RULE-2', Severity: 'high', Message: 'second alert', Fingerprint: 'b'.repeat(64), Location: { FilePath: 'b.go' } },
  ] };

  const result = await reportNoxIssues({ github, context: context(), report, baseline });

  assert.equal(result.updated, 1);
  assert.match(issue.body, /Maintainer rationale: waiting for upstream image\./);
  assert.match(issue.body, /nox-fingerprint:[a-f0-9]{64}/);
  assert.match(issue.body, /first alert/);
  assert.match(issue.body, /second alert/);

  await reportNoxIssues({ github, context: context({ runId: 43 }), report: { findings: [report.findings[0]] }, baseline });
  assert.match(issue.body, /second alert/);
  assert.equal((issue.body.match(/These reports record what Nox observed/g) || []).length, 1);

  // The second fingerprint remains discoverable after its baseline link is removed.
  await reportNoxIssues({ github, context: context({ runId: 44 }), report: { findings: [report.findings[1]] }, baseline: { entries: [baseline.entries[0]] } });
  assert.equal(github.issues.length, 1);
  assert.match(issue.body, /second alert/);
});

test('does not trust a matching marker on an issue without the reporter label', async () => {
  const forged = { number: 5, state: 'open', title: 'Human issue', body: `<!-- nox-fingerprint:${'a'.repeat(64)} -->\nHuman notes`, labels: [] };
  const github = fakeGithub([forged]);
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'real finding', Fingerprint: 'a'.repeat(64), Location: { FilePath: 'a.go' } }] };

  const result = await reportNoxIssues({ github, context: context(), report, baseline: { entries: [] } });

  assert.equal(result.created, 1);
  assert.equal(forged.body, `<!-- nox-fingerprint:${'a'.repeat(64)} -->\nHuman notes`);
  assert.equal(github.issues.length, 2);
});

test('does not replace newer issue evidence with an older scan', async () => {
  const fingerprint = 'a'.repeat(64);
  const olderSha = 'b'.repeat(40);
  const newerSha = 'c'.repeat(40);
  const issue = { number: 7, state: 'open', title: 'Nox finding', labels: [{ name: 'nox-finding' }], body: issueBody({ fingerprint, ruleID: 'RULE-1', severity: 'high', message: 'finding', locations: [{ file: 'a.go' }] }, { repositoryURL: 'https://github.com/owner/repo', sha: newerSha, runURL: 'https://github.com/owner/repo/actions/runs/2' }) };
  const github = fakeGithub([issue], { olderSha, newerSha });
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'finding', Fingerprint: fingerprint, Location: { FilePath: 'a.go' } }] };

  // First update creates reporter-managed, revision-linked evidence at the newer SHA.
  await reportNoxIssues({ github, context: context({ sha: newerSha }), report, baseline: { entries: [] } });
  const result = await reportNoxIssues({ github, context: context({ sha: olderSha }), report, baseline: { entries: [] } });

  assert.equal(result.updated, 0);
  assert.match(issue.body, new RegExp(`commit/${newerSha}`));
});

test('does not replace issue evidence with a divergent revision that happens to be ahead', async () => {
  const fingerprint = 'a'.repeat(64);
  const currentSha = 'b'.repeat(40);
  const divergentSha = 'c'.repeat(40);
  const issue = { number: 9, state: 'open', title: 'Nox finding', labels: [{ name: 'nox-finding' }], body: issueBody({ fingerprint, ruleID: 'RULE-1', severity: 'high', message: 'finding', locations: [{ file: 'a.go' }] }, { repositoryURL: 'https://github.com/owner/repo', sha: currentSha, runURL: 'https://github.com/owner/repo/actions/runs/2' }) };
  const github = fakeGithub([issue], { olderSha: currentSha, newerSha: divergentSha, comparison: { ahead_by: 3, status: 'diverged' } });
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'finding', Fingerprint: fingerprint, Location: { FilePath: 'a.go' } }] };

  const result = await reportNoxIssues({ github, context: context({ sha: divergentSha }), report, baseline: { entries: [] } });

  assert.equal(result.updated, 0);
  assert.match(issue.body, new RegExp(`commit/${currentSha}`));
});

test('uses the managed revision after upgrading a legacy Nox issue', async () => {
  const fingerprint = 'a'.repeat(64);
  const legacySha = 'b'.repeat(40);
  const firstNewSha = 'c'.repeat(40);
  const secondNewSha = 'd'.repeat(40);
  const finding = { RuleID: 'RULE-1', Severity: 'high', Message: 'finding', Fingerprint: fingerprint, Location: { FilePath: 'a.go' } };
  const issue = { number: 10, state: 'open', title: 'Nox RULE-1 high: finding', labels: [{ name: 'nox-finding' }], body: issueBody({ fingerprint, ruleID: 'RULE-1', severity: 'high', message: 'finding', locations: [{ file: 'a.go' }] }, { repositoryURL: 'https://github.com/owner/repo', sha: legacySha, runURL: 'https://github.com/owner/repo/actions/runs/1' }) };
  const ancestry = { olderSha: legacySha, newerSha: firstNewSha };
  const github = fakeGithub([issue], ancestry);
  await reportNoxIssues({ github, context: context({ sha: firstNewSha }), report: { findings: [finding] }, baseline: { entries: [] } });
  ancestry.olderSha = firstNewSha;
  ancestry.newerSha = secondNewSha;
  const result = await reportNoxIssues({ github, context: context({ sha: secondNewSha }), report: { findings: [finding] }, baseline: { entries: [] } });
  assert.equal(result.updated, 1);
  assert.match(issue.body, new RegExp(`commit/${secondNewSha}`));
});

test('reads the source revision field instead of commit links in finding text', async () => {
  const fingerprint = 'd'.repeat(64);
  const olderSha = 'b'.repeat(40);
  const newerSha = 'c'.repeat(40);
  const issue = { number: 8, state: 'open', title: 'Nox finding', labels: [{ name: 'nox-finding' }], body: issueBody({ fingerprint, ruleID: 'RULE-1', severity: 'high', message: `Related: https://github.com/owner/repo/commit/${olderSha}`, locations: [{ file: 'a.go' }] }, { repositoryURL: 'https://github.com/owner/repo', sha: newerSha, runURL: 'https://github.com/owner/repo/actions/runs/2' }) };
  const github = fakeGithub([issue], { olderSha, newerSha });
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'finding', Fingerprint: fingerprint, Location: { FilePath: 'a.go' } }] };

  const result = await reportNoxIssues({ github, context: context({ sha: olderSha }), report, baseline: { entries: [] } });

  assert.equal(result.updated, 0);
  assert.match(issue.body, new RegExp(`commit/${newerSha}`));
});


test('reports each fingerprint once and reopens a previously closed finding', async () => {
  const labels = [];
  const issues = [];
  const github = {
    paginate: async (method, params) => (await method(params)).data,
    rest: {
      issues: {
        listLabelsForRepo: async () => ({ data: labels }),
        createLabel: async ({ name }) => { labels.push({ name }); },
        listForRepo: async () => ({ data: issues }),
        create: async (input) => {
          const issue = { number: issues.length + 1, state: 'open', ...input };
          issues.push(issue);
          return { data: issue };
        },
        update: async ({ issue_number, ...changes }) => {
          const issue = issues.find((item) => item.number === issue_number);
          Object.assign(issue, changes);
          return { data: issue };
        },
      },
    },
  };
  const context = { repo: { owner: 'owner', repo: 'repo' }, sha: 'abc123', serverUrl: 'https://github.com', runId: 42 };
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'check', Fingerprint: 'a'.repeat(64), Location: { FilePath: 'src/a.go' } }] };

  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 1, updated: 0, reopened: 0, existing: 0 });
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, updated: 0, reopened: 0, existing: 1 });
  assert.equal(issues.length, 1);
  context.sha = 'def456';
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, updated: 1, reopened: 0, existing: 0 });
  assert.match(issues[0].body, /\/commit\/def456/);
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, updated: 0, reopened: 0, existing: 1 });
  issues[0].state = 'closed';
  context.sha = 'ghi789';
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, updated: 0, reopened: 1, existing: 0 });
  assert.equal(issues[0].state, 'open');
  assert.match(issues[0].body, /\/commit\/ghi789/);
});

test('reopens the issue linked from a baseline entry when its finding recurs', async () => {
  const fingerprint = 'b'.repeat(64);
  const issue = { number: 12, state: 'closed', title: 'Known Nox finding', body: 'Tracked before fingerprint markers existed.' };
  const github = {
    paginate: async (method, params) => (await method(params)).data,
    rest: {
      issues: {
        listLabelsForRepo: async () => ({ data: [{ name: 'nox-finding' }] }),
        listForRepo: async () => ({ data: [issue] }),
        update: async ({ issue_number, ...changes }) => {
          assert.equal(issue_number, 12);
          Object.assign(issue, changes);
          return { data: issue };
        },
      },
    },
  };
  const report = { findings: [{ RuleID: 'RULE-2', Severity: 'medium', Message: 'It returned', Fingerprint: fingerprint, Location: { FilePath: 'src/a.go' } }] };
  const baseline = { entries: [{ fingerprint, reason: 'Tracked in https://github.com/owner/repo/issues/12' }] };

  const result = await reportNoxIssues({
    github,
    context: { repo: { owner: 'owner', repo: 'repo' }, sha: 'abc123', serverUrl: 'https://github.com', runId: 42 },
    report,
    baseline,
  });

  assert.deepEqual(result, { created: 0, updated: 0, reopened: 1, existing: 0 });
  assert.equal(issue.state, 'open');
  assert.match(issue.body, /nox-fingerprint:/);
  assert.match(issue.body, /\/commit\/abc123/);
});
