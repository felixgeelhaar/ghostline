const test = require('node:test');
const assert = require('node:assert/strict');

const {
  findingsToIssues,
  escapeIssueText,
  issueBody,
  reportNoxIssues,
} = require('./report-nox-issues.cjs');

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

test('skips baseline findings already linked to a GitHub issue', () => {
  const result = findingsToIssues(
    { findings: [{ RuleID: 'VULN-1', Severity: 'medium', Message: 'accepted', Fingerprint: 'tracked', Location: { FilePath: 'Dockerfile' } }] },
    [{ fingerprint: 'tracked', reason: 'Tracked in https://github.com/example/repo/issues/12' }],
  );

  assert.deepEqual(result, []);
});

test('keeps baseline findings without a linked issue reportable', () => {
  const result = findingsToIssues(
    { findings: [{ RuleID: 'IAC-1', Severity: 'low', Message: 'review me', Fingerprint: 'untracked', Location: { FilePath: '.github/workflows/ci.yml' } }] },
    [{ fingerprint: 'untracked', reason: 'Accepted with a temporary exception' }],
  );

  assert.equal(result.length, 1);
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
        update: async ({ issue_number, state }) => {
          const issue = issues.find((item) => item.number === issue_number);
          issue.state = state;
          return { data: issue };
        },
      },
    },
  };
  const context = { repo: { owner: 'owner', repo: 'repo' }, sha: 'abc123', serverUrl: 'https://github.com', runId: 42 };
  const report = { findings: [{ RuleID: 'RULE-1', Severity: 'high', Message: 'check', Fingerprint: 'a'.repeat(64), Location: { FilePath: 'src/a.go' } }] };

  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 1, reopened: 0, existing: 0 });
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, reopened: 0, existing: 1 });
  assert.equal(issues.length, 1);
  issues[0].state = 'closed';
  assert.deepEqual(await reportNoxIssues({ github, context, report, baseline: { entries: [] } }), { created: 0, reopened: 1, existing: 0 });
  assert.equal(issues[0].state, 'open');
});
