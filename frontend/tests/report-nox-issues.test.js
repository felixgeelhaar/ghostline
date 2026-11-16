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
